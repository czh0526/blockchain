package p2p

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/czh0526/blockchain/log"
	"github.com/czh0526/blockchain/p2p/discover"
)

const (
	defaultDialTimeout = 15 * time.Second

	maxActiveDialTasks     = 16
	defaultMaxPendingPeers = 50
	defaultDialRatio       = 3

	frameReadTimeout  = 30 * time.Second
	frameWriteTimeout = 20 * time.Second
)

var errServerStopped = errors.New("server stopped")

type transport interface {
	MsgReadWriter

	doEncHandshake(prv *ecdsa.PrivateKey, dialDest *discover.Node) (discover.NodeID, error)
	doProtoHandshake(our *protoHandshake) (their *protoHandshake, err error)

	close(err error)
}

type conn struct {
	fd net.Conn
	transport
	flags connFlag
	cont  chan error
	id    discover.NodeID
	caps  []Cap
	name  string
}

func (c *conn) is(f connFlag) bool {
	return c.flags&f != 0
}

type Config struct {
	PrivateKey      *ecdsa.PrivateKey `toml:"-"`
	MaxPeers        int
	MaxPendingPeers int        `toml:",omitempty"`
	Name            string     `toml:"-"`
	Protocols       []Protocol `tom:"-"`
	ListenAddr      string
	Dialer          NodeDialer `toml:"-"`
	NoDial          bool       `toml:",omitempty"`
	Logger          log.Logger `toml:",omitempty"`
}

type Server struct {
	Config
	newTransport func(net.Conn) transport
	newPeerHook  func(*Peer)
	running      bool

	lastLookup time.Time
	ntab       discoverTable

	listener      net.Listener
	ourHandshake  *protoHandshake
	posthandshake chan *conn

	addstatic    chan *discover.Node
	removestatic chan *discover.Node
	addpeer      chan *conn
	delpeer      chan peerDrop

	loopWG sync.WaitGroup
	log    log.Logger
	quit   chan struct{}
}

type peerDrop struct {
	*Peer
	err       error
	requested bool
}

func (srv *Server) Start() (err error) {
	if srv.running {
		return errors.New("server already running")
	}

	srv.running = true
	srv.log = srv.Config.Logger
	if srv.log == nil {
		srv.log = log.New()
	}

	// 检查私钥
	if srv.PrivateKey == nil {
		return fmt.Errorf("Server.PrivateKey must be set to a non-nil key")
	}

	// 检查连接生成器
	if srv.newTransport == nil {
		srv.newTransport = newRLPX
	}

	// 检查拨号器
	if srv.Dialer == nil {
		srv.Dialer = TCPDialer{&net.Dialer{Timeout: defaultDialTimeout}}
	}

	srv.quit = make(chan struct{})
	srv.addstatic = make(chan *discover.Node)
	srv.removestatic = make(chan *discover.Node)
	srv.posthandshake = make(chan *conn)
	srv.addpeer = make(chan *conn)
	srv.delpeer = make(chan peerDrop)

	var (
		conn     *net.UDPConn
		realaddr *net.UDPAddr
		//sconn     *sharedUDPConn
	//unhandled chan discover.ReadPacket
	)

	// readAddr
	addr, err := net.ResolveUDPAddr("udp", srv.ListenAddr)
	if err != nil {
		return err
	}

	conn, err = net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	realaddr = conn.LocalAddr().(*net.UDPAddr)

	// bootnodes
	bootnodes := make([]*discover.Node, 0, 1)
	if node, err := discover.ParseNode("enode://0f231b57ffe1a1b69dcd5e6fbed3ea4bc2e903eae6e6295aca2abf92e264652945219403ecdb99a8523c485e6dfd05f1124d332feb89397820843ee7ed2b3a1f@139.199.100.150:30308"); err == nil {
		bootnodes = append(bootnodes, node)
	}

	cfg := discover.Config{
		PrivateKey:   srv.PrivateKey,
		AnnounceAddr: realaddr,
		//NodeDBPath: srv.NodeDatabase,
		//NetRestrict: srv.Netstrict,
		Bootnodes: bootnodes,
		//Unhandled: unhandled,
	}

	// DiscoveryTable
	ntab, err := discover.ListenUDP(conn, cfg)
	if err != nil {
		return err
	}
	srv.ntab = ntab

	dialer := newDialState([]*discover.Node{}, bootnodes, srv.ntab, 5)

	srv.ourHandshake = &protoHandshake{
		Version: baseProtocolVersion,
		Name:    srv.Name,
		ID:      discover.PubkeyID(&srv.PrivateKey.PublicKey),
	}
	for _, p := range srv.Protocols {
		srv.ourHandshake.Caps = append(srv.ourHandshake.Caps, p.cap())
	}

	// 启动 TCP 端口监听
	if srv.ListenAddr != "" {
		if err := srv.startListening(); err != nil {
			return err
		}
	}
	if srv.NoDial && srv.ListenAddr == "" {
		srv.log.Warn("P2P server will be useless, neither dialing nor listening")
	}

	srv.loopWG.Add(1)
	// 启动拨号循环
	go srv.run(dialer)
	srv.running = true
	return nil
}

