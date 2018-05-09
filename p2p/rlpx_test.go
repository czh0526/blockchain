package p2p

import ( 
	"fmt"
	"net"
	"time"
	"bytes"
	"testing"
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

	// 使用0的PrivateKey和1的PublicKey构建共享对称密钥
	ss0, err := ecies.ImportECDSA(prv0).GenerateShared(ecies.ImportECDSAPublic(pub1), sskLen, sskLen)
	if err != nil {
		return
	}

	ss1, err := ecies.ImportECDSA(prv1).GenerateShared(ecies.ImportECDSAPublic(pub0), sskLen, sskLen)
	if err != nil {
		return
	}

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
}

func testEncHandshake(token []byte) error {
	type result struct {
		side string 
		id discover.NodeID
		err error
	}
	
	var (
		prv0, _ = crypto.GenerateKey()
		prv1, _ = crypto.GenerateKey()
		fd0, fd1 = net.Pipe()
		c0, c1 = newRLPX(fd0).(*rlpx), newRLPX(fd1).(*rlpx)
		output = make(chan result)
	)

	go func() {
		r := result{side: "initiator"}
		defer func() {output <- r} ()
		defer fd0.Close()

		dest := &discover.Node{ID:discover.PubkeyID(&prv1.PublicKey)}
		r.id, r.err = c0.doEncHandshake(prv0, dest)
		if r.err != nil {
			return
		}

		id1 := discover.PubkeyID(&prv1.PublicKey)
		if r.id != id1 {
			r.err = fmt.Errorf("remote ID mismatch: got %v, want: %v", r.id, id1)
		}
	}()

	go func() {
		r := result{side: "receiver"}
		defer func() { output <- r} ()
		defer fd1.Close()

		r.id, r.err = c1.doEncHandshake(prv1, nil)
		if r.err != nil {
			return
		}

		id0 := discover.PubkeyID(&prv0.PublicKey)
		if r.id != id0 {
			r.err = fmt.Errorf("remote ID mismatch: got %v, want: %v", r.id, id0)
		}
	}()

	r1, r2 := <-output, <-output
	if r1.err != nil {
		return fmt.Errorf("%s side error: %v", r1.side, r1.err)
	}
	if r2.err != nil {
		return fmt.Errorf("%s side error: %v", r2.side, r2.err)
	}

	return nil
}
