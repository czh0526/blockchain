package discover

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

var (
	futureExp          = uint64(time.Now().Add(10 * time.Hour).Unix())
	testRemote         = rpcEndpoint{IP: net.ParseIP("1.1.1.1").To4(), UDP: 1, TCP: 2}
	testLocalAnnounced = rpcEndpoint{IP: net.ParseIP("2.2.2.2").To4(), UDP: 3, TCP: 4}
	testLocal          = rpcEndpoint{IP: net.ParseIP("3.3.3.3").To4(), UDP: 5, TCP: 6}
)

type dgramPipe struct {
	mu      *sync.Mutex
	cond    *sync.Cond
	closing chan struct{}
	closed  bool
	queue   [][]byte
}

func newpipe() *dgramPipe {
	mu := new(sync.Mutex)
	return &dgramPipe{
		closing: make(chan struct{}),
		cond:    &sync.Cond{L: mu},
		mu:      mu,
	}
}

func (c *dgramPipe) WriteToUDP(b []byte, to *net.UDPAddr) (n int, err error) {
	msg := make([]byte, len(b))
	copy(msg, b)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errors.New("closed")
	}
	c.queue = append(c.queue, msg)
	c.cond.Signal()
	return len(b), nil
}

func (c *dgramPipe) ReadFromUDP(b []byte) (n int, addr *net.UDPAddr, err error) {
	<-c.closing
	return 0, nil, io.EOF
}

func (c *dgramPipe) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.closed {
		close(c.closing)
		c.closed = true
	}
	return nil
}

func (c *dgramPipe) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: testLocal.IP, Port: int(testLocal.UDP)}
}

type udpTest struct {
	t                   *testing.T
	pipe                *dgramPipe
	table               *Table
	udp                 *udp
	sent                [][]byte
	localkey, remotekey *ecdsa.PrivateKey
	remoteaddr          *net.UDPAddr
}

func (test *udpTest) packetIn(wantError error, ptype byte, data packet) error {
	// 打包
	enc, _, err := encodePacket(test.remotekey, ptype, data)
	if err != nil {
		return test.errorf("packet (%d) encode error: %v.", ptype, err)
	}

	test.sent = append(test.sent, enc)
	// 处理
	if err = test.udp.handlePacket(test.remoteaddr, enc); err != wantError {
		return test.errorf("error mismatch: got %q, want %q", err, wantError)
	}
	return nil
}

func (test *udpTest) errorf(format string, args ...interface{}) error {
	_, file, line, ok := runtime.Caller(0)
	if ok {
		file = filepath.Base(file)
	} else {
		file = "???"
		line = 1
	}

	err := fmt.Errorf(format, args...)
	fmt.Printf("\t%s:%d: %v\n", file, line, err)
	test.t.Fail()
	return err
}

func newUDPTest(t *testing.T) *udpTest {
	test := &udpTest{
		t:          t,
		pipe:       newpipe(),
		localkey:   newkey(),
		remotekey:  newkey(),
		remoteaddr: &net.UDPAddr{IP: net.IP{10, 0, 1, 99}, Port: 30303},
	}

	test.table, test.udp, _ = newUDP(test.pipe, Config{PrivateKey: test.localkey})

	return test
}

func TestUDP_packetErrors(t *testing.T) {
	test := newUDPTest(t)
	defer test.table.Close()

	test.packetIn(errExpired, pingPacket, &ping{From: testRemote, To: testLocalAnnounced, Version: Version})
	test.packetIn(errUnsolicitedReply, pongPacket, &pong{ReplyTok: []byte{}, Expiration: futureExp})
	test.packetIn(errUnknownNode, findnodePacket, &findnode{Expiration: futureExp})
	test.packetIn(errUnsolicitedReply, neighborsPacket, &neighbors{Expiration: futureExp})
}
