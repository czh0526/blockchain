package discv5

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/czh0526/blockchain/common"

	"github.com/czh0526/blockchain/log"
)

var (
	errInvalidEvent = errors.New("invalid in current state")
	errNoQuery      = errors.New("no pending query")
)

const (
	autoRefreshInterval = 1 * time.Hour
)

type nodeNetGuts struct {
	sha   common.Hash
	state *nodeState
}

// Node State Machine
type nodeState struct {
	name     string
	handle   func(*Network, *Node, nodeEvent, *ingressPacket) (net *nodeState, err error)
	enter    func(*Network, *Node)
	canQuery bool
}

func (s *nodeState) String() string {
	return s.name
}

var (
	unknown          *nodeState
	verifyinit       *nodeState
	verifywait       *nodeState
	remoteverifywait *nodeState
	known            *nodeState
	contested        *nodeState
	unresponsive     *nodeState
)

func init() {
	unknown = &nodeState{
		name: "unknown",
		enter: func(net *Network, n *Node) {
			fmt.Println("[unknown].enter()")
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			fmt.Println("[unknown].handle()")
			switch ev {
			case pingPacket:
				return verifywait, nil
			case pongPacket:
				return remoteverifywait, nil
			case pongTimeout:
				return unknown, nil
			default:
				return verifyinit, errInvalidEvent
			}
		},
	}

	verifyinit = &nodeState{
		name: "verifyinit",
		enter: func(net *Network, n *Node) {
			fmt.Println("[verifyinit].enter()")
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			fmt.Println("[verifyinit].handle()")
			switch ev {
			case pingPacket:
				return verifywait, nil
			case pongPacket:
				return remoteverifywait, nil
			case pongTimeout:
				return unknown, nil
			default:
				return verifyinit, errInvalidEvent
			}
		},
	}

	verifywait = &nodeState{
		name: "verifywait",
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			switch ev {
			case pingPacket:
				return verifywait, nil
			case pongPacket:
				return known, nil
			case pongTimeout:
				return unknown, nil
			default:
				return verifywait, errInvalidEvent
			}
		},
	}

	remoteverifywait = &nodeState{
		name: "remoteverifywait",
		enter: func(net *Network, n *Node) {
			fmt.Println("[remoteverifywait].enter()")
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			fmt.Println("[remoteverifywait].handle()")
			switch ev {
			case pingPacket:
				return remoteverifywait, nil
			case pingTimeout:
				return known, nil
			default:
				return remoteverifywait, errInvalidEvent
			}
		},
	}

	known = &nodeState{
		name:     "known",
		canQuery: true,
		enter: func(net *Network, n *Node) {
			fmt.Println("[known].enter()")
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			fmt.Println("[known].handle()")
			switch ev {
			case pingPacket:
				return known, nil
			case pongPacket:
				return known, nil
			default:
				return net.handleQueryEvent(n, ev, pkt)
			}
		},
	}

	contested = &nodeState{
		name:     "contested",
		canQuery: true,
		enter: func(net *Network, n *Node) {
			fmt.Println("[contested].enter()")
		},
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			fmt.Println("[contested].handle()")
			switch ev {
			case pongPacket:
				return known, nil
			case pongTimeout:
				return unresponsive, nil
			case pingPacket:
				return contested, nil
			default:
				return net.handleQueryEvent(n, ev, pkt)
			}
		},
	}

	unresponsive = &nodeState{
		name:     "unresponsive",
		canQuery: true,
		handle: func(net *Network, n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
			fmt.Println("[unresponsive].handle()")
			switch ev {
			case pingPacket:
				return known, nil
			case pongPacket:
				return known, nil
			default:
				return net.handleQueryEvent(n, ev, pkt)
			}
		},
	}
}

type Topic string

type transport interface {
	sendPing(remote *Node, remoteAddr *net.UDPAddr, topics []Topic) (hash []byte)
	sendNeighbours(remote *Node, nodes []*Node)
	sendFindnodeHash(remote *Node, target common.Hash)
	sendTopicRegister(remote *Node, topics []Topic, topicIdx int, pong []byte)
	sendTopicNodes(remote *Node, queryHash common.Hash, nodes []*Node)

	send(remote *Node, ptype nodeEvent, p interface{}) (hash []byte)

	localAddr() *net.UDPAddr
	Close()
}

