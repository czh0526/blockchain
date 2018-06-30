package node

import (
	"github.com/czh0526/blockchain/p2p"
	"github.com/czh0526/blockchain/rpc"
)

type Service interface {
	Protocols() []p2p.Protocol
	APIs() []rpc.API
	Start(server *p2p.Server) error
	Stop() error
}

type ServiceContext struct {
}

type ServiceConstructor func(ctx *ServiceContext) (Service, error)
