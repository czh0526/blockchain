package shadow

import (
	"fmt"
	"sync"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/log"
	"github.com/czh0526/blockchain/p2p"
	"github.com/czh0526/blockchain/p2p/discover"
	"github.com/czh0526/blockchain/rpc"
)

type Shadow struct {
	networkId uint64
	protocols []p2p.Protocol

	newPeerCh chan *peer
	quitSync  chan struct{}

	wg sync.WaitGroup
}

func New(networkId uint64) (*Shadow, error) {

	shadow := &Shadow{
		networkId: networkId,
		protocols: []p2p.Protocol{},
	}
	for _, version := range ProtocolVersions {
		version := version
		protocol := p2p.Protocol{
			Name:    "shadow protocol",
			Version: 1,
			Length:  3,
			Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
				peer := shadow.newPeer(int(version), p, rw)
				select {
				case shadow.newPeerCh <- peer:
					shadow.wg.Add(1)
					defer shadow.wg.Done()
					return shadow.handle(peer)

				case <-shadow.quitSync:
					return p2p.DiscQuitting
				}
				return nil
			},
			NodeInfo: func() interface{} {
				return &NodeInfo{
					Network: 333,
				}
			},
			PeerInfo: func(id discover.NodeID) interface{} {
				return nil
			},
		}
		shadow.protocols = append(shadow.protocols, protocol)
	}

	return shadow, nil
}

func (s *Shadow) newPeer(version int, p *p2p.Peer, rw p2p.MsgReadWriter) *peer {
	return &peer{
		Peer:    p,
		rw:      rw,
		version: version,
		id:      fmt.Sprintf("%x", p.ID().Bytes()[:8]),
	}
}

func (s *Shadow) handle(p *peer) error {
	var (
		genesis = common.HexToHash("0x00")
	)

	if err := p.Handshake(s.networkId, genesis); err != nil {
		return err
	}
	log.Debug("[Shadow]: Handshake finished.")

	for {
		if err := s.handleMsg(p); err != nil {
			log.Debug(fmt.Sprintf("Shadow message handling failed, err = %v", err))
			return err
		}
	}
}

func (s *Shadow) handleMsg(p *peer) error {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}

	defer msg.Discard()

	switch {
	case msg.Code == StatusMsg:
		return errResp(ErrExtraStatusMsg, "uncontrolled status message")
	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}
	return nil
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

func errResp(code errCode, format string, v ...interface{}) error {
	return fmt.Errorf("%v - %v", code, fmt.Sprintf(format, v...))
}