type Network struct {
	conn transport

	closed        chan struct{}
	closeReq      chan struct{}
	read          chan ingressPacket
	timeoutTimers map[timeoutEvent]*time.Timer

	tab   *Table
	nodes map[NodeID]*Node
}

func newNetwork(conn transport, ourPubkey ecdsa.PublicKey, dbPath string) (*Network, error) {
	ourID := PubkeyID(&ourPubkey)

	tab := newTable(ourID, conn.localAddr())
	net := &Network{
		conn: conn,
		tab:  tab,
	}

	go net.loop()
	return net, nil
}

func (net *Network) handleQueryEvent(n *Node, ev nodeEvent, pkt *ingressPacket) (*nodeState, error) {
	switch ev {
	case findnodePacket:
	case neighborsPacket:
	case neighborsTimeout:
	case findnodeHashPacket:
	case topicRegisterPacket:
	case topicQueryPacket:
	case topicNodesPacket:
	default:
		return n.state, errInvalidEvent
	}
}

func (net *Network) internNode(pkt *ingressPacket) *Node {
	// 如果存在节点，返回原有节点
	if n := net.nodes[pkt.remoteID]; n != nil {
		n.IP = pkt.remoteAddr.IP
		n.UDP = uint16(pkt.remoteAddr.Port)
		n.TCP = uint16(pkt.remoteAddr.Port)
		return n
	}
	// 如果节点不存在，构建节点返回
	n := NewNode(pkt.remoteID, pkt.remoteAddr.IP, uint16(pkt.remoteAddr.Port), uint16(pkt.remoteAddr.Port))
	n.state = unknown
	net.nodes[pkt.remoteID] = n
	return n
}

func (net *Network) loop() {
	var (
		refreshTimer = time.NewTicker(autoRefreshInterval)
		refreshDone  chan struct{}
	)
loop:
	for {
		select {
		case <-net.closeReq:
			log.Trace("<-net.closeReq")
			break loop
		case pkt := <-net.read:
			log.Trace("<-net.read")
			// packet ==> node
			n := net.internNode(&pkt)
			prestate := n.state
			status := "ok"
			// 处理由nodeEvent引起的网络变化
			if err := net.handle(n, pkt.ev, &pkt); err != nil {
				status = err.Error()
			}

		case <-refreshTimer.C:
			log.Trace("<-refreshTimer.C")
			if refreshDone == nil {
				refreshDone = make(chan struct{})
				net.refresh(refreshDone)
			}
		}
	}
	log.Trace("loop stopped")

	log.Debug(fmt.Sprintf("shutting down"))
	if net.conn != nil {
		net.conn.Close()
	}
	if refreshDone != nil {
	}
	for _, timer := range net.timeoutTimers {
		timer.Stop()
	}
	close(net.closed)
}

func (net *Network) handle(n *Node, ev nodeEvent, pkt *ingressPacket) error {
	if pkt != nil {
		if err := net.checkPacket(n, ev, pkt); err != nil {
			return err
		}
	}

	if n.state == nil {
		n.state = unknown
	}

	next, err := n.state.handle(net, n, ev, pkt)
	net.transition(n, next)

	return err
}

func (net *Network) checkPacket(n *Node, ev nodeEvent, pkt *ingressPacket) error {
	switch ev {
	case pingPacket, findnodeHashPacket, neighborsPacket:
	case pongPacket:
	}

	fmt.Println("Network.checkPacket() need to be filled.")
	return nil
}

func (net *Network) transition(n *Node, next *nodeState) {
	if n.state != next {
		n.state = next
		if next.enter != nil {
			next.enter(net, n)
		}
	}
}

type nodeEvent uint

const (
	pingPacket nodeEvent = iota + 1
	pongPacket
	findnodePacket
	neighborsPacket
	findnodeHashPacket
	topicRegisterPacket
	topicQueryPacket
	topicNodesPacket

	pongTimeout nodeEvent = iota + 256
	pingTimeout
	neighborsTimeout
)

type timeoutEvent struct {
	ev   nodeEvent
	node *Node
}
