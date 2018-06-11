package discover

import (
	"bytes"
	"container/list"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/czh0526/blockchain/crypto"
	"github.com/czh0526/blockchain/log"
	"github.com/czh0526/blockchain/p2p/netutil"
	"github.com/czh0526/blockchain/rlp"
)

const Version = 4

var (
	errPacketTooSmall   = errors.New("too small")
	errBadHash          = errors.New("bad hash")
	errExpired          = errors.New("expired")
	errUnsolicitedReply = errors.New("unsolicited reply")
	errUnknownNode      = errors.New("unknown node")
	errTimeout          = errors.New("RPC timeout")
	errClockWarp        = errors.New("reply deadline too far in the future")
	errClosed           = errors.New("socket closed")
)

const (
	respTimeout = 500 * time.Millisecond
	expiration  = 20 * time.Second

	ntpFailureThreshold = 32
	ntpWarningCooldown  = 10 * time.Minute
	driftThreshold      = 10 * time.Second
)

const (
	pingPacket = iota + 1
	pongPacket
	findnodePacket
	neighborsPacket
)

type ReadPacket struct {
	Data []byte
	Addr *net.UDPAddr
}

type pending struct {
	from     NodeID
	ptype    byte
	deadline time.Time
	callback func(resp interface{}) (done bool) // 返回值说明 (true：pending队列中保留消息, false: pending队列中删除消息)
	errc     chan<- error
}