func (srv *Server) startListening() error {
	listener, err := net.Listen("tcp", srv.ListenAddr)
	if err != nil {
		return err
	}

	laddr := listener.Addr().(*net.TCPAddr)
	srv.ListenAddr = laddr.String()
	srv.listener = listener
	srv.loopWG.Add(1)

	srv.log.Info(fmt.Sprintf("[Srv]: listenLoop() at %s —— RLPx listener up.", srv.ListenAddr))
	go srv.listenLoop()
	if !laddr.IP.IsLoopback() {
		srv.loopWG.Add(1)
		go func() {
			srv.loopWG.Done()
		}()
	}
	return nil
}

type dialer interface {
	newTasks(running int, peers map[discover.NodeID]*Peer, now time.Time) []task
	taskDone(task, time.Time)
	addStatic(*discover.Node)
	removeStatic(*discover.Node)
}

func (srv *Server) run(dialstate dialer) {
	defer srv.loopWG.Done()
	var (
		peers        = make(map[discover.NodeID]*Peer)
		inboundCount = 0
		taskdone     = make(chan task, maxActiveDialTasks)
		runningTasks []task
		queuedTasks  []task
	)

	delTask := func(t task) {
		for i := range runningTasks {
			if runningTasks[i] == t {
				runningTasks = append(runningTasks[:i], runningTasks[i+1:]...)
				break
			}
		}
	}
	startTasks := func(ts []task) (rest []task) {
		i := 0
		for ; len(runningTasks) < maxActiveDialTasks && i < len(ts); i++ {
			t := ts[i]
			srv.log.Debug(fmt.Sprintf("[Srv]: startTasks(), task = %s.", t))
			go func() {
				t.Do(srv)
				taskdone <- t
			}()
			runningTasks = append(runningTasks, t)
		}
		return ts[i:]
	}
	scheduleTasks := func() {
		// 进行一次 queuedTasks ==> runningTasks 的任务调度
		queuedTasks = append(queuedTasks[:0], startTasks(queuedTasks)...)
		srv.log.Debug(fmt.Sprintf("[Srv]: runningTasks = %v, maxActiveDialTasks = %v", len(runningTasks), maxActiveDialTasks))
		if len(runningTasks) < maxActiveDialTasks {
			// runningTasks 没有放满，创建新任务，放入 runningTasks queuedTasks 中
			nt := dialstate.newTasks(len(runningTasks)+len(queuedTasks), peers, time.Now())
			queuedTasks = append(queuedTasks, startTasks(nt)...)
		}
	}

	log.Info(fmt.Sprintf("[Srv]: run() loop up —— 监听并控制 RLPx 连接建立."))

running:
	for {
		scheduleTasks()

		select {
		case <-srv.quit:
			break running
		case n := <-srv.addstatic:
			log.Debug(fmt.Sprintf("[Srv]: run() loop  ---- signal 'addstatic', n = 0x%x \n", n.ID.Bytes()[:4]))
			go func() {
				fd, err := srv.Dialer.Dial(n)
				if err != nil {
					log.Error(fmt.Sprintf("[Srv]: run() loop --- srv dial %v error: %v \n", n.String(), err))
					return
				}
				if err = srv.SetupConn(fd, staticDialedConn, n); err != nil {
					log.Error(fmt.Sprintf("[Srv]: srv SetupConn error: %v \n", err))
					return
				}
			}()
		case n := <-srv.removestatic:
			log.Debug(fmt.Sprintf("[Srv]: run() loop  ---- signal 'removestatic', n = 0x%x \n", n.ID.Bytes()[:4]))
		case t := <-taskdone:
			srv.log.Debug(fmt.Sprintf("[Srv]: run() loop  --- signal 'taskdone', task = %s", t))
			dialstate.taskDone(t, time.Now())
			delTask(t)
		case c := <-srv.posthandshake:
			srv.log.Debug(fmt.Sprintf("[Srv]: run() loop  --- signal 'posthandshake', 0x%x...", c.id.Bytes()[:4]))
			select {
			case c.cont <- srv.encHandshakeChecks(peers, inboundCount, c):
			case <-srv.quit:
				break running
			}
		case c := <-srv.addpeer:
			srv.log.Debug(fmt.Sprintf("[Srv]: run() loop  --- signal 'addpeer' 0x%x...", c.id.Bytes()[:4]))
			err := srv.protoHandshakeChecks(peers, inboundCount, c)
			if err == nil {
				p := newPeer(c, srv.Protocols)
				srv.log.Debug("Adding p2p peer", "name", c.name, "addr", c.fd.RemoteAddr())
				go srv.runPeer(p)
				peers[c.id] = p
			}
			select {
			case c.cont <- err:
			case <-srv.quit:
				break running
			}
		}
	}
}

