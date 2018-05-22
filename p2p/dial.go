package p2p

import (
	"container/heap"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/discover"
)

var (
	errSelf             = errors.New("is self")
	errAlreadyDialing   = errors.New("already dialing")
	errAlreadyConnected = errors.New("aliready connected")
	errRecentlyDialed   = errors.New("recently dialed")
	errNotWhitelisted   = errors.New("not contained in netrestrict whitelist")
)

const (
	dialHistoryExpiration = 30 * time.Second
	lookupInterval        = 4 * time.Second
	fallbackInterval      = 20 * time.Second
	initialResolveDelay   = 60 * time.Second
	maxResolveDelay       = time.Hour
)

type connFlag int

const (
	dynDialedConn connFlag = 1 << iota
	staticDialedConn
	inboundConn
	trustedConn
)

type NodeDialer interface {
	Dial(*discover.Node) (net.Conn, error)
}

type TCPDialer struct {
	*net.Dialer
}

func (t TCPDialer) Dial(dest *discover.Node) (net.Conn, error) {
	addr := &net.TCPAddr{IP: dest.IP, Port: int(dest.TCP)}
	return t.Dialer.Dial("tcp", addr.String())
}

type pastDial struct {
	id  discover.NodeID
	exp time.Time
}

type dialHistory []pastDial

// heap.Interface
func (h dialHistory) Len() int           { return len(h) }
func (h dialHistory) Less(i, j int) bool { return h[i].exp.Before(h[j].exp) }
func (h dialHistory) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *dialHistory) Push(x interface{}) {
	*h = append(*h, x.(pastDial))
}
func (h *dialHistory) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h dialHistory) min() pastDial {
	return h[0]
}

func (h *dialHistory) add(id discover.NodeID, exp time.Time) {
	heap.Push(h, pastDial{id, exp})
}

func (h *dialHistory) remove(id discover.NodeID) bool {
	for i, v := range *h {
		if v.id == id {
			heap.Remove(h, i)
			return true
		}
	}
	return false
}

func (h dialHistory) contains(id discover.NodeID) bool {
	for _, v := range h {
		if v.id == id {
			return true
		}
	}
	return false
}

type dialstate struct {
	maxDynDials int

	lookupRunning bool
	dialing       map[discover.NodeID]connFlag
	lookupBuf     []*discover.Node
	randomNodes   []*discover.Node
	static        map[discover.NodeID]*dialTask
	hist          *dialHistory
	start         time.Time
	bootnodes     []*discover.Node
}

func (s *dialstate) taskDone(t task, now time.Time) {
	switch t := t.(type) {
	case *dialTask:
		fmt.Printf("taskDone(), task is %v \n", t)
	case *discoverTask:
		fmt.Printf("taskDone(), task is %v \n", t)
	}
}

func (s *dialstate) newTasks(nRunning int, peers map[discover.NodeID]*Peer, now time.Time) []task {
	if s.start.IsZero() {
		s.start = now
	}

	var newtasks []task
	// 创建和Node相连的拨号任务
	addDial := func(flag connFlag, n *discover.Node) bool {
		if err := s.checkDial(n, peers); err != nil {
			log.Trace("Skipping dial candidate", "id", n.ID, "addr", &net.TCPAddr{IP: n.IP, Port: int(n.TCP)}, "err", err)
			return false
		}
		newtasks = append(newtasks, &dialTask{})
		return true
	}

	needDynDials := s.maxDynDials

	// 将static中的dialTask放入newtasks
	for id, t := range s.static {
		err := s.checkDial(t.dest, peers)
		switch err {
		case errNotWhitelisted, errSelf:
			delete(s.static, t.dest.ID)
		case nil:
			s.dialing[id] = t.flags
			newtasks = append(newtasks, t)
		}
	}

	// 将bootnodes中的节点加入newtasks
	if len(peers) == 0 && len(s.bootnodes) > 0 && needDynDials > 0 && now.Sub(s.start) > fallbackInterval {
		// shift bootnodes节点
		bootnode := s.bootnodes[0]
		s.bootnodes = append(s.bootnodes[:0], s.bootnodes[1:]...)
		s.bootnodes = append(s.bootnodes, bootnode)

		if addDial(dynDialedConn, bootnode) {
			needDynDials--
		}
	}

	return newtasks
}

func (s *dialstate) checkDial(n *discover.Node, peers map[discover.NodeID]*Peer) error {
	_, dialing := s.dialing[n.ID] // 检查正在Dialing的队列中，是否存在这个目标节点
	switch {
	case dialing:
		return errAlreadyDialing
	case peers[n.ID] != nil: // 检查已经连接的节点中，是否存在这个目标节点
		return errAlreadyConnected
	case s.hist.contains(n.ID):
		return errRecentlyDialed
	}
	return nil
}

type task interface {
	Do(*Server)
}

type dialTask struct {
	flags        connFlag
	dest         *discover.Node
	lastResolved time.Time
	resolveDelay time.Duration
}

func (t *dialTask) Do(srv *Server) {
	fmt.Println("        dialTask.Do()")
}

type discoverTask struct {
}

func (t *discoverTask) Do(srv *Server) {
	fmt.Println("        discoverTask.Do()")
}

type waitExpireTask struct {
}

func (t *waitExpireTask) Do(srv *Server) {
	fmt.Println("        waitExpireTask.Do()")
}