type conn interface {
	ReadFromUDP(b []byte) (n int, addr *net.UDPAddr, err error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (n int, err error)
	Close() error
	LocalAddr() net.Addr
}

type reply struct {
	from    NodeID
	ptype   byte
	data    interface{}
	matched chan<- bool
}

type Config struct {
	PrivateKey   *ecdsa.PrivateKey
	AnnounceAddr *net.UDPAddr
	NodeDBPath   string
	NetRestrict  *netutil.Netlist
	Bootnodes    []*Node
	Unhandled    chan<- ReadPacket
}

type udp struct {
	conn        conn
	netrestrict *netutil.Netlist
	priv        *ecdsa.PrivateKey
	ourEndpoint rpcEndpoint
	addpending  chan *pending
	gotreply    chan reply
	closing     chan struct{}

	*Table
}

func ListenUDP(c conn, cfg Config) (*Table, error) {
	tab, _, err := newUDP(c, cfg)
	if err != nil {
		return nil, err
	}
	log.Info("UDP listener up", "self", tab.self)
	return tab, nil
}

func newUDP(c conn, cfg Config) (*Table, *udp, error) {
	udp := &udp{
		conn:        c,
		priv:        cfg.PrivateKey,
		netrestrict: cfg.NetRestrict,
		closing:     make(chan struct{}),
		gotreply:    make(chan reply),
		addpending:  make(chan *pending),
	}
	realaddr := c.LocalAddr().(*net.UDPAddr)
	if cfg.AnnounceAddr != nil {
		realaddr = cfg.AnnounceAddr
	}

	udp.ourEndpoint = makeEndpoint(realaddr, uint16(realaddr.Port))
	tab, err := newTable(udp, PubkeyID(&cfg.PrivateKey.PublicKey), realaddr, cfg.NodeDBPath, cfg.Bootnodes)
	if err != nil {
		return nil, nil, err
	}
	udp.Table = tab

	go udp.loop()
	go udp.readLoop(cfg.Unhandled)
	return udp.Table, udp, nil
}

func (t *udp) loop() {
	var (
		plist        = list.New()
		timeout      = time.NewTimer(0)
		nextTimeout  *pending
		contTimeouts = 0
		ntpWarnTime  = time.Unix(0, 0)
	)

	<-timeout.C
	defer timeout.Stop()

	resetTimeout := func() {
		if plist.Front() == nil || nextTimeout == plist.Front().Value {
			return
		}

		now := time.Now()
		for el := plist.Front(); el != nil; el = el.Next() {
			nextTimeout = el.Value.(*pending)
			if dist := nextTimeout.deadline.Sub(now); dist < 2*respTimeout {
				timeout.Reset(dist)
				return
			}
			nextTimeout.errc <- errClockWarp
			plist.Remove(el)
		}
		nextTimeout = nil
		timeout.Stop()
	}

	log.Info(fmt.Sprintf("start udp loop() localAddr = %v, realAddr = %v", t.conn.LocalAddr(), t.ourEndpoint))
	for {
		resetTimeout()

		select {
		case <-t.closing:
			// 设置 pending 对象的错误值
			for el := plist.Front(); el != nil; el = el.Next() {
				el.Value.(*pending).errc <- errClosed
			}
			return

		case p := <-t.addpending:
			// 设置 pending 对象的过期时间
			p.deadline = time.Now().Add(respTimeout)
			// 插入队列
			plist.PushBack(p)

		case r := <-t.gotreply:
			var matched bool
			for el := plist.Front(); el != nil; el = el.Next() {
				p := el.Value.(*pending)
				// 找到匹配的 pending, 激活回调函数
				if p.from == r.from && p.ptype == r.ptype {
					matched = true
					if p.callback(r.data) {
						p.errc <- nil
						plist.Remove(el)
					}

					contTimeouts = 0
				}
			}
			// 设置reply对象的结果
			r.matched <- matched

		case now := <-timeout.C:
			log.Trace(fmt.Sprintf("timeout 定时器调度: plist len = %d, %s", plist.Len(), now.Format("2006-01-02 15:04:05")))
			nextTimeout = nil

			// 删除过期的 pending
			for el := plist.Front(); el != nil; el = el.Next() {
				p := el.Value.(*pending)
				if now.After(p.deadline) || now.Equal(p.deadline) {
					p.errc <- errTimeout
					plist.Remove(el)
					contTimeouts++
					log.Trace(fmt.Sprintf("删除 pending 对象, ptype = %v, node id = 0x%x..., contTimeouts = %v, plist len = %v.", p.ptype, p.from[:4], contTimeouts, plist.Len()))
				}
			}

			if contTimeouts > ntpFailureThreshold {
				if time.Since(ntpWarnTime) >= ntpWarningCooldown {
					ntpWarnTime = time.Now()
					go checkClockDrift()
				}
				contTimeouts = 0
			}
		}
	}
}

func (t *udp) readLoop(unhandled chan<- ReadPacket) {
	defer t.conn.Close()
	if unhandled != nil {
		defer close(unhandled)
	}

	log.Info(fmt.Sprintf("start udp readLoop(), localAddr = %v, realAddr = %v", t.conn.LocalAddr(), t.ourEndpoint))
	buf := make([]byte, 1280)
	for {
		// 将UDP报文中的数据读入buf
		nbytes, from, err := t.conn.ReadFromUDP(buf)
		if netutil.IsTemporaryError(err) {
			log.Debug("Temporary UDP read error", "err", err)
			continue
		} else if err != nil {
			log.Debug("UDP read error", "err", err)
			return
		}

		// handlePacket 处理收到的消息
		if t.handlePacket(from, buf[:nbytes]) != nil && unhandled != nil {
			select {
			case unhandled <- ReadPacket{buf[:nbytes], from}:
				// 如果 udp 处理不了这条消息，投递到 unhandled chan
			default:
			}
		}
	}
}

// 无需进行pending操作
func (t *udp) send(toaddr *net.UDPAddr, ptype byte, req packet) ([]byte, error) {
	// packet ==> []byte
	packet, hash, err := encodePacket(t.priv, ptype, req)
	if err != nil {
		return hash, err
	}
	// write bytes
	return hash, t.write(toaddr, req.name(), packet)
}

func (t *udp) write(toaddr *net.UDPAddr, what string, packet []byte) error {
	_, err := t.conn.WriteToUDP(packet, toaddr)
	log.Trace(fmt.Sprintf(">> %s, addr = %v, err = %v", what, toaddr, err))
	return err
}

func (t *udp) handlePacket(from *net.UDPAddr, buf []byte) error {
	// 包解码
	packet, fromID, hash, err := decodePacket(buf)
	if err != nil {
		log.Debug(fmt.Sprintf("<< %s, addr = %v, err = %v", packet.name(), from, err))
		return err
	}
	// 包处理
	err = packet.handle(t, from, fromID, hash)
	log.Trace(fmt.Sprintf("<< %s, addr = %v, err = %v", packet.name(), from, err))
	return err
}

// 向 toaddr 发送 ping/v4， 并等到 pong 返回
func (t *udp) ping(toid NodeID, toaddr *net.UDPAddr) error {
	// 构建ping对象
	req := &ping{
		Version:    Version,
		From:       t.ourEndpoint,
		To:         makeEndpoint(toaddr, 0),
		Expiration: uint64(time.Now().Add(expiration).Unix()),
	}
	// packet => []byte
	packet, hash, err := encodePacket(t.priv, pingPacket, req)
	if err != nil {
		return err
	}
	// add pending object
	errc := t.pending(toid, pongPacket, func(p interface{}) bool {
		return bytes.Equal(p.(*pong).ReplyTok, hash)
	})
	// write bytes
	t.write(toaddr, req.name(), packet)
	// 等待pong返回
	return <-errc
}

// 等待来自 from 节点的 ping 消息
func (t *udp) waitping(from NodeID) error {
	return <-t.pending(from, pingPacket, func(interface{}) bool { return true })
}

func (t *udp) findnode(toid NodeID, toaddr *net.UDPAddr, target NodeID) ([]*Node, error) {
	nodes := make([]*Node, 0, bucketSize)
	nreceived := 0
	// 处理 neighbors 返回消息
	errc := t.pending(toid, neighborsPacket, func(r interface{}) bool {
		reply := r.(*neighbors)
		for _, rn := range reply.Nodes {
			nreceived++
			n, err := t.nodeFromRPC(toaddr, rn)
			if err != nil {
				log.Trace("Invalid neighbor node received", "ip", rn.IP, "addr", toaddr, "err", err)
				continue
			}
			nodes = append(nodes, n)
		}
		return nreceived >= bucketSize
	})
	// 发送 findnode 消息
	t.send(toaddr, findnodePacket, &findnode{
		Target:     target,
		Expiration: uint64(time.Now().Add(expiration).Unix()),
	})
	// 等待 neighbors 消息到来
	err := <-errc
	return nodes, err
}

func (t *udp) pending(id NodeID, ptype byte, callback func(interface{}) bool) <-chan error {
	ch := make(chan error, 1)
	p := &pending{from: id, ptype: ptype, callback: callback, errc: ch}
	select {
	case t.addpending <- p:
		//log.Trace(fmt.Sprintf("挂起消息：ptype = %v, node id = 0x%x...", ptype, id[:4]))
	case <-t.closing:
		ch <- errClosed
	}
	return ch
}

func (t *udp) handleReply(from NodeID, ptype byte, req packet) bool {
	matched := make(chan bool, 1)
	select {
	case t.gotreply <- reply{from, ptype, req, matched}:
		return <-matched
	case <-t.closing:
		return false
	}
}

func (t *udp) close() {
	close(t.closing)
	t.conn.Close()
}

type (
	ping struct {
		Version    uint
		From, To   rpcEndpoint
		Expiration uint64
		Rest       []rlp.RawValue `rlp:"tail`
	}

	pong struct {
		To         rpcEndpoint
		ReplyTok   []byte
		Expiration uint64
		Rest       []rlp.RawValue `rlp:"tail"`
	}

	findnode struct {
		Target     NodeID
		Expiration uint64
		Rest       []rlp.RawValue `rlp:"tail"`
	}

	neighbors struct {
		Nodes      []rpcNode
		Expiration uint64
		Rest       []rlp.RawValue `rlp:"tail`
	}
)

func (req *ping) handle(t *udp, from *net.UDPAddr, fromID NodeID, mac []byte) error {
	// 判断到来的 ping 消息是否过期
	if expired(req.Expiration) {
		return errExpired
	}
	// 发回 pong 消息
	t.send(from, pongPacket, &pong{
		To:         makeEndpoint(from, req.From.TCP),
		ReplyTok:   mac,
		Expiration: uint64(time.Now().Add(expiration).Unix()),
	})
	if !t.handleReply(fromID, pingPacket, req) {
		// 如果pending队列中没有ping ==> 没有调用过 waitping()
		go t.bond(true, fromID, from, req.From.TCP)
	}
	return nil
}

func (req *ping) name() string { return "PING/v4" }

func (req *pong) handle(t *udp, from *net.UDPAddr, fromID NodeID, mac []byte) error {
	if expired(req.Expiration) {
		return errExpired
	}
	if !t.handleReply(fromID, pongPacket, req) {
		return errUnsolicitedReply
	}
	return nil
}

func (req *pong) name() string { return "PONG/v4" }

func (req *findnode) handle(t *udp, from *net.UDPAddr, fromID NodeID, mac []byte) error {
	if expired(req.Expiration) {
		return errExpired
	}
	if !t.db.hasBond(fromID) {
		return errUnknownNode
	}
	// 请求节点的 Hash
	target := crypto.Keccak256Hash(req.Target[:])
	// 查找本地数据库
	t.mutex.Lock()
	closest := t.closest(target, bucketSize).entries
	t.mutex.Unlock()

	// 填充返回对象
	p := neighbors{Expiration: uint64(time.Now().Add(expiration).Unix())}
	var sent bool
	for _, n := range closest {
		// 凑足 maxNeighbors 个数据再发送，减少网络拥堵。
		if netutil.CheckRelayIP(from.IP, n.IP) == nil {
			p.Nodes = append(p.Nodes, nodeToRPC(n))
		}
		if len(p.Nodes) == maxNeighbors {
			t.send(from, neighborsPacket, &p)
			p.Nodes = p.Nodes[:0]
			sent = true
		}
	}

	// 把最后剩下的发送出去
	if len(p.Nodes) > 0 || !sent {
		t.send(from, neighborsPacket, &p)
	}

	return nil
}

func (req *findnode) name() string { return "FINDNODE/v4" }

func (req *neighbors) handle(t *udp, from *net.UDPAddr, fromID NodeID, mac []byte) error {
	if expired(req.Expiration) {
		return errExpired
	}
	if !t.handleReply(fromID, neighborsPacket, req) {
		return errUnsolicitedReply
	}
	return nil
}

func (req *neighbors) name() string { return "NEIGHBORS/v4" }

func nodeToRPC(n *Node) rpcNode {
	return rpcNode{ID: n.ID, IP: n.IP, UDP: n.UDP, TCP: n.TCP}
}

func (t *udp) nodeFromRPC(sender *net.UDPAddr, rn rpcNode) (*Node, error) {
	if rn.UDP <= 1024 {
		return nil, errors.New("low port")
	}
	if err := netutil.CheckRelayIP(sender.IP, rn.IP); err != nil {
		return nil, err
	}
	if t.netrestrict != nil && !t.netrestrict.Contains(rn.IP) {
		return nil, errors.New("not contained in netrestrict whitelist")
	}
	n := NewNode(rn.ID, rn.IP, rn.UDP, rn.TCP)
	err := n.validateComplete()
	return n, err
}

type packet interface {
	handle(t *udp, from *net.UDPAddr, fromID NodeID, mac []byte) error
	name() string
}

const (
	macSize  = 256 / 8 // 32 bytes
	sigSize  = 520 / 8 // 65 bytes
	headSize = macSize + sigSize
)

var (
	headSpace    = make([]byte, headSize)
	maxNeighbors int
)

// 初始化 maxNeighbors 变量，确保neighbors消息的大小不超过 UDP包容量(1280)
func init() {
	p := neighbors{Expiration: ^uint64(0)}
	// 最大尺寸的节点，用IPv6算
	maxSizeNode := rpcNode{IP: make(net.IP, 16), UDP: ^uint16(0), TCP: ^uint16(0)}
	for n := 0; ; n++ {
		p.Nodes = append(p.Nodes, maxSizeNode)
		size, _, err := rlp.EncodeToReader(p)
		if err != nil {
			panic("cannot encode: " + err.Error())
		}
		if headSize+size+1 >= 1280 {
			maxNeighbors = n
			break
		}
	}
}

// packet = hash + sig + content
// 先做签名，后做hash
func encodePacket(priv *ecdsa.PrivateKey, ptype byte, req interface{}) (packet, hash []byte, err error) {
	// 构建一个Writer
	b := new(bytes.Buffer)
	// 写入空白的 mac + sig
	b.Write(headSpace)
	// 写入 ptype
	b.WriteByte(ptype)
	// 写入 req
	if err := rlp.Encode(b, req); err != nil {
		log.Error("Can't encode discv4 packet", "err", err)
		return nil, nil, err
	}

	// 计算签名
	packet = b.Bytes()
	sig, err := crypto.Sign(crypto.Keccak256(packet[headSize:]), priv)
	if err != nil {
		log.Error("Can't sign discv4 packet", "err", err)
		return nil, nil, err
	}

	// 设置 sig
	copy(packet[macSize:], sig)
	// 计算hash, 设置mac
	hash = crypto.Keccak256(packet[macSize:])
	copy(packet, hash)
	return packet, hash, nil
}

func decodePacket(buf []byte) (packet, NodeID, []byte, error) {
	if len(buf) < headSize+1 {
		return nil, NodeID{}, nil, errPacketTooSmall
	}

	// 分别取出 hash, sig, sigdata 进行验证
	hash, sig, sigdata := buf[:macSize], buf[macSize:headSize], buf[headSize:]

	// 验证 hash
	shouldhash := crypto.Keccak256(buf[macSize:])
	if !bytes.Equal(hash, shouldhash) {
		return nil, NodeID{}, nil, errBadHash
	}

	// 验证 sig
	fromID, err := recoverNodeID(crypto.Keccak256(buf[headSize:]), sig)
	if err != nil {
		return nil, NodeID{}, hash, err
	}

	// 确定消息类型
	var req packet
	switch ptype := sigdata[0]; ptype {
	case pingPacket:
		req = new(ping)
	case pongPacket:
		req = new(pong)
	case findnodePacket:
		req = new(findnode)
	case neighborsPacket:
		req = new(neighbors)
	default:
		return nil, fromID, hash, fmt.Errorf("unknown type: %d", ptype)
	}
	// 反序列化消息
	s := rlp.NewStream(bytes.NewReader(sigdata[1:]), 0)
	err = s.Decode(req)
	return req, fromID, hash, err
}

type (
	rpcNode struct {
		IP  net.IP
		UDP uint16
		TCP uint16
		ID  NodeID
	}

	rpcEndpoint struct {
		IP  net.IP
		UDP uint16
		TCP uint16
	}
)

func makeEndpoint(addr *net.UDPAddr, tcpPort uint16) rpcEndpoint {
	ip := addr.IP.To4()
	if ip == nil {
		ip = addr.IP.To16()
	}
	return rpcEndpoint{IP: ip, UDP: uint16(addr.Port), TCP: tcpPort}
}

func expired(ts uint64) bool {
	return time.Unix(int64(ts), 0).Before(time.Now())
}
