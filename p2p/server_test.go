package p2p

import (
	"math/rand"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
)

func init() {
}

func startTestServer(t *testing.T, id discover.NodeID, pf func(*Peer)) *Server {
	config := Config{
		Name:       "test",
		MaxPeers:   10,
		ListenAddr: "127.0.0.1:0",
		PrivateKey: newkey(),
	}
	server := &Server{
		Config:       config,
		newPeerHook:  pf,
		newTransport: func(fd net.Conn) transport { return newTestTransport{id, fd} },
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Could not start server: %v", err)
	}
	return server
}

func TestServerListen(t *testing.T) {
	connected := make(chan *Peer)
	remid := randomID()
	// 启动server
	srv := startTestServer(t, remid, func(p *Peer) {
		if p.ID() != remid {
			t.Error("peer func called with wrong node id")
		}
		if p == nil {
			t.Error("peer func called with nil conn")
		}
		connected <- p
	})
	defer close(connected)
	defer srv.Stop()

	// 客户端拨号
	conn, err := net.DialTimeout("tcp", srv.ListenAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("could not dial: %v", err)
	}
	defer conn.Close()

	//  检查连接信息
	select {
	case peer := <-connected:
		if peer.LocalAddr().String() != conn.RemoteAddr().String() {
			t.Errorf("peer started with wrong conn: got %v, want %v", peer.LocalAdddr(), conn.RemoteAddr())
		}
		peers := srv.Peers()
		if !reflect.DeepEqual(peers, []*Peer{peer}) {
			t.Errorf("Peers mismatch: got %v, want %v", peers, []*Peer{peer})
		}
	case <-time.After(1 * time.Second):
		t.Errorf("server did not accept within one second")
	}
}

func randomID() (id discover.NodeID) {
	for i := range id {
		id[i] = byte(rand.Intn(255))
	}
	return id
}