func (srv *Server) runPeer(p *Peer) {
	log.Info(fmt.Sprintf("[Srv]: runPeer() ==> 0x%x...@%v", p.ID().Bytes()[:4], p.rw.fd.RemoteAddr()))
	if srv.newPeerHook != nil {
		srv.newPeerHook(p)
	}

	remoteRequested, err := p.run()
	srv.delpeer <- peerDrop{p, err, remoteRequested}
}

func (srv *Server) Self() *discover.Node {
	if !srv.running {
		return &discover.Node{IP: net.ParseIP("0.0.0.0")}
	}
	return srv.makeSelf(srv.listener)
}

func (srv *Server) makeSelf(listener net.Listener) *discover.Node {
	if listener == nil {
		return &discover.Node{IP: net.ParseIP("0.0.0.0"), ID: discover.PubkeyID(&srv.PrivateKey.PublicKey)}
	}
	addr := listener.Addr().(*net.TCPAddr)
	return &discover.Node{
		ID:  discover.PubkeyID(&srv.PrivateKey.PublicKey),
		IP:  addr.IP,
		TCP: uint16(addr.Port),
	}
}

func (srv *Server) encHandshakeChecks(peers map[discover.NodeID]*Peer, inboundCount int, c *conn) error {
	switch {
	case peers[c.id] != nil:
		return DiscAlreadyConnected
	case c.id == srv.Self().ID:
		return DiscSelf
	default:
		return nil
	}
}

func (srv *Server) protoHandshakeChecks(peers map[discover.NodeID]*Peer, inboundCount int, c *conn) error {
	return srv.encHandshakeChecks(peers, inboundCount, c)
}

type tempError interface {
	Temporary() bool
}

