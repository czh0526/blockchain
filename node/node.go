package node

import (
	"sync"

	"github.com/czh0526/blockchain/crypto"
	"github.com/czh0526/blockchain/p2p"
)

type Node struct {
	serverConfig p2p.Config
	server       *p2p.Server

	stop chan struct{}
	lock sync.RWMutex
}

func New() (*Node, error) {
	return &Node{}, nil
}

func (n *Node) Start() error {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.server != nil {
		return ErrNodeRunning
	}

	key, err := crypto.GenerateKey()
	if err != nil {
		return err
	}

	n.serverConfig = p2p.Config{
		PrivateKey: key,
	}
	running := &p2p.Server{Config: n.serverConfig}
	if err := running.Start(); err != nil {
		return err
	}

	n.server = running
	n.stop = make(chan struct{})
	return nil
}

func (n *Node) Wait() {
	n.lock.RLock()
	if n.server == nil {
		n.lock.RUnlock()
		return
	}
	stop := n.stop
	n.lock.RUnlock()

	<-stop
}
