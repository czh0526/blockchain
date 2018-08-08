package p2p

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/czh0526/blockchain/crypto"
	"github.com/czh0526/blockchain/crypto/ecies"
	"github.com/czh0526/blockchain/crypto/sha3"
	"github.com/czh0526/blockchain/p2p/discover"
	"github.com/czh0526/blockchain/rlp"
	"github.com/davecgh/go-spew/spew"
)

func TestSharedSecret(t *testing.T) {
	//构建 ecdsa.PrivateKey
	prv0, _ := crypto.GenerateKey()
	pub0 := &prv0.PublicKey
	prv1, _ := crypto.GenerateKey()
	pub1 := &prv1.PublicKey

	// ecies.PrivateKey + ecies.PublicKey ==> ss
	ss0, err := ecies.ImportECDSA(prv0).GenerateShared(ecies.ImportECDSAPublic(pub1), sskLen, sskLen)
	if err != nil {
		return
	}

	// 使用1的PrivateKey和0的PublicKey构建共享的对称密钥
	ss1, err := ecies.ImportECDSA(prv1).GenerateShared(ecies.ImportECDSAPublic(pub0), sskLen, sskLen)
	if err != nil {
		return
	}

	// 检查两端密钥是否一致
	if !bytes.Equal(ss0, ss1) {
		t.Errorf("don't match, :(")
	}
}

func TestEncHandshake(t *testing.T) {
	for i := 0; i < 10; i++ {
		start := time.Now()
		if err := testEncHandshake(nil); err != nil {
			t.Fatalf("i=%d %v", i, err)
		}
		t.Logf("(without token) %d %v \n", i+1, time.Since(start))
	}

	for i := 0; i < 10; i++ {
		tok := make([]byte, shaLen)
		rand.Reader.Read(tok)
		start := time.Now()
		if err := testEncHandshake(tok); err != nil {
			t.Fatalf("i=%d %v", i, err)
		}
		t.Logf("(with token) %d %v\n", i+1, time.Since(start))
	}
}

func testEncHandshake(token []byte) error {
	type result struct {
		side string
		id   discover.NodeID
		err  error
	}

	var (
		// 构建一对ecdsa私钥
		prv0, _ = crypto.GenerateKey()
		prv1, _ = crypto.GenerateKey()
		// 构建一个管道
		fd0, fd1 = net.Pipe()
		// 构建一对rlpx端
		c0, c1 = newRLPX(fd0).(*rlpx), newRLPX(fd1).(*rlpx)
		output = make(chan result)
	)

	go func() {
		r := result{side: "initiator"}
		defer func() { output <- r }()
		defer fd0.Close()

		// 由0端向1端发起握手请求，得到1端的id
		dest := &discover.Node{ID: discover.PubkeyID(&prv1.PublicKey)}
		r.id, r.err = c0.doEncHandshake(prv0, dest)
		if r.err != nil {
			return
		}

		// 对比得到的1端id是否和期望的一致。
		id1 := discover.PubkeyID(&prv1.PublicKey)
		if r.id != id1 {
			r.err = fmt.Errorf("remote ID mismatch: got %v, want: %v", r.id, id1)
		}
	}()

	go func() {
		r := result{side: "receiver"}
		defer func() { output <- r }()
		defer fd1.Close()

		// 等待接收0端发来的握手请求，得到0端的id
		r.id, r.err = c1.doEncHandshake(prv1, nil)
		if r.err != nil {
			return
		}

		// 对比得到的0端id是否和期望的一致。
		id0 := discover.PubkeyID(&prv0.PublicKey)
		if r.id != id0 {
			r.err = fmt.Errorf("remote ID mismatch: got %v, want: %v", r.id, id0)
		}
	}()

	// 检查握手过程中是否有错误产生
	r0, r1 := <-output, <-output
	if r0.err != nil {
		return fmt.Errorf("%s side error: %v", r0.side, r0.err)
	}
	if r1.err != nil {
		return fmt.Errorf("%s side error: %v", r1.side, r1.err)
	}

	if !reflect.DeepEqual(c0.rw.egressMAC, c1.rw.ingressMAC) {
		return fmt.Errorf("egress mac mismatch:\n c0.rw = %#v \n c1.rw = %#v", c0.rw.egressMAC, c1.rw.ingressMAC)
	}
	if !reflect.DeepEqual(c0.rw.ingressMAC, c1.rw.egressMAC) {
		return fmt.Errorf("ingress mac mismatch:\n c0.rw = %#v \n c1.rw = %#v", c0.rw.ingressMAC, c1.rw.egressMAC)
	}
	if !reflect.DeepEqual(c0.rw.enc, c1.rw.enc) {
		return fmt.Errorf("enc cipher mismatch:\n c0.rw.enc: %#v\n c1.rw.enc: %#v", c0.rw.enc, c1.rw.enc)
	}
	if !reflect.DeepEqual(c0.rw.dec, c1.rw.dec) {
		return fmt.Errorf("dec cipher mismatch:\n c0.rw.dec: %#v\n c1.rw.dec: %#v", c0.rw.dec, c1.rw.dec)
	}
	if !reflect.DeepEqual(c0.rw.macCipher, c1.rw.macCipher) {
		return fmt.Errorf("mac cipher mismatch:\n c0.rw.macCipher: %#v\n c1.rw.macCipher: %#v", c0.rw.macCipher, c1.rw.macCipher)
	}

	return nil
}

