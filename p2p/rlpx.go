package p2p

import (
	"fmt"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"crypto/ecdsa"
	"sync"
	"time"
	"net"
)

const (
	sskLen = 16

	handshakeTimeout = 5 * time.Second
)

type rlpx struct {
	fd net.Conn
	rmu, wmu sync.Mutex
}

func (t *rlpx) doEncHandshake(prv *ecdsa.PrivateKey, dial *discover.Node) (discover.NodeID, error) {
	fmt.Println("doEncHandshake()")

	return discover.NodeID{}, nil
}

func (t *rlpx) close(err error) {
	t.wmu.Lock()
	defer t.wmu.Unlock()

	t.fd.Close()
}

func newRLPX(fd net.Conn) transport {
	fd.SetDeadline(time.Now().Add(handshakeTimeout))
	return &rlpx{fd: fd}
}