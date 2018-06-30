package node

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/czh0526/blockchain/crypto"
	"github.com/czh0526/blockchain/log"
	"github.com/czh0526/blockchain/p2p"
	"github.com/czh0526/blockchain/rpc"
)

type Node struct {
	serverConfig p2p.Config
	server       *p2p.Server

	// Services
	serviceFuncs []ServiceConstructor
	services     map[reflect.Type]Service

	// RPC inproc
	rpcAPIs       []rpc.API
	inprocHandler *rpc.Server

	// RPC ipc
	ipcEndpoint string
	ipcListener net.Listener
	ipcHandler  *rpc.Server

	// RPC http
	httpEndpoint  string
	httpWhitelist []string
	httpListener  net.Listener
	httpHandler   *rpc.Server

	// RPC websocket
	wsEndpoint string
	wsListener net.Listener
	wsHandler  *rpc.Server

	stop chan struct{}
	lock sync.RWMutex
}

func New() (*Node, error) {
	return &Node{
		serviceFuncs: []ServiceConstructor{},
		ipcEndpoint:  `\\.\pipe\tri-shadow.ipc`,
		httpEndpoint: "127.0.0.1:8545",
		wsEndpoint:   "127.0.0.1:8546",
	}, nil
}

func (n *Node) Register(constructor ServiceConstructor) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.server != nil {
		return ErrNodeRunning
	}
	n.serviceFuncs = append(n.serviceFuncs, constructor)
	return nil
}

func (n *Node) Start() error {
	n.lock.Lock()
	defer n.lock.Unlock()

	if n.server != nil {
		return ErrNodeRunning
	}

	currDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return err
	}
	keyfilepath := filepath.Join(currDir, "nodekey")
	key, err := crypto.LoadECDSA(keyfilepath)
	if err != nil {
		return err
	}

	n.serverConfig = p2p.Config{
		PrivateKey: key,
		ListenAddr: ":30308",
	}

	// 构建 p2p.Server
	running := &p2p.Server{Config: n.serverConfig}

	// 构建 node.Service
	services := make(map[reflect.Type]Service)
	for _, constructor := range n.serviceFuncs {

		ctx := &ServiceContext{}

		service, err := constructor(ctx)
		if err != nil {
			return err
		}
		kind := reflect.TypeOf(service)
		if _, exists := services[kind]; exists {
			return &DuplicateServiceError{Kind: kind}
		}
		services[kind] = service
	}

	// 设置 p2p.Server Protocols
	for _, service := range services {
		running.Protocols = append(running.Protocols, service.Protocols()...)
	}

	// 启动 p2p.Server
	log.Info("[Node]: start p2p server ... ")
	if err := running.Start(); err != nil {
		return err
	}

	// 启动 node.Service
	for _, service := range services {
		if err := service.Start(running); err != nil {
			running.Stop()
			return err
		}
	}

	// 启动 RPC [in-proc, ipc, http, websocket]
	log.Info("[Node]: start rpc server ... ")
	if err := n.startRPC(services); err != nil {
		return err
	}

	n.server = running
	n.stop = make(chan struct{})
	return nil
}

func (n *Node) startRPC(services map[reflect.Type]Service) error {
	apis := make([]rpc.API, 0)
	for _, service := range services {
		apis = append(apis, service.APIs()...)
	}

	// in-proc
	if err := n.startInProc(apis); err != nil {
		return err
	}
	log.Debug("[Node]: RPC [in-proc] module started.")

	// ipc
	if err := n.startIPC(apis); err != nil {
		return err
	}
	log.Debug(fmt.Sprintf("[Node]: RPC [ipc] module started: %s", n.ipcEndpoint))

	// http
	if err := n.startHTTP(n.httpEndpoint, apis, []string{}, []string{}, []string{}); err != nil {
		return err
	}
	log.Debug(fmt.Sprintf("[Node]: RPC [http] module started: %s", n.httpEndpoint))

	if err := n.startWS(n.wsEndpoint, apis, []string{}, []string{}, false); err != nil {
		return err
	}
	log.Debug(fmt.Sprintf("[Node]: RPC [websocket] module started: %s", n.wsEndpoint))

	n.rpcAPIs = apis
	return nil
}

func (n *Node) startInProc(apis []rpc.API) error {
	handler := rpc.NewServer()
	for _, api := range apis {
		if err := handler.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
	}
	n.inprocHandler = handler
	return nil
}

func (n *Node) startIPC(apis []rpc.API) error {
	if n.ipcEndpoint == "" {
		return nil // IPC disabled
	}

	listener, handler, err := rpc.StartIPCEndpoint(n.ipcEndpoint, apis)
	if err != nil {
		return err
	}

	n.ipcListener = listener
	n.ipcHandler = handler
	return nil
}

func (n *Node) startHTTP(endpoint string, apis []rpc.API, modules []string, cors []string, vhosts []string) error {
	if endpoint == "" {
		return nil
	}

	listener, handler, err := rpc.StartHTTPEndpoint(endpoint, apis, modules, cors, vhosts)
	if err != nil {
		return err
	}

	n.httpEndpoint = endpoint
	n.httpListener = listener
	n.httpHandler = handler

	return nil
}

func (n *Node) startWS(endpoint string, apis []rpc.API, modules []string, wsOrigins []string, exposeAll bool) error {
	if endpoint == "" {
		return nil
	}
	listener, handler, err := rpc.StartWSEndpoint(endpoint, apis, modules, wsOrigins, exposeAll)
	if err != nil {
		return err
	}

	n.wsEndpoint = endpoint
	n.wsListener = listener
	n.wsHandler = handler

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
