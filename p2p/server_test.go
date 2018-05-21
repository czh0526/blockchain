package p2p

import (
	"crypto/ecdsa"
	"fmt"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/sha3"
	"github.com/ethereum/go-ethereum/p2p/discover"
)

func init() {
}

type testTransport struct {
	id discover.NodeID
	*rlpx
	closeErr error
}

func (c *testTransport) doEncHandshake(prv *ecdsa.PrivateKey, dialDest *discover.Node) (discover.NodeID, error) {
	return c.id, nil
}

func (c *testTransport) doProtoHandshake(our *protoHandshake) (*protoHandshake, error) {
	return &protoHandshake{ID: c.id, Name: "test"}, nil
}

func newTestTransport(id discover.NodeID, fd net.Conn) transport {
	wrapped := newRLPX(fd).(*rlpx)
	wrapped.rw = newRLPXFrameRW(fd, secrets{
		MAC:        zero16,
		AES:        zero16,
		IngressMAC: sha3.NewKeccak256(),
		EgressMAC:  sha3.NewKeccak256(),
	})
	return &testTransport{id: id, rlpx: wrapped}
}

// 构建 TestServer, 
// RemoteID: id, 省略doEncHandshake
// newPeerHook: pf
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
		newTransport: func(fd net.Conn) transport { return newTestTransport(id, fd) },
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
	fmt.Printf("client dial to %s \n", srv.ListenAddr)

	//  检查连接信息
	select {
	case peer := <-connected:
		fmt.Println("read peer from connected chan.")
		if peer.LocalAddr().String() != conn.RemoteAddr().String() {
			t.Errorf("peer started with wrong conn: got %v, want %v", peer.LocalAddr(), conn.RemoteAddr())
		}
	}
	fmt.Println("Test Case Finished.")
}

func TestServerDial(t *testing.T) {
	// 构建 listener 
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not setup listener: %v", err)
	}
	defer listener.Close()

	// 启动例程，等待连接请求
	accepted := make(chan net.Conn)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			t.Error("accept error:", err)
			return
		}
		accepted <- conn
	}()
	
	// 构建 TestServer
	connected := make(chan *Peer)
	remid := randomID() 
	srv := startTestServer(t, remid, func(p *Peer) {
		connected <- p
	})
	defer close(connected)
	defer srv.Stop()

	// 让 TestServer 向listener 发起连接
	tcpAddr := listener.Addr().(*net.TCPAddr)
	srv.AddPeer(&discover.Node{ID: remid, IP: tcpAddr.IP, TCP: uint16(tcpAddr.Port)})

	select {
	case conn := <-accepted:
		defer conn.Close()

		select {
		case peer := <-connected:
			fmt.Printf("got peer ==> 0x%x", peer.ID().Bytes()[:4])
		}
	}
	fmt.Println("Test Case Finished.")
}

func randomID() (id discover.NodeID) {
	for i := range id {
		id[i] = byte(rand.Intn(255))
	}
	return id
}

func newkey() *ecdsa.PrivateKey {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic("couldn't generate key: " + err.Error())
	}
	return key
}
