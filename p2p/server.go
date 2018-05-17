package p2p

import (
	"crypto/ecdsa"
	"fmt"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
)

const (
	frameReadTimeout  = 30 * time.Second
	frameWriteTimeout = 20 * time.Second
)

type transport interface {
	MsgReadWriter

	doEncHandshake(prv *ecdsa.PrivateKey, dialDest *discover.Node) (discover.NodeID, error)
	doProtoHandshake(our *protoHandshake) (their *protoHandshake, err error)

	close(err error)
}

type connFlag int

const (
	dynDialedConn connFlag = 1 << iota
	staticDialedConn
	inboundConn
	trustedConn
)

type conn struct {
	fd net.Conn
	transport
	flags connFlag
	cont  chan error
	id    discover.NodeID
	caps  []Cap
	name  string
}

type Config struct {
	PrivateKey *ecdsa.PrivateKey `toml:"-"`
	MaxPeers   int
	Name       string `toml:"-"`
	ListenAddr string
}

type Server struct {
	Config
	newTransport func(net.Conn) transport
	newPeerHook  func(*Peer)
}

func (srv *Server) Start() (err error) {
	srv.running = true

	if srv.PrivateKey == nil {
		return fmt.Errorf("Server.PrivateKey must be set to a non-nil key")
	}
	if srv.newTransport == nil {
		srv.newTransport = newRLPX
	}
	if srv.Dialer == nil {
		srv.Dialer = TCPDialer(&net.Dialer{Timeout: defaultialTimeout})
	}

	var (
		conn      *net.UDPConn
		sconn     *sharedUDPConn
		realaddr  *net.UDPAddr
		unhandled chan discover.ReadPacket
	)

	srv.ourHandshake = &protoHandshake{
		Version: baseProtocolVersion,
		Name:    srv.Name,
		ID:      discover.PublicID(&srv.Privatekey.PublicKey),
	}
	for _, p := range srv.Protocols {
		srv.outHandshake.Caps = append(srv.ourHandshake.Caps, p.cap())
	}
	if srv.ListenAddr != "" {
		if err := srv.startListening(); err != nil {
			return err
		}
		if srv.NoDial && srv.ListenAddr == "" {
			srv.log.Warn("P2P server will be useless, neither dialing nor listening")
		}
	}

}

type sharedUDPConn struct {
	*net.UDPConn
	unhandled chan discover.ReadPacket
}
