package p2p

import (
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/czh0526/blockchain/rlp"
	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/discover"
)

const (
	baseProtocolVersion    = 5
	baseProtocolLength     = uint64(16)
	baseProtocolMaxMsgSize = 2 * 1024

	snappyProtocolVersion = 5

	pingInterval = 15 * time.Second
)

const (
	handshakeMsg = 0x00
	discMsg      = 0x01
	pingMsg      = 0x02
	pongMsg      = 0x03
)

type protoHandshake struct {
	Version    uint64
	Name       string
	Caps       []Cap
	ListenPort uint64
	ID         discover.NodeID
	Rest       []rlp.RawValue `rlp:"tail"`
}

type protoRW struct {
	Protocol
	in     chan Msg
	closed <-chan struct{}
	wstart <-chan struct{}
	werr   chan<- error
	offset uint64
	w      MsgWriter
}

func (rw *protoRW) ReadMsg() (Msg, error) {
	select {
	case msg := <-rw.in:
		msg.Code -= rw.offset
		return msg, nil
	case <-rw.closed:
		return Msg{}, io.EOF
	}
}

func (rw *protoRW) WriteMsg(msg Msg) (err error) {
	if msg.Code >= rw.Length {
		return newPeerError(errInvalidMsgCode, "not handled")
	}
	msg.Code += rw.offset
	select {
	case <-rw.wstart:
		err = rw.w.WriteMsg(msg)
		rw.werr <- err
	case <-rw.closed:
		err = fmt.Errorf("shutting down")
	}
	return err
}

type Peer struct {
	rw      *conn
	running map[string]*protoRW
	log     log.Logger
	created mclock.AbsTime

	wg       sync.WaitGroup
	protoErr chan error
	closed   chan struct{}
	disc     chan DiscReason
}

func matchProtocols(protocols []Protocol, caps []Cap, rw MsgReadWriter) map[string]*protoRW {
	sort.Sort(capsByNameAndVersion(caps))
	offset := baseProtocolLength
	result := make(map[string]*protoRW)

outer:
	for _, cap := range caps {
		for _, proto := range protocols {
			if proto.Name == cap.Name && proto.Version == cap.Version {
				if old := result[cap.Name]; old != nil {
					offset -= old.Length
				}

				result[cap.Name] = &protoRW{Protocol: proto, offset: offset, in: make(chan Msg), w: rw}
				offset += proto.Length

				continue outer
			}
		}
	}

	return result
}

func newPeer(conn *conn, protocols []Protocol) *Peer {
	protomap := matchProtocols(protocols, conn.caps, conn)
	p := &Peer{
		rw:       conn,
		running:  protomap,
		created:  mclock.Now(),
		disc:     make(chan DiscReason),
		protoErr: make(chan error, len(protomap)+1),
		closed:   make(chan struct{}),
		log:      log.New("id", conn.id, "conn", conn.flags),
	}
	p.log.SetHandler(log.StdoutHandler)
	return p
}

func (p *Peer) ID() discover.NodeID {
	return p.rw.id
}

func (p *Peer) Name() string {
	return p.rw.name
}

func (p *Peer) LocalAddr() net.Addr {
	return p.rw.fd.LocalAddr()
}

func (p *Peer) run() (remoteRequested bool, err error) {
	var (
		writeStart = make(chan struct{}, 1)
		writeErr   = make(chan error, 1)
		readErr    = make(chan error, 1)
		reason     DiscReason
	)
	p.wg.Add(2)
	p.log.Trace("启动 readLoop 循环")
	go p.readLoop(readErr)
	p.log.Trace("启动 pingLoop 循环")
	go p.pingLoop()

	writeStart <- struct{}{}
	p.log.Trace("启动 Protocol 处理")
	p.startProtocols(writeStart, writeErr)

loop:
	for {
		select {
		case err = <-writeErr:
			if err != nil {
				reason = DiscNetworkError
				break loop
			}
			writeStart <- struct{}{}
		case err = <-readErr:
			if r, ok := err.(DiscReason); ok {
				remoteRequested = true
				reason = r
			} else {
				reason = DiscNetworkError
			}
		case err = <-p.protoErr:
			reason = discReasonForError(err)
			break loop
		case err = <-p.disc:
			reason = discReasonForError(err)
			break loop
		}
	}

	// 关闭标志chan
	close(p.closed)
	// 写入断开原因，关闭读写通道
	p.rw.close(reason)
	// 等待其它例程结束
	p.wg.Wait()
	return remoteRequested, err
}

func (p *Peer) pingLoop() {
	ping := time.NewTimer(pingInterval)
	defer p.wg.Done()
	defer ping.Stop()
	for {
		select {
		case <-ping.C:
			if err := SendItems(p.rw, pingMsg); err != nil {
				p.protoErr <- err
				return
			}
			ping.Reset(pingInterval)

		case <-p.closed:
			return
		}
	}
}

func (p *Peer) readLoop(errc chan<- error) {
	defer p.wg.Done()
	for {
		msg, err := p.rw.ReadMsg()
		if err != nil {
			errc <- err
			return
		}
		msg.ReceivedAt = time.Now()
		if err = p.handle(msg); err != nil {
			errc <- err
			return
		}
	}
}

func (p *Peer) handle(msg Msg) error {
	switch {
	case msg.Code == pingMsg:
		msg.Discard()
		go SendItems(p.rw, pongMsg)
	case msg.Code == discMsg:
		var reason [1]DiscReason
		rlp.Decode(msg.Payload, &reason)
		return reason[0]
	case msg.Code < baseProtocolLength:
		return msg.Discard()
	default:
		proto, err := p.getProto(msg.Code)
		if err != nil {
			return fmt.Errorf("msg code out of range: %v", msg.Code)
		}
		select {
		case proto.in <- msg:
			return nil
		case <-p.closed:
			return io.EOF
		}
	}
	return nil
}

func (p *Peer) getProto(code uint64) (*protoRW, error) {
	for _, proto := range p.running {
		if code >= proto.offset && code < proto.offset+proto.Length {
			return proto, nil
		}
	}
	return nil, newPeerError(errInvalidMsgCode, "%d", code)
}

func (p *Peer) startProtocols(writeStart <-chan struct{}, writeErr chan<- error) {
	p.wg.Add(len(p.running))
	for _, proto := range p.running {
		proto := proto
		proto.closed = p.closed
		proto.wstart = writeStart
		proto.werr = writeErr
		var rw MsgReadWriter = proto
		p.log.Trace(fmt.Sprintf("Starting protocol %s/%d", proto.Name, proto.Version))
		go func() {
			err := proto.Run(p, rw)
			if err == nil {
				p.log.Trace(fmt.Sprintf("Protocol %s/%d returned", proto.Name, proto.Version))
				err = errProtocolReturned
			} else if err != io.EOF {
				p.log.Trace(fmt.Sprintf("Protocol %s/%d failed", proto.Name, proto.Version), "err", err)
			}
			p.protoErr <- err
			p.wg.Done()
		}()
	}
}
