package discover

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/crypto"
)

var (
	futureExp          = uint64(time.Now().Add(10 * time.Hour).Unix())
	testTarget         = NodeID{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1}        // 目标查找节点
	testRemote         = rpcEndpoint{IP: net.ParseIP("1.1.1.1").To4(), UDP: 1, TCP: 2} // 远程节点
	testLocalAnnounced = rpcEndpoint{IP: net.ParseIP("2.2.2.2").To4(), UDP: 3, TCP: 4} // 本地监听节点
	testLocal          = rpcEndpoint{IP: net.ParseIP("3.3.3.3").To4(), UDP: 5, TCP: 6} //
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

// 从 queue 中摘下[0]位置的元素
func (c *dgramPipe) waitPacketOut() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.queue) == 0 {
		c.cond.Wait()
	}
	p := c.queue[0]
	copy(c.queue, c.queue[1:])
	c.queue = c.queue[:len(c.queue)-1]
	return p
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

// 拦截发送的Packet，并调用 validate 函数进行校验
func (test *udpTest) waitPacketOut(validate interface{}) ([]byte, error) {
	dgram := test.pipe.waitPacketOut()
	p, _, hash, err := decodePacket(dgram)
	if err != nil {
		return hash, test.errorf("sent packet decode error: %v", err)
	}
	fn := reflect.ValueOf(validate)
	exptype := fn.Type().In(0)
	if reflect.TypeOf(p) != exptype {
		return hash, test.errorf("send packet type mismatch, got: %v, want: %v", reflect.TypeOf(p), exptype)
	}
	fn.Call([]reflect.Value{reflect.ValueOf(p)})
	return hash, nil
}

/*
func (test *udpTest) waitPacketOut(validator func(p *neighbors)) ([]byte, error) {
	dgram := test.pipe.waitPacketOut()
	p, _, hash, err := decodePacket(dgram)
	if err != nil {
		return hash, test.errorf("sent packet decode error: %v", err)
	}

	neighborsPacket, ok := p.(*neighbors)
	if !ok {
		return hash, test.errorf("send packet type mismatch, got: %v, want: 'neighbors'", reflect.TypeOf(p))
	}
	validator(neighborsPacket)
	return hash, nil
}
*/

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

// 3.3.3.3:5/6 <= ..... 10.0.1.99:30303/0
func newUDPTest(t *testing.T) *udpTest {
	test := &udpTest{
		t:          t,
		pipe:       newpipe(),
		localkey:   newkey(),
		remotekey:  newkey(),
		remoteaddr: &net.UDPAddr{IP: net.IP{10, 0, 1, 99}, Port: 30303},
	}

	// 启动 Table\UDP
	test.table, test.udp, _ = newUDP(test.pipe, Config{PrivateKey: test.localkey})

	return test
}

func TestUDP_packetErrors(t *testing.T) {
	test := newUDPTest(t)
	// 关闭 Table\UDP
	defer test.table.Close()

	test.packetIn(errExpired, pingPacket, &ping{From: testRemote, To: testLocalAnnounced, Version: Version})
	test.packetIn(errUnsolicitedReply, pongPacket, &pong{ReplyTok: []byte{}, Expiration: futureExp})
	test.packetIn(errUnknownNode, findnodePacket, &findnode{Expiration: futureExp})
	test.packetIn(errUnsolicitedReply, neighborsPacket, &neighbors{Expiration: futureExp})
}

func TestUDP_pingTimeout(t *testing.T) {
	t.Parallel()
	test := newUDPTest(t)
	defer test.table.Close()

	// 构建节点
	toaddr := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 2222}
	toid := NodeID{1, 2, 3, 4}
	// 通过 udp 发送 ping 消息
	if err := test.udp.ping(toid, toaddr); err != errTimeout {
		t.Error("expected timeout error, got", err)
	}
}

