package p2p

import (
	"crypto/ecdsa"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
)

const (
	frameReadTimeout  = 30 * time.Second
	frameWriteTimeout = 20 * time.Second
)

type transport interface {
	MsgReadWriter

	doEncHandshake(prv *ecdsa.PrivateKey, dialDest *discover.Node) (discover.NodeID, error)
	doProtoHandshake(our *protoHandshake) (their *protoHandshake, err error)

	close(err error)
}
