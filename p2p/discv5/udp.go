package discv5

import (
	"crypto/ecdsa"
	"net"

	"github.com/czh0526/blockchain/log"
)

type ingressPacket struct {
	remoteID   NodeID
	remoteAddr *net.UDPAddr
	ev         nodeEvent
	hash       []byte
	data       interface{} // one of the RPC structs
	rawData    []byte
}

type conn interface {
	ReadFromUDP(b []byte) (n int, addr *net.UDPAddr, err error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (n int, err error)
	Close() error
	LocalAddr() net.Addr
}

type udp struct {
	conn conn
	priv *ecdsa.PrivateKey
	net  *Network
}

func ListenUDP(priv *ecdsa.PrivateKey, conn conn, realaddr *net.UDPAddr, nodeDBPath string) (*Network, error) {
	transport, err := listenUDP(priv, conn, realaddr)
	if err != nil {
		return nil, err
	}

	net, err := newNetwork(transport, priv.PublicKey, nodeDBPath)
	if err != nil {
		return nil, err
	}

	log.Info("UDP listener up", "net", net.tab.self)
	transport.net = net
	go transport.readLoop()
	return net, nil
}

func listenUDP(priv *ecdsa.PrivateKey, conn conn, realaddr *net.UDPAddr) (*udp, error) {
	return &udp{conn: conn, priv: priv}, nil
}
