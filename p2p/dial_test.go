package p2p

import (
	"encoding/binary"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"

	"github.com/czh0526/blockchain/p2p/discover"
)

type dialtest struct {
	init   *dialstate
	rounds []round
}

type round struct {
	peers []*Peer
	done  []task // 模拟构建Task的返回结果
	new   []task // 根据当前状态，新构建出的Task集合
}

func runDialTest(t *testing.T, test dialtest) {
	var (
		vtime   time.Time
		running int // 运行中的任务
	)

	pm := func(ps []*Peer) map[discover.NodeID]*Peer {
		m := make(map[discover.NodeID]*Peer)
		for _, p := range ps {
			m[p.rw.id] = p
		}
		return m
	}

	for i, round := range test.rounds {
		// 处理任务结果
		for _, task := range round.done {
			running--
			if running < 0 {
				panic("runing task counter underflow")
			}
			test.init.taskDone(task, vtime)
		}

		// 构建新任务
		new := test.init.newTasks(running, pm(round.peers), vtime)
		// 检查新任务
		if !sametasks(new, round.new) {
			t.Errorf("round %d: new tasks mismatch:\n got %v\n want %v\n state: %v\n running: %v\n",
				i, spew.Sdump(new), spew.Sdump(round.new), spew.Sdump(test.init), spew.Sdump(running))
		}

		vtime = vtime.Add(16 * time.Second)
		running += len(new)
	}
}

type fakeTable []*discover.Node

func (t fakeTable) Self() *discover.Node                     { return new(discover.Node) }
func (t fakeTable) Close()                                   {}
func (t fakeTable) Lookup(discover.NodeID) []*discover.Node  { return nil }
func (t fakeTable) Resolve(discover.NodeID) *discover.Node   { return nil }
func (t fakeTable) ReadRandomNodes(buf []*discover.Node) int { return copy(buf, t) }

func TestDialStateDynDial(t *testing.T) {
	runDialTest(t, dialtest{
		init: newDialState(nil, nil, fakeTable{}, 5),
		rounds: []round{
			{ // 通过节点0,1,2,没有找到新节点，生成一个discoverTask任务
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)}},
				},
				new: []task{&discoverTask{}},
			},
			{ // 通过节点0,1,2，发现了6个节点(2-7), 生成 dialTask(3,4,5)
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)}},
				},
				done: []task{ // 发现了6个节点
					&discoverTask{results: []*discover.Node{
						{ID: uintID(2)},
						{ID: uintID(3)},
						{ID: uintID(4)},
						{ID: uintID(5)},
						{ID: uintID(6)},
						{ID: uintID(7)},
					}},
				},
				new: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(3)}},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)}},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(5)}},
				},
			},
			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(3)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(4)}},
				},
				done: []task{
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(3)}},
					&dialTask{flags: dynDialedConn, dest: &discover.Node{ID: uintID(4)}},
				},
			},
		},
	})
}

func TestDialResolve(t *testing.T) {
	resolved := discover.NewNode(uintID(1), net.IP{127, 0, 55, 234}, 3333, 4444)
	// 构建 discoverTable
	table := &resolveMock{answer: resolved}
	// 构建 dialstate
	state := newDialState(nil, nil, table, 0)

	// 添加一个static节点，开始进行网络探测
	dest := discover.NewNode(uintID(1), nil, 0, 0)
	state.addStatic(dest)
	tasks := state.newTasks(0, nil, time.Time{})
	// 检查任务是否正确
	if !reflect.DeepEqual(tasks, []task{&dialTask{flags: staticDialedConn, dest: dest}}) {
		t.Fatalf("expected dial task, got %#v", tasks)
	}

	config := Config{Dialer: TCPDialer{&net.Dialer{Deadline: time.Now().Add(-5 * time.Minute)}}}
	srv := &Server{ntab: table, Config: config}
	tasks[0].Do(srv)
	if !reflect.DeepEqual(table.resolveCalls, []discover.NodeID{dest.ID}) {
		t.Fatalf("wrong resolve calls, got %v", table.resolveCalls)
	}
}

type resolveMock struct {
	resolveCalls []discover.NodeID
	answer       *discover.Node
}

func (t *resolveMock) Resolve(id discover.NodeID) *discover.Node {
	t.resolveCalls = append(t.resolveCalls, id)
	return t.answer
}

func (t *resolveMock) Self() *discover.Node                     { return new(discover.Node) }
func (t *resolveMock) Close()                                   {}
func (t *resolveMock) Bootstrap([]*discover.Node)               {}
func (t *resolveMock) Lookup(discover.NodeID) []*discover.Node  { return nil }
func (t *resolveMock) ReadRandomNodes(buf []*discover.Node) int { return 0 }

func uintID(i uint32) discover.NodeID {
	var id discover.NodeID
	binary.BigEndian.PutUint32(id[:], i)
	return id
}

func sametasks(a, b []task) bool {
	if len(a) != len(b) {
		return false
	}
next:
	for _, ta := range a {
		for _, tb := range b {
			if reflect.DeepEqual(ta, tb) {
				continue next
			}
		}
		return false
	}
	return true
}
