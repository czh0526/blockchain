package shadow

import (
	"time"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/p2p"
)

const (
	handshakeTimeout = 5 * time.Second
)

type PeerInfo struct {
	Version int `json:"version"`
}

type peer struct {
	*p2p.Peer

	rw      p2p.MsgReadWriter
	id      string
	version int
}

func (p *peer) Info() *PeerInfo {
	return &PeerInfo{
		Version: p.version,
	}
}

func (p *peer) Handshake(network uint64, genesis common.Hash) error {
	errc := make(chan error, 2)
	var status statusData

	// 启动发送例程
	go func() {
		errc <- p2p.Send(p.rw, StatusMsg, &statusData{
			ProtocolVersion: uint32(p.version),
			NetworkId:       network,
			GenesisBlock:    genesis,
		})
	}()

	// 启动接收例程
	go func() {
		errc <- p.readStatus(network, &status, genesis)
	}()

	// 启动读操作的超时检测
	timeout := time.NewTimer(handshakeTimeout)
	defer timeout.Stop()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errc:
			if err != nil {
				return err
			}
		case <-timeout.C:
			return p2p.DiscReadTimeout
		}
	}
	return nil
}

func (p *peer) readStatus(network uint64, status *statusData, genesis common.Hash) (err error) {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != StatusMsg {
		return errResp(ErrNoStatusMsg, "first msg has code %x (!= %x)", msg.Code, StatusMsg)
	}

	if err := msg.Decode(&status); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}
	if status.GenesisBlock != genesis {
		return errResp(ErrGenesisBlockMismatch, "%x (!= %x)", status.GenesisBlock[:8], genesis[:8])
	}
	if status.NetworkId != network {
		return errResp(ErrNetworkIdMismatch, "%d(!= %d)", status.NetworkId, network)
	}
	if int(status.ProtocolVersion) != p.version {
		return errResp(ErrProtocolVersionMismatch, "%d (!= %d)", status.ProtocolVersion, p.version)
	}

	return nil
}
