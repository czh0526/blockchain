package p2p

import "github.com/ethereum/go-ethereum/p2p/discover"

type Cap struct {
	Name    string
	Version uint
}

type Protocol struct {
	Name     string
	Version  uint
	Length   uint64
	Run      func(peer *Peer, rw MsgReadWriter) error
	NodeInfo func() interface{}
	PeerInfo func(id discover.NodeID) interface{}
}

func (p Protocol) cap() Cap {
	return Cap{p.Name, p.Version}
}