func (srv *Server) listenLoop() {
	defer srv.loopWG.Done()

	tokens := defaultMaxPendingPeers
	if srv.MaxPendingPeers > 0 {
		tokens = srv.MaxPendingPeers
	}
	slots := make(chan struct{}, tokens)
	for i := 0; i < tokens; i++ {
		slots <- struct{}{}
	}

	for {
		<-slots
		srv.log.Trace("pending queue decrease 1")

		var (
			fd  net.Conn
			err error
		)
		for {
			fd, err = srv.listener.Accept()
			if tempErr, ok := err.(tempError); ok && tempErr.Temporary() {
				srv.log.Debug("Temporary read error", "err", err)
				continue
			} else if err != nil {
				srv.log.Debug("Read error", "err", err)
				return
			}
			break
		}

		srv.log.Info(fmt.Sprintf("tcp accept conn, fd = %v.", fd))
		go func() {
			srv.SetupConn(fd, inboundConn, nil)
			slots <- struct{}{}
			srv.log.Info("pending queue increase 1")
		}()
	}
}

func (srv *Server) Stop() {
	if !srv.running {
		return
	}
	srv.running = false
	if srv.listener != nil {
		srv.listener.Close()
	}
	close(srv.quit)
}

func (srv *Server) SetupConn(fd net.Conn, flags connFlag, dialDest *discover.Node) error {
	srv.log.Info(fmt.Sprintf("[Srv]: SetupConn:  ==> %v", fd.RemoteAddr()))

	c := &conn{fd: fd, transport: srv.newTransport(fd), flags: flags, cont: make(chan error)}
	err := srv.setupConn(c, flags, dialDest)
	if err != nil {
		c.close(err)
		srv.log.Trace("Setup connection failed", "id", c.id, "err", err)
	}
	return err
}

func (srv *Server) setupConn(c *conn, flags connFlag, dialDest *discover.Node) error {
	running := srv.running
	if !running {
		return errServerStopped
	}

	var err error

	// RLPx handshake
	if c.id, err = c.doEncHandshake(srv.PrivateKey, dialDest); err != nil {
		srv.log.Trace("[Srv]: setupConn() --- Failed RLPx handshake", "addr", c.fd.RemoteAddr(), "conn", c.flags, "err", err)
		return err
	}
	srv.log.Info(fmt.Sprintf("[Srv]: setupConn() --- RLPx handshake done. id = 0x%x...", c.id.Bytes()[:4]))
	if dialDest != nil && c.id != dialDest.ID {
		return DiscUnexpectedIdentity
	}
	err = srv.checkpoint(c, srv.posthandshake)
	if err != nil {
		return err
	}

	// proto handshake
	phs, err := c.doProtoHandshake(srv.ourHandshake)
	if err != nil {
		srv.log.Trace("[Srv]: Failed proto handshake", "err", err)
		return err
	}
	if phs.ID != c.id {
		return DiscUnexpectedIdentity
	}
	srv.log.Info(fmt.Sprintf("[Srv]: proto handshake done. phs = 0x%x...", phs.ID.Bytes()[:4]))

	c.caps, c.name = phs.Caps, phs.Name
	err = srv.checkpoint(c, srv.addpeer)
	if err != nil {
		return err
	}

	return nil
}

func (srv *Server) checkpoint(c *conn, stage chan<- *conn) error {
	select {
	case stage <- c:
	case <-srv.quit:
		return errServerStopped
	}
	select {
	case err := <-c.cont:
		return err
	case <-srv.quit:
		return errServerStopped
	}
}

func (srv *Server) AddPeer(node *discover.Node) {
	fmt.Println("AddPeer()")
	select {
	case srv.addstatic <- node:
		fmt.Println("Write node to addstatic.")
	case <-srv.quit:
	}
}

type sharedUDPConn struct {
	*net.UDPConn
	unhandled chan discover.ReadPacket
}

type NodeInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Enode string `json:"enode"`
	IP    string `json:"ip"`
	Ports struct {
		Discovery int `json:"discovery"`
		Listener  int `json:"listener"`
	} `json:"ports"`
	ListenAddr string                 `json:"listenAddr"`
	Protocols  map[string]interface{} `json:"protocols"`
}