func TestProtocolHandshake(t *testing.T) {
	var (
		prv0, _ = crypto.GenerateKey()
		node0   = &discover.Node{ID: discover.PubkeyID(&prv0.PublicKey), IP: net.IP{1, 2, 3, 4}, TCP: 33}
		hs0     = &protoHandshake{Version: 3, ID: node0.ID, Caps: []Cap{{"a", 0}, {"b", 2}}}

		prv1, _ = crypto.GenerateKey()
		node1   = &discover.Node{ID: discover.PubkeyID(&prv1.PublicKey), IP: net.IP{5, 6, 7, 8}, TCP: 44}
		hs1     = &protoHandshake{Version: 3, ID: node1.ID, Caps: []Cap{{"c", 1}, {"d", 3}}}

		wg sync.WaitGroup
	)

	// 创建通信管道
	fd0, fd1, err := tcpPipe()
	if err != nil {
		t.Fatal(err)
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		defer fd0.Close()
		// 创建fd0一端
		rlpx := newRLPX(fd0)
		// 与fd1端握手
		remid, err := rlpx.doEncHandshake(prv0, node1)
		if err != nil {
			t.Errorf("dial side enc handshake failed: %v", err)
			return
		}
		fmt.Println("doEncHandshake: node0 ==> node1 ")

		// 检查nodeID是否一致
		if remid != node1.ID {
			t.Errorf("dial side remote id mismatch: got %v, want %v", remid, node1.ID)
			return
		}
		fmt.Println("node0的remoteID与node1一致 ")

		phs, err := rlpx.doProtoHandshake(hs0)
		if err != nil {
			t.Errorf("dial side proto handshake error: %v", err)
			return
		}
		fmt.Println("doProtoHandshake: node0 ==> node1")

		phs.Rest = nil
		if !reflect.DeepEqual(phs, hs1) {
			t.Errorf("dial side proto handshake mismatch:\n got: %s\n want: %s\n", spew.Sdump(phs), spew.Sdump(hs1))
			return
		}

		fmt.Println("node0关闭连接")
		rlpx.close(DiscQuitting)
	}()

	go func() {
		defer wg.Done()
		defer fd1.Close()
		rlpx := newRLPX(fd1)
		remid, err := rlpx.doEncHandshake(prv1, nil)
		if err != nil {
			t.Errorf("listen side env handshake failed: %v", err)
			return
		}
		fmt.Println("doEncHandshake: node1 ==> node0 ")

		if remid != node0.ID {
			t.Errorf("listen side remote id mismatch: got %v, want %v", remid, node0.ID)
			return
		}

		phs, err := rlpx.doProtoHandshake(hs1)
		if err != nil {
			t.Errorf("listen side proto handshake error: %v", err)
			return
		}
		fmt.Println("doProtoHandshake: node 1 ==> node0")

		phs.Rest = nil
		if !reflect.DeepEqual(phs, hs0) {
			t.Errorf("listen side proto handshake mismatch: \ngot: %s\n wan: %s\n", spew.Sdump(phs), spew.Sdump(hs0))
			return
		}

		if err := ExpectMsg(rlpx, discMsg, []DiscReason{DiscQuitting}); err != nil {
			t.Errorf("error receiving disconnect: %v", err)
		}
		fmt.Println("node1读取到断开连接的消息")
	}()

	wg.Wait()
}

