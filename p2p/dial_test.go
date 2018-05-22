package p2p

import (
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"

	"github.com/ethereum/go-ethereum/p2p/discover"
)

type dialtest struct {
	init   *dialstate
	rounds []round
}

type round struct {
	peers []*Peer
	done  []task
	new   []task
}

func runDialTest(t *testing.T, test dialtest) {
	var (
		vtime   time.Time
		running int
	)

	pm := func(ps []*Peer) map[discover.NodeID]*Peer {
		m := make(map[discover.NodeID]*Peer)
		for _, p := range ps {
			m[p.rw.id] = p
		}
		return m
	}

	for i, round := range test.rounds {
		// 处理完成的Task
		for _, task := range round.done {
			running--
			if running < 0 {
				panic("runing task counter underflow")
			}
			test.init.taskDone(task, vtime)
		}

		new := test.init.newTasks(running, pm(round.peers), vtime)
		if !sametasks(new, round.new) {
			t.Errorf("round %d: nnew tasks mismatch:\n got %v\n want %v\n state: %v\n running: %v\n",
				i, spew.Sdump(new), spew.Sdump(round.new), spew.Sdump(test.init), spew.Sdump(running))
		}

		vtime = vtime.Add(16 * time.Second)
		running += len(new)
	}
}

func TestDialStateDynDial(t *testing.T) {
	runDialTest(t, dialtest{
		init: newDialState(nil, nil, fakeTable{}, 5, nil),
		rounds: []round{
			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(2)}},
				},
				new: []task{&discoverTask{}},
			}, 
			{
				peers: []*Peer{
					{rw: &conn{flags: staticDialedConn, id: uintID(0)}},
					{rw: &conn{flags: dynDialedConn, id: uintID(1)}},
				}
			}
		}
	})
}
