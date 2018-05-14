package p2p

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/ecies"
	"github.com/ethereum/go-ethereum/p2p/discover"
)

func TestSharedSecret(t *testing.T) {
	//构建 ecdsa.PrivateKey
	prv0, _ := crypto.GenerateKey()
	pub0 := &prv0.PublicKey
	prv1, _ := crypto.GenerateKey()
	pub1 := &prv1.PublicKey

	// 使用0的PrivateKey和1的PublicKey构建共享的对称密钥
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
		fmt.Println("node0 doEncHandshake ")

		// 检查nodeID是否一致
		if remid != node1.ID {
			t.Errorf("dial side remote id mismatch: got %v, want %v", remid, node1.ID)
			return
		}

		phs, err := rlpx.doProtoHandshake(hs0)
		if err != nil {
			t.Errorf("dial side proto handshake error: %v", err)
			return
		}
		fmt.Println("node0 doProtoHandshake() ")

		phs.Rest = nil
		if !reflect.DeepEqual(phs, hs1) {
			t.Errorf("dial side proto handshake mismatch:\n got: %s\n want: %s\n", spew.Sdump(phs), spew.Sdump(hs1))
			return
		}
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
		fmt.Println("node1 doEncHandshake ")

		if remid != node0.ID {
			t.Errorf("listen side remote id mismatch: got %v, want %v", remid, node0.ID)
			return
		}

		phs, err := rlpx.doProtoHandshake(hs1)
		if err != nil {
			t.Errorf("listen side proto handshake error: %v", err)
			return
		}
		fmt.Println("node1 doProtoHandshake ")

		phs.Rest = nil
		if !reflect.DeepEqual(phs, hs0) {
			t.Errorf("listen side proto handshake mismatch: \ngot: %s\n wan: %s\n", spew.Sdump(phs), spew.Sdump(hs0))
			return
		}

		if err := ExpectMsg(rlpx, discMsg, []DiscReason{DiscQuitting}); err != nil {
			t.Errorf("error receiving disconnect: %v", err)
		}
	}()

	wg.Wait()
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
