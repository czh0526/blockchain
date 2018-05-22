package p2p

import (
	"errors"
	"fmt"
	"net"
	"testing"
)

func testPeer(protos []Protocol) (func(), *conn, *Peer, <-chan error) {
	// 生成读写管道
	fd1, fd2 := net.Pipe()
	id1 := randomID()
	c1 := &conn{id: id1, fd: fd1, transport: newTestTransport(id1, fd1)}
	id2 := randomID()
	c2 := &conn{id: id2, fd: fd2, transport: newTestTransport(id2, fd2)}
	for _, p := range protos {
		c1.caps = append(c1.caps, p.cap())
		c2.caps = append(c2.caps, p.cap())
	}

	// 在read端建立Peer
	peer := newPeer(c1, protos)
	errc := make(chan error, 1)

	// 启动read端的Peer
	go func() {
		_, err := peer.run()
		errc <- err
	}()

	closer := func() { c2.close(errors.New("close func called")) }
	return closer, c2, peer, errc
}

func TestPeerProtoReadMsg(t *testing.T) {
	proto := Protocol{
		Name:   "a",
		Length: 5,
		Run: func(peer *Peer, rw MsgReadWriter) error {
			// 验证 Msg 2
			if err := ExpectMsg(rw, 2, []uint{1}); err != nil {
				t.Error(err)
			}
			// 验证 Msg 3
			if err := ExpectMsg(rw, 3, []uint{2}); err != nil {
				t.Error(err)
			}
			// 验证 Msg 4
			if err := ExpectMsg(rw, 4, []uint{3}); err != nil {
				t.Error(err)
			}
			return nil
		},
	}
	fmt.Println("构建 Protocol.")

	// 启动 read Peer
	closer, rw, _, errc := testPeer([]Protocol{proto})
	defer closer()
	fmt.Println("启动支持protocol协议的peer节点, 验证后续接收到的消息.")

	// 发送消息
	fmt.Println("发送消息：baseProtocolLength +2")
	Send(rw, baseProtocolLength+2, []uint{1})
	fmt.Println("发送消息：baseProtocolLength +3")
	Send(rw, baseProtocolLength+3, []uint{2})
	fmt.Println("发送消息：baseProtocolLength +4")
	Send(rw, baseProtocolLength+4, []uint{3})

	select {
	case err := <-errc:
		if err != errProtocolReturned {
			t.Errorf("peer returned error: %v", err)
		}
	}

	fmt.Sprintln("TestCase Finished.")
}

func TestPeerProtoEncodeMsg(t *testing.T) {
	proto := Protocol{
		Name:   "a",
		Length: 2,
		Run: func(peer *Peer, rw MsgReadWriter) error {
			// 发送一个错误消息
			if err := SendItems(rw, 2); err == nil {
				t.Error("expected error for out-of-range msg code, got nil.")
			}
			// 发送一个正确消息
			if err := SendItems(rw, 1, "foo", "bar"); err != nil {
				t.Errorf("write error: %v", err)
			}
			return nil
		},
	}

	closer, rw, _, _ := testPeer([]Protocol{proto})
	defer closer()

	// 验证正确消息 17 = baseProtocolLength + 1
	if err := ExpectMsg(rw, 17, []string{"foo", "bar"}); err != nil {
		t.Error(err)
	}
}
