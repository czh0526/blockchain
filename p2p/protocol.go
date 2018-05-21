package p2p

import "github.com/ethereum/go-ethereum/p2p/discover"

type Cap struct {
	Name    string
	Version uint
}

type capsByNameAndVersion []Cap

func (cs capsByNameAndVersion) Len() int      { return len(cs) }
func (cs capsByNameAndVersion) Swap(i, j int) { cs[i], cs[j] = cs[j], cs[i] }
func (cs capsByNameAndVersion) Less(i, j int) bool {
	if cs[i].Name < cs[j].Name {
		return true
	}

	if cs[i].Name == cs[j].Name && cs[i].Version < cs[j].Version {
		return true
	}

	return false
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
