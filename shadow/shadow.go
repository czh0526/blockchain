package shadow

import (
	"fmt"

	"github.com/czh0526/blockchain/p2p"
	"github.com/czh0526/blockchain/rpc"
)

type Shadow struct {
	protocols []p2p.Protocol
}

func New() (*Shadow, error) {

	protocol := p2p.Protocol{
		Name:    "shadow protocol",
		Version: 1,
		Length:  3,
		Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
			fmt.Println("shadow protocol Run()")
			return nil
		},
		NodeInfo: func() interface{} {
			return &NodeInfo{
				Network: 333,
			}
		},
		PeerInfo: func(id discover.NodeInfo) interface{} {
			return nil
		},
	}
	return &Shadow{
		protocols: []p2p.Protocol{protocol},
	}, nil
}

func (s *Shadow) APIs() []rpc.API {
	return []rpc.API{
		{
			Namespace: "shadow",
			Version:   "1.0",
			Service:   &Svc1{name: "Svc1 Name."},
			Public:    true,
		},
	}
}

func (s *Shadow) Protocols() []p2p.Protocol {
	return s.protocols
}

func (s *Shadow) Start(server *p2p.Server) error {
	return nil
}

func (s *Shadow) Stop() error {
	return nil
}
