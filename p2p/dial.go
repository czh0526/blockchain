package p2p

import (
	"net"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
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

type dialstate struct {
	maxDynDials int
}

func (s *dialstate) taskDone(t task, now time.Time) {
	switch t := t.(type) {
	case *dialTask:
	case *discoverTask:
	}
}

type task interface {
	Do(*Server)
}

type dialTask struct {
}

type discoverTask struct {
}
