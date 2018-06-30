package rpc

import (
	"context"
	"net"
)

// 向服务器打通一个 net.Conn 通道
func DialInProc(handler *Server) *Client {
	initctx := context.Background()
	c, _ := newClient(initctx, func(context.Context) (net.Conn, error) {
		p1, p2 := net.Pipe()
		go handler.ServeCodec(NewJSONCodec(p1), OptionMethodInvocation|OptionSubscriptions)
		return p2, nil
	})
	return c
}
