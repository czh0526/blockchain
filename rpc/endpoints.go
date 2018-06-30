package rpc

import (
	"fmt"
	"net"

	"github.com/czh0526/blockchain/log"
)

func StartIPCEndpoint(ipcEndpoint string, apis []API) (net.Listener, *Server, error) {
	// 构建 Server 对象
	handler := NewServer()
	// 注册 API 对象
	for _, api := range apis {
		if err := handler.RegisterName(api.Namespace, api.Service); err != nil {
			return nil, nil, err
		}
		log.Debug(fmt.Sprintf("[RPC]: IPC registered, namespace = %s ", api.Namespace))
	}

	// 启动 Listener
	listener, err := ipcListen(ipcEndpoint)
	if err != nil {
		return nil, nil, err
	}

	// 启动 IPC 服务, 守护 Listener
	go handler.ServeListener(listener)
	return listener, handler, nil
}

func StartHTTPEndpoint(endpoint string, apis []API, modules []string, cors []string, vhosts []string) (net.Listener, *Server, error) {
	whitelist := make(map[string]bool)
	for _, module := range modules {
		whitelist[module] = true
	}

	// 构建 Server 对象
	handler := NewServer()

	// 注册 Service 对象
	for _, api := range apis {
		if whitelist[api.Namespace] || (len(whitelist) == 0 && api.Public) {
			if err := handler.RegisterName(api.Namespace, api.Service); err != nil {
				return nil, nil, err
			}
			log.Debug("HTTP registered", "namespace", api.Namespace)
		}
	}

	var (
		listener net.Listener
		err      error
	)
	// 启动 Listener
	if listener, err = net.Listen("tcp", endpoint); err != nil {
		return nil, nil, err
	}

	// 启动HTTP服务，守护 Listener
	go NewHTTPServer(cors, vhosts, handler).Serve(listener)
	return listener, handler, err
}

func StartWSEndpoint(endpoint string, apis []API, modules []string, wsOrigins []string, exposeAll bool) (net.Listener, *Server, error) {
	whitelist := make(map[string]bool)
	for _, module := range modules {
		whitelist[module] = true
	}

	// 构建 Server 对象
	handler := NewServer()
	// 注册 Service 对象
	for _, api := range apis {
		if exposeAll || whitelist[api.Namespace] || (len(whitelist) == 0 && api.Public) {
			if err := handler.RegisterName(api.Namespace, api.Service); err != nil {
				return nil, nil, err
			}
			log.Debug("WebSocket registered", "service", api.Service, "namespace", api.Namespace)
		}
	}

	// 启动 Listener
	var (
		listener net.Listener
		err      error
	)
	if listener, err = net.Listen("tcp", endpoint); err != nil {
		return nil, nil, err
	}

	// 启动服务，守护 Listener
	go NewWSServer(wsOrigins, handler).Serve(listener)
	return listener, handler, err
}