func TestUDP_responseTimeouts(t *testing.T) {
	//t.Parallel()
	test := newUDPTest(t)
	defer test.table.Close()

	rand.Seed(time.Now().UnixNano())
	randomDuration := func(max time.Duration) time.Duration {
		return time.Duration(rand.Int63n(int64(max)))
	}

	var (
		nReqs     = 200
		nTimeouts = 0
		// 设置两个错误通道
		nilErr     = make(chan error, nReqs)
		timeoutErr = make(chan error, nReqs)
	)

	for i := 0; i < nReqs; i++ {
		// 构建200个 pending 对象
		p := &pending{
			ptype:    byte(rand.Intn(255)),
			callback: func(interface{}) bool { return true },
		}
		binary.BigEndian.PutUint64(p.from[:], uint64(i))

		// 将两个错误通道随机分配给200个pending对象
		if p.ptype <= 128 {
			fmt.Printf("(%d) 挂起消息: %d, 0x%x...\n", i+1, p.ptype, p.from[:8])
			p.errc = timeoutErr
			test.udp.addpending <- p // udp.loop()将会在超时后，向timeoutErr返回 errTimeout
			// 统计 timeoutErr 通道的 pending 对象数量
			nTimeouts++
		} else {
			p.errc = nilErr
			test.udp.addpending <- p // udp.loop()将会在p.callback()后，向nilErr返回 nil
			time.AfterFunc(randomDuration(60*time.Millisecond), func() {
				fmt.Printf("(%d) 挂起消息: %d, 0x%x...\n", i+1, p.ptype, p.from[:8])
				if !test.udp.handleReply(p.from, p.ptype, nil) {
					t.Logf("not matched: %v", p)
				}
			})
		}
		fmt.Printf("---------------- 等待 30 毫秒 --------------------\n")
		time.Sleep(randomDuration(30 * time.Millisecond))
	}

	var (
		recvDeadline        = time.After(20 * time.Second)
		nTimeoutsRecv, nNil = 0, 0
	)
	for i := 0; i < nReqs; i++ {
		select {
		case err := <-timeoutErr:
			if err != errTimeout {
				t.Fatalf("got non-timeout error on timeoutErr %d: %v", i, err)
			}
			// 统计 errTimeout 事件的数量
			nTimeoutsRecv++

		case err := <-nilErr:
			if err != nil {
				t.Fatalf("got non-nil error on nilErr %d: %v", i, err)
			}
			// 统计 nil 事件的数量
			nNil++

		case <-recvDeadline:
			t.Fatalf("exceeded recv deadline")
		}
	}
	if nTimeoutsRecv != nTimeouts {
		t.Errorf("wrong number of timeout errors received: got %d, want %d", nTimeoutsRecv, nTimeouts)
	}
	if nNil != nReqs-nTimeouts {
		t.Errorf("wrong number of successful replies: got %d, want %d", nNil, nReqs-nTimeouts)
	}
}

func TestUDP_findnodeTimeout(t *testing.T) {
	t.Parallel()
	test := newUDPTest(t)
	defer test.table.Close()

	toaddr := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 2222}
	toid := NodeID{1, 2, 3, 4}
	target := NodeID{4, 5, 6, 7}
	// 向 1.2.3.4:2222 询问距离 4.5.6.7 最近的节点
	result, err := test.udp.findnode(toid, toaddr, target)
	if err != errTimeout {
		t.Error("expected timeout error, fot", err)
	}
	if len(result) > 0 {
		t.Error("expected empty result, got", result)
	}
}

func nodeAtDistance(base common.Hash, ld int) (n *Node) {
	n = new(Node)
	n.sha = hashAtDistance(base, ld)
	n.IP = net.IP{byte(ld), 0, 2, byte(ld)}
	copy(n.ID[:], n.sha[:])
	return n
}

func TestUDP_findnode(t *testing.T) {
	test := newUDPTest(t)
	defer test.table.Close()

	targetHash := crypto.Keccak256Hash(testTarget[:])
	nodes := &nodesByDistance{target: targetHash}
	for i := 0; i < bucketSize; i++ {
		nodes.push(nodeAtDistance(test.table.self.sha, i+2), bucketSize)
	}
	test.table.stuff(nodes.entries)

	test.table.db.updateBondTime(PubkeyID(&test.remotekey.PublicKey), time.Now())

	// 模拟收到一个 findnode 消息
	test.packetIn(nil, findnodePacket, &findnode{Target: testTarget, Expiration: futureExp})
	// 在 Table 中找到离 targetHash 最近的 bucketSize 个节点
	expected := test.table.closest(targetHash, bucketSize)

	waitNeighbors := func(want []*Node) {
		test.waitPacketOut(func(p *neighbors) {
			if len(p.Nodes) != len(want) {
				t.Errorf("wrong number of results: got %d, want %d", len(p.Nodes), bucketSize)
			}
			for i := range p.Nodes {
				if p.Nodes[i].ID != want[i].ID {
					t.Errorf("result mismatch at %d: \n got: %v\n want: %v", i, p.Nodes[i], expected.entries[i])
				}
			}
		})
	}

	/*
	 bucketSize个节点会分多个UDP返回，校验第一个UDP和最后一个UDP.
	*/

	// 校验第一个 UDP 中包含的 Neighbors
	waitNeighbors(expected.entries[:maxNeighbors])
	// 校验最后一个 UDP 中包含的 Neighbors
	waitNeighbors(expected.entries[maxNeighbors:])
}

