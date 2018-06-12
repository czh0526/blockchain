package p2p

import (
	"container/heap"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/czh0526/blockchain/log"
	"github.com/czh0526/blockchain/p2p/discover"
)

var (
	errSelf             = errors.New("is self")
	errAlreadyDialing   = errors.New("already dialing")
	errAlreadyConnected = errors.New("already connected")
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

func (h *dialHistory) expire(now time.Time) {
	for h.Len() > 0 && h.min().exp.Before(now) {
		heap.Pop(h)
	}
}

type discoverTable interface {
	Self() *discover.Node
	Close()
	Resolve(target discover.NodeID) *discover.Node // 查找某一个节点
	Lookup(target discover.NodeID) []*discover.Node
	ReadRandomNodes([]*discover.Node) int // 获取随机数量的节点
}

type dialstate struct {
	maxDynDials int
	ntab        discoverTable

	lookupRunning bool
	lookupBuf     []*discover.Node
	dialing       map[discover.NodeID]connFlag
	randomNodes   []*discover.Node
	static        map[discover.NodeID]*dialTask
	hist          *dialHistory
	start         time.Time
	bootnodes     []*discover.Node
}

func newDialState(static []*discover.Node, bootnodes []*discover.Node, ntab discoverTable, maxdyn int) *dialstate {
	s := &dialstate{
		maxDynDials: maxdyn,
		ntab:        ntab,
		static:      make(map[discover.NodeID]*dialTask),
		dialing:     make(map[discover.NodeID]connFlag),
		bootnodes:   make([]*discover.Node, len(bootnodes)),
		hist:        new(dialHistory),
	}

	// 设置 bootnodes
	copy(s.bootnodes, bootnodes)
	// 设置 static
	for _, n := range static {
		s.addStatic(n)
	}
	return s
}

func (s *dialstate) addStatic(n *discover.Node) {
	s.static[n.ID] = &dialTask{flags: staticDialedConn, dest: n}
}

func (s *dialstate) removeStatic(n *discover.Node) {
	delete(s.static, n.ID)
	s.hist.remove(n.ID)
}

func (s *dialstate) taskDone(t task, now time.Time) {
	switch t := t.(type) {
	case *dialTask:
		s.hist.add(t.dest.ID, now.Add(dialHistoryExpiration))
		delete(s.dialing, t.dest.ID)
	case *discoverTask:
		s.lookupRunning = false
		s.lookupBuf = append(s.lookupBuf, t.results...)
	}
}

func (s *dialstate) newTasks(nRunning int, peers map[discover.NodeID]*Peer, now time.Time) []task {
	if s.start.IsZero() {
		s.start = now
	}

	// 核心数据，保存任务集合
	var newtasks []task

	// 创建和Node相连的拨号任务
	addDial := func(flag connFlag, n *discover.Node) bool {
		if err := s.checkDial(n, peers); err != nil {
			log.Trace("Skipping dial candidate", "id", n.ID, "addr", &net.TCPAddr{IP: n.IP, Port: int(n.TCP)}, "err", err)
			return false
		}
		log.Trace(fmt.Sprintf("添加dialTask() ==> 0x%x", n.ID.Bytes()))
		s.dialing[n.ID] = flag
		newtasks = append(newtasks, &dialTask{flags: flag, dest: n})
		return true
	}

	needDynDials := s.maxDynDials
	// 减去已经建立连接的
	for _, p := range peers {
		if p.rw.is(dynDialedConn) {
			needDynDials--
		}
	}
	// 减去正在建立连接的
	for _, flag := range s.dialing {
		if flag&dynDialedConn != 0 {
			needDynDials--
		}
	}

	s.hist.expire(now)

	// 向 newtasks 中加入全部的 static 节点
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

	// 向 newtasks 中加入一个 bootnode 节点
	if len(peers) == 0 && len(s.bootnodes) > 0 && needDynDials > 0 && now.Sub(s.start) > fallbackInterval {
		// shift bootnodes节点
		bootnode := s.bootnodes[0]
		s.bootnodes = append(s.bootnodes[:0], s.bootnodes[1:]...)
		s.bootnodes = append(s.bootnodes, bootnode)

		if addDial(dynDialedConn, bootnode) {
			needDynDials--
		}
	}

	// 从 discoverTable 中选出随机节点， 放入 newtasks
	randomCandidates := needDynDials / 2
	if randomCandidates > 0 {
		n := s.ntab.ReadRandomNodes(s.randomNodes)
		for i := 0; i < randomCandidates && i < n; i++ {
			if addDial(dynDialedConn, s.randomNodes[i]) {
				needDynDials--
			}
		}
	}

	// 从 lookupBuf 中选出节点, 放入 newtasks
	i := 0
	for ; i < len(s.lookupBuf) && needDynDials > 0; i++ {
		if addDial(dynDialedConn, s.lookupBuf[i]) {
			needDynDials--
		}
	}
	s.lookupBuf = s.lookupBuf[:copy(s.lookupBuf, s.lookupBuf[i:])]

	// 如果任务数量不够，构建一个discoverTask任务
	if len(s.lookupBuf) < needDynDials && !s.lookupRunning {
		s.lookupRunning = true
		newtasks = append(newtasks, &discoverTask{})
	}

	if nRunning == 0 && len(newtasks) == 0 && s.hist.Len() > 0 {
		t := &waitExpireTask{s.hist.min().exp.Sub(now)}
		newtasks = append(newtasks, t)
	}

	return newtasks
}

/**
检查节点n的历史操作状态
1) 拨号后放弃了
2) 正在拨号
3) 已经建立连接
*/
func (s *dialstate) checkDial(n *discover.Node, peers map[discover.NodeID]*Peer) error {
	_, dialing := s.dialing[n.ID]
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
	Info() string
}

type dialTask struct {
	flags        connFlag
	dest         *discover.Node
	lastResolved time.Time
	resolveDelay time.Duration
}

func (t *dialTask) Info() string {
	return fmt.Sprintf("#dialTask: {dest = %v:%v, lastResolved = %s}", t.dest.IP, t.dest.TCP, t.lastResolved.Format("2006-01-02 15:04:05"))
}

func (t *dialTask) Do(srv *Server) {
	if t.dest.Incomplete() {
		if !t.resolve(srv) {
			return
		}
	}

	err := t.dial(srv, t.dest)
	if err != nil {
		log.Trace("Dial error", "task", t, "err", err)
		if _, ok := err.(*dialError); ok && t.flags&staticDialedConn != 0 {
			if t.resolve(srv) {
				t.dial(srv, t.dest)
			}
		}
	}
}

func (t *dialTask) resolve(srv *Server) bool {
	fmt.Println("dialTask.resolve() has not be implemented. ")
	return false
}

type dialError struct {
	error
}

func (t *dialTask) dial(srv *Server, dest *discover.Node) error {
	fmt.Println("dialTask.dial() has not be implemented. ")
	return errors.New("func has not be implemented.")
}

type discoverTask struct {
	results []*discover.Node
}

func (t *discoverTask) Info() string {
	var info string
	info += "#discoverTask: {"
	for _, n := range t.results {
		info += fmt.Sprintf("    %x...@%s:%v, ", n.ID[:4], n.IP, n.TCP)
	}
	info += "}"
	return info
}

func (t *discoverTask) Do(srv *Server) {
	next := srv.lastLookup.Add(lookupInterval)
	if now := time.Now(); now.Before(next) {
		time.Sleep(next.Sub(now))
	}
	srv.lastLookup = time.Now()

	// 构建一个随机的NodeID, 执行 ntab.Lookup()
	var target discover.NodeID
	rand.Read(target[:])
	t.results = srv.ntab.Lookup(target)
}

type waitExpireTask struct {
	time.Duration
}

func (t *waitExpireTask) Info() string {
	return fmt.Sprintf("#waitExpireTask{ %s }", t.String())
}

func (t *waitExpireTask) Do(srv *Server) {
	fmt.Println("        waitExpireTask.Do()")
}