func TestProtocolHandshakeErrors(t *testing.T) {
	our := &protoHandshake{
		Version: 3,
		Name:    "quux",
		Caps: []Cap{
			{"foo", 2},
			{"bar", 3},
		},
	}

	tests := []struct {
		code uint64
		msg  interface{}
		err  error
	}{
		{
			code: discMsg,
			msg:  []DiscReason{DiscQuitting},
			err:  DiscQuitting,
		},
		{
			code: 0x989898,
			msg:  []byte{1},
			err:  errors.New("expected handshake, got 989898"),
		},
		{
			code: handshakeMsg,
			msg:  make([]byte, baseProtocolMaxMsgSize+2),
			err:  errors.New("message too big"),
		},
		{
			code: handshakeMsg,
			msg:  []byte{1, 2, 3},
			err:  newPeerError(errInvalidMsg, "(code 0) (size 4) rlp: expected input list for p2p.protoHandshake"),
		},
		{
			code: handshakeMsg,
			msg:  &protoHandshake{Version: 3},
			err:  DiscInvalidIdentity,
		},
	}

	for i, test := range tests {
		p1, p2 := MsgPipe()
		// p1向p2发送test消息
		go Send(p1, test.code, test.msg)
		// 从p2端读取Handshake消息
		_, err := readProtocolHandshake(p2, our)
		// 比较返回的结果
		if !reflect.DeepEqual(err, test.err) {
			t.Errorf("test %d: error mismatch: got %q, want %q", i, err, test.err)
		}
	}
}

type fakeHash []byte

func (fakeHash) Write(p []byte) (int, error) {
	return len(p), nil
}

func (fakeHash) Reset() {}

func (fakeHash) BlockSize() int {
	return 0
}

func (h fakeHash) Size() int {
	return len(h)
}

func (h fakeHash) Sum(b []byte) []byte {
	return append(b, h...)
}

func TestRLPXFrameFake(t *testing.T) {
	buf := new(bytes.Buffer)
	hash := fakeHash([]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	rw := newRLPXFrameRW(buf, secrets{
		AES:        crypto.Keccak256(),
		MAC:        crypto.Keccak256(),
		IngressMAC: hash,
		EgressMAC:  hash,
	})

	golden := unhex(`
		00828ddae471818bb0bfa6b551d1cb42
		01010101010101010101010101010101
		ba628a4ba590cb43f7848f41c4382885
		01010101010101010101010101010101
		`)

	// 将数据通过rlpxFrameRW写入buf
	if err := Send(rw, 8, []uint{1, 2, 3, 4}); err != nil {
		t.Fatalf("WriteMsg error: %v", err)
	}
	// 比较buf中的数据和预期是否一致
	written := buf.Bytes()
	if !bytes.Equal(written, golden) {
		t.Fatalf("output mismatch:\n got: %x\n want: %x", written, golden)
	}

	// 读取消息
	msg, err := rw.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg error: %v", err)
	}
	if msg.Size != 5 {
		t.Errorf("msg size mismatch: got %d, want %d", msg.Size, 5)
	}
	if msg.Code != 8 {
		t.Errorf("msg code mismatch: got %d, want %d", msg.Code, 8)
	}
	// 读取消息携带的数据
	payload, _ := ioutil.ReadAll(msg.Payload)
	wantPayload := unhex("C401020304") // rlp编码的数据

	// 比较携带的数据和预期是否一致
	if !bytes.Equal(payload, wantPayload) {
		t.Errorf("msg payload mismatch:\ngot %x\nwant %x", payload, wantPayload)
	}
}

