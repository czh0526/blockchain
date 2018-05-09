package p2p

import (
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/p2p/discover"
)

type transport interface {
	doEncHandshake(prv *ecdsa.PrivateKey, dialDest *discover.Node)(discover.NodeID, error) 
	
	close(err error)
}