func TestUDP_findnodeMultiReply(t *testing.T) {
	test := newUDPTest(t)
	defer test.table.Close()

	resultc, errc := make(chan []*Node), make(chan error)
	// 向 remoteaddr 发送 findnode 消息
	go func() {
		rid := PubkeyID(&test.remotekey.PublicKey)
		ns, err := test.udp.findnode(rid, test.remoteaddr, testTarget)
		if err != nil && len(ns) == 0 {
			errc <- err
		} else {
			resultc <- ns
		}
	}()

	// 拦截 findnode 消息，并进行 target 的校验
	test.waitPacketOut(func(p *findnode) {
		if p.Target != testTarget {
			t.Errorf("wrong target: got %v, want %v", p.Target, testTarget)
		}
	})

	// 构建两个neighbors消息，模拟响应
	list := []*Node{
		MustParseNode("enode://ba85011c70bcc5c04d8607d3a0ed29aa6179c092cbdda10d5d32684fb33ed01bd94f588ca8f91ac48318087dcb02eaf36773a7a453f0eedd6742af668097b29c@10.0.1.16:30303?discport=30304"),
		MustParseNode("enode://81fa361d25f157cd421c60dcc28d8dac5ef6a89476633339c5df30287474520caca09627da18543d9079b5b288698b542d56167aa5c09111e55acdbbdf2ef799@10.0.1.16:30303"),
		MustParseNode("enode://9bffefd833d53fac8e652415f4973bee289e8b1a5c6c4cbe70abf817ce8a64cee11b823b66a987f51aaa9fba0d6a91b3e6bf0d5a5d1042de8e9eeea057b217f8@10.0.1.36:30301?discport=17"),
		MustParseNode("enode://1b5b4aa662d7cb44a7221bfba67302590b643028197a7d5214790f3bac7aaa4a3241be9e83c09cf1f6c69d007c634faae3dc1b1221793e8446c0b3a09de65960@10.0.1.16:30303"),
	}

	rpclist := make([]rpcNode, len(list))
	for i := range list {
		rpclist[i] = nodeToRPC(list[i])
	}
	test.packetIn(nil, neighborsPacket, &neighbors{Expiration: futureExp, Nodes: rpclist[:2]})
	test.packetIn(nil, neighborsPacket, &neighbors{Expiration: futureExp, Nodes: rpclist[2:]})

	select {
	case result := <-resultc:
		want := append(list[:2], list[3:]...)
		if !reflect.DeepEqual(result, want) {
			t.Errorf("neighbors mismatch: \n got: %v\n	want: %v", result, want)
		}
	case err := <-errc:
		t.Errorf("findnode error: %v", err)
	case <-time.After(5 * time.Second):
		t.Error("findnode did not return within 5 seconds")
	}
}

/*
		3.3.3.3:5/6	    .......................		1.1.1.1:1/2
				|									  |
				|									  |
			2.2.2.2:3/4	  ....................	10.0.1.99:30303/0
*/
func TestUDP_successfulPing(t *testing.T) {
	test := newUDPTest(t)
	added := make(chan *Node, 1)
	test.table.nodeAddedHook = func(n *Node) { added <- n }
	defer test.table.Close()

	// 启动例程模拟节点(10.0.1.99)，发送一个 ping 消息给 2.2.2.2
	// 1.1.1.1:1/2 ==> 2.2.2.2:3/4
	go test.packetIn(nil, pingPacket,
		&ping{From: testRemote, To: testLocalAnnounced, Version: Version, Expiration: futureExp})

	// 校验构造的 pong 消息, 是否是发送给 10.0.1.99:30303/2
	test.waitPacketOut(func(p *pong) {
		pinghash := test.sent[0][:macSize]
		if !bytes.Equal(p.ReplyTok, pinghash) {
			t.Errorf("got pong.ReplyTok %x, want %x", p.ReplyTok, pinghash)
		}
		wantTo := rpcEndpoint{
			IP:  test.remoteaddr.IP,
			UDP: uint16(test.remoteaddr.Port),
			TCP: testRemote.TCP,
		}
		if !reflect.DeepEqual(p.To, wantTo) {
			t.Errorf("got pong.To %v, want %v", p.To, wantTo)
		}
	})

	// 校验发送 ping 消息内容
	_, _ = test.waitPacketOut(func(p *ping) error {
		fmt.Println(test.udp.ourEndpoint)
		if !reflect.DeepEqual(p.From, test.udp.ourEndpoint) {
			t.Errorf("got ping.From %v, want %v", p.From, test.udp.ourEndpoint)
		}
		return nil
	})

	<-time.After(time.Minute)
}
