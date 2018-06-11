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

	ntab         discoverTable
	listener     net.Listener
	ourHandshake *protoHandshake

	quit          chan struct{}
	addstatic     chan *discover.Node
	removestatic  chan *discover.Node
	posthandshake chan *conn
	addpeer       chan *conn
	delpeer       chan peerDrop
	loopWG        sync.WaitGroup
	log           log.Logger
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

	/**
	"enode://a979fb575495b8d6db44f750317d0f4622bf4c2aa3365d6af7c284339968eef29b69ad0dce72a4d8db5ebb4968de0e3bec910127f134779fbcb0cb6d3331163c@52.16.188.185:30303", // IE
	"enode://3f1d12044546b76342d59d4a05532c14b85aa669704bfe1f864fe079415aa2c02d743e03218e57a33fb94523adb54032871a6c51b2cc5514cb7c7e35b3ed0a99@13.93.211.84:30303",  // US-WEST
	"enode://78de8a0916848093c73790ead81d1928bec737d565119932b98c6b100d944b7a95e94f847f689fc723399d2e31129d182f7ef3863f2b4c820abbf3ab2722344d@191.235.84.50:30303", // BR
	"enode://158f8aab45f6d19c6cbf4a089c2670541a8da11978a2f90dbf6a502a4a3bab80d288afdbeb7ec0ef6d92de563767f3b1ea9e8e334ca711e9f8e2df5a0385e8e6@13.75.154.138:30303", // AU
	"enode://1118980bf48b0a3640bdba04e0fe78b1add18e1cd99bf22d53daac1fd9972ad650df52176e7c7d89d1114cfef2bc23a2959aa54998a46afcf7d91809f0855082@52.74.57.123:30303",  // SG

	*/
	// bootnodes
	bootnodes := make([]*discover.Node, 0, 5)
	if node, err := discover.ParseNode("enode://a979fb575495b8d6db44f750317d0f4622bf4c2aa3365d6af7c284339968eef29b69ad0dce72a4d8db5ebb4968de0e3bec910127f134779fbcb0cb6d3331163c@52.16.188.185:30303"); err == nil {
		bootnodes = append(bootnodes, node)
	}
	if node, err := discover.ParseNode("enode://3f1d12044546b76342d59d4a05532c14b85aa669704bfe1f864fe079415aa2c02d743e03218e57a33fb94523adb54032871a6c51b2cc5514cb7c7e35b3ed0a99@13.93.211.84:30303"); err == nil {
		bootnodes = append(bootnodes, node)
	}
	if node, err := discover.ParseNode("enode://78de8a0916848093c73790ead81d1928bec737d565119932b98c6b100d944b7a95e94f847f689fc723399d2e31129d182f7ef3863f2b4c820abbf3ab2722344d@191.235.84.50:30303"); err == nil {
		bootnodes = append(bootnodes, node)
	}
	if node, err := discover.ParseNode("enode://158f8aab45f6d19c6cbf4a089c2670541a8da11978a2f90dbf6a502a4a3bab80d288afdbeb7ec0ef6d92de563767f3b1ea9e8e334ca711e9f8e2df5a0385e8e6@13.75.154.138:30303"); err == nil {
		bootnodes = append(bootnodes, node)
	}
	if node, err := discover.ParseNode("enode://1118980bf48b0a3640bdba04e0fe78b1add18e1cd99bf22d53daac1fd9972ad650df52176e7c7d89d1114cfef2bc23a2959aa54998a46afcf7d91809f0855082@52.74.57.123:30303"); err == nil {
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
	ntab, err := discover.ListenUDP(conn, cfg)
	if err != nil {
		return err
	}
	srv.ntab = ntab

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

	// 启动逻辑循环
	srv.loopWG.Add(1)
	go srv.run()
	srv.running = true
	return nil
}

func (srv *Server) startListening() error {
	listener, err := net.Listen("tcp", srv.ListenAddr)
	if err != nil {
		return err
	}
	srv.log.Info(fmt.Sprintf("tcp listen at %s", srv.ListenAddr))

	laddr := listener.Addr().(*net.TCPAddr)
	srv.ListenAddr = laddr.String()
	srv.listener = listener
	srv.loopWG.Add(1)
	go srv.listenLoop()
	if !laddr.IP.IsLoopback() {
		srv.loopWG.Add(1)
		go func() {
			srv.loopWG.Done()
		}()
	}
	return nil
}

func (srv *Server) run() {
	defer srv.loopWG.Done()
	var (
		peers        = make(map[discover.NodeID]*Peer)
		inboundCount = 0
	)

running:
	for {
		select {
		case <-srv.quit:
			break running
		case n := <-srv.addstatic:
			fmt.Printf("    ---- receive 'addstatic', n = 0x%x \n", n.ID.Bytes()[:4])
			go func() {
				fd, err := srv.Dialer.Dial(n)
				if err != nil {
					fmt.Printf("srv dial %v error: %v \n", n.String(), err)
					return
				}
				if err = srv.SetupConn(fd, staticDialedConn, n); err != nil {
					fmt.Printf("srv SetupConn error: %v \n", err)
					return
				}
			}()
		case c := <-srv.posthandshake:
			srv.log.Info(fmt.Sprintf("    --- server get 'posthandshake' 0x%x...", c.id.Bytes()[:4]))
			select {
			case c.cont <- srv.encHandshakeChecks(peers, inboundCount, c):
			case <-srv.quit:
				break running
			}
		case c := <-srv.addpeer:
			srv.log.Info(fmt.Sprintf("    --- server get 'addpeer' 0x%x...", c.id.Bytes()[:4]))
			err := srv.protoHandshakeChecks(peers, inboundCount, c)
			if err == nil {
				p := newPeer(c, srv.Protocols)
				srv.log.Info("Adding p2p peer", "name", c.name, "addr", c.fd.RemoteAddr())
				go srv.runPeer(p)
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
	srv.log.Info("RLPx listener up")

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
		srv.log.Info("pending queue decrease 1")

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
	srv.log.Info(fmt.Sprintf("Server SetupConn: fd = %v", fd))

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
		srv.log.Trace("Failed RLPx handshake", "addr", c.fd.RemoteAddr(), "conn", c.flags, "err", err)
		return err
	}
	srv.log.Info(fmt.Sprintf("RLPx handshake done. id = 0x%x...", c.id.Bytes()[:4]))
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
		srv.log.Trace("Failed proto handshake", "err", err)
	}
	if phs.ID != c.id {
		return DiscUnexpectedIdentity
	}
	srv.log.Info(fmt.Sprintf("proto handshake done. phs = 0x%x...", phs.ID.Bytes()[:4]))

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