func TestRLPXFrameRW(t *testing.T) {
	var (
		aesSecret      = make([]byte, 16)
		macSecret      = make([]byte, 16)
		egressMACinit  = make([]byte, 32)
		ingressMACinit = make([]byte, 32)
	)
	for _, s := range [][]byte{aesSecret, macSecret, egressMACinit, ingressMACinit} {
		rand.Read(s)
	}

	// 使用buffer模拟通信通道
	conn := new(bytes.Buffer)

	// 构建1端的共享密钥
	s1 := secrets{
		AES:        aesSecret,
		MAC:        macSecret,
		EgressMAC:  sha3.NewKeccak256(),
		IngressMAC: sha3.NewKeccak256(),
	}

	// 构建1端的读写器
	s1.EgressMAC.Write(egressMACinit)
	s1.IngressMAC.Write(ingressMACinit)
	rw1 := newRLPXFrameRW(conn, s1)

	// 构建2端的共享密钥
	s2 := secrets{
		AES:        aesSecret,
		MAC:        macSecret,
		EgressMAC:  sha3.NewKeccak256(),
		IngressMAC: sha3.NewKeccak256(),
	}

	// 构建2端的读写器
	s2.EgressMAC.Write(ingressMACinit)
	s2.IngressMAC.Write(egressMACinit)
	rw2 := newRLPXFrameRW(conn, s2)

	for i := 0; i < 10; i++ {
		wmsg := []interface{}{"foo", "bar", strings.Repeat("test", i)}
		// 从1端写入数据
		err := Send(rw1, uint64(i), wmsg)
		if err != nil {
			t.Fatalf("WriteMsg error (i = %d): %v", i, err)
		}

		// 从2端读出数据
		msg, err := rw2.ReadMsg()
		if err != nil {
			t.Fatalf("ReadMsg error (i=%d): %v", i, err)
		}

		// 比较msg code
		if msg.Code != uint64(i) {
			t.Fatalf("msg code mismatch: got %d, want %d", msg.Code, i)
		}
		// 比较msg payload
		// 检查msg的内容
		// wmsg2 := [3]string{}
		// if err := msg.Decode(&wmsg2); err != nil {
		//  	t.Fatalf("Decode msg error: %v", err)
		// }

		payload, _ := ioutil.ReadAll(msg.Payload)
		wantPayload, _ := rlp.EncodeToBytes(wmsg)
		if !bytes.Equal(payload, wantPayload) {
			t.Fatalf("msg payload mismatch: \ngot %x\n want %x", payload, wantPayload)
		}
	}
}

func tcpPipe() (net.Conn, net.Conn, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		l.Close()
		fmt.Println("tcp Listener Close ")
	}()
	fmt.Println("tcp Listen ")

	var aconn net.Conn
	aerr := make(chan error, 1)
	go func() {
		var err error
		aconn, err = l.Accept()
		fmt.Println("tcp Accept ")
		aerr <- err
	}()

	dconn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		<-aerr
		return nil, nil, nil
	}
	fmt.Println("tcp Dial ")

	if err := <-aerr; err != nil {
		dconn.Close()
		return nil, nil, err
	}
	return aconn, dconn, nil
}
