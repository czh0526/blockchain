package discv5

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const nodeIDBits = 512

type NodeID [nodeIDBits / 8]byte

func (id NodeID) Pubkey() (*ecdsa.PublicKey, error) {
	p := &ecdsa.PublicKey{Curve: crypto.S256(), X: new(big.Int), Y: new(big.Int)}
	half := len(id) / 2
	p.X.SetBytes(id[:half])
	p.Y.SetBytes(id[half:])
	if !p.Curve.IsOnCurve(p.X, p.Y) {
		return nil, errors.New("id is invalid secp256k1 curve point")
	}
	return p, nil
}

func (n NodeID) String() string {
	return fmt.Sprintf("%x", n[:])
}

func (n NodeID) GoString() string {
	return fmt.Sprintf("discover.HexID(\"%x\")", n[:])
}

func (n NodeID) TerminalString() string {
	return hex.EncodeToString(n[:8])
}

type Node struct {
	IP       net.IP
	UDP, TCP uint16
	ID       NodeID
	nodeNetGuts
}

func NewNode(id NodeID, ip net.IP, udpPort, tcpPort uint16) *Node {
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	return &Node{
		IP:          ip,
		UDP:         udpPort,
		TCP:         tcpPort,
		ID:          id,
		nodeNetGuts: nodeNetGuts{sha: crypto.Keccak256Hash(id[:])},
	}
}

func (n *Node) Incomplete() bool {
	return n.IP == nil
}

func (n *Node) validateComplete() error {
	if n.Incomplete() {
		return errors.New("incomplete node")
	}
	if n.UDP == 0 {
		return errors.New("missing UDP port")
	}
	if n.TCP == 0 {
		return errors.New("missing TCP port")
	}
	if n.IP.IsMulticast() || n.IP.IsUnspecified() {
		return errors.New("invalid IP (multicast/unspecified)")
	}
	_, err := n.ID.Pubkey()
	return err
}

func (n *Node) String() string {
	u := url.URL{Scheme: "enode"}
	if n.Incomplete() {
		u.Host = fmt.Sprintf("%x", n.ID[:])
	} else {
		addr := net.TCPAddr{IP: n.IP, Port: int(n.TCP)}
		u.User = url.User(fmt.Sprintf("%x", n.ID[:]))
		u.Host = addr.String()
		if n.UDP != n.TCP {
			u.RawQuery = "discport=" + strconv.Itoa(int(n.UDP))
		}
	}
	return u.String()
}

var incompleteNodeURL = regexp.MustCompile("(?i)^(?:enode://)?([0-9a-f]+)$")

func ParseNode(rawurl string) (*Node, error) {
	if m := incompleteNodeURL.FindStringSubmatch(rawurl); m != nil {
		id, err := HexID(m[1])
		if err != nil {
			return nil, fmt.Errorf("invalid node ID (%v)", err)
		}
		return NewNode(id, nil, 0, 0), nil
	}
	return parseComplete(rawurl)
}

func HexID(in string) (NodeID, error) {
	var id NodeID
	b, err := hex.DecodeString(strings.TrimPrefix(in, "0x"))
	if err != nil {
		return id, err
	} else if len(b) != len(id) {
		return id, fmt.Errorf("wrong length, want %d hex chars", len(id)*2)
	}
	copy(id[:], b)
	return id, nil
}

func MustHexID(in string) NodeID {
	id, err := HexID(in)
	if err != nil {
		panic(err)
	}
	return id
}

// 根据 ecdsa.PublicKey 构建 NodeID
func PubkeyID(pub *ecdsa.PublicKey) NodeID {
	var id NodeID
	pbytes := elliptic.Marshal(pub.Curve, pub.X, pub.Y)
	if len(pbytes)-1 != len(id) {
		panic(fmt.Errorf("need %d bit pubkey, got %d bits", (len(id)+1)*8, len(pbytes)))
	}
	copy(id[:], pbytes[1:])
	return id

}

// 根据内容和签名，恢复NodeID
func recoverNodeID(hash, sig []byte) (id NodeID, err error) {
	pubkey, err := crypto.Ecrecover(hash, sig)
	if err != nil {
		return id, err
	}

	if len(pubkey)-1 != len(id) {
		return id, fmt.Errorf("recovered pubkey has %d bits, want %d bits", len(pubkey)*8, (len(id)+1)*8)
	}
	for i := range id {
		id[i] = pubkey[i+1]
	}
	return id, nil
}

func parseComplete(rawurl string) (*Node, error) {
	var (
		id               NodeID
		ip               net.IP
		tcpPort, udpPort uint64
	)
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "enode" {
		return nil, errors.New("invalid URL scheme, want \"enode\"")
	}
	if u.User == nil {
		return nil, errors.New("does not contain node ID")
	}
	if id, err = HexID(u.User.String()); err != nil {
		return nil, fmt.Errorf("invalid node ID (%v)", err)
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid host: %v", err)
	}
	if ip = net.ParseIP(host); ip == nil {
		return nil, errors.New("invalid IP address")
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	if tcpPort, err = strconv.ParseUint(port, 10, 16); err != nil {
		return nil, errors.New("invalid port")
	}
	udpPort = tcpPort
	qv := u.Query()
	if qv.Get("discport") != "" {
		udpPort, err = strconv.ParseUint(qv.Get("discport"), 10, 16)
		if err != nil {
			return nil, errors.New("invalid discport in query")
		}
	}
	return NewNode(id, ip, uint16(udpPort), uint16(tcpPort)), nil
}

// 两地址异或运算后得到256个结果，对应8个可变距离。
var lzcount = [256]int{
	8, 7, 6, 6, 5, 5, 5, 5, // 1
	4, 4, 4, 4, 4, 4, 4, 4, // 2
	3, 3, 3, 3, 3, 3, 3, 3, // 3
	3, 3, 3, 3, 3, 3, 3, 3, // 4
	2, 2, 2, 2, 2, 2, 2, 2, // 5
	2, 2, 2, 2, 2, 2, 2, 2, // 6
	2, 2, 2, 2, 2, 2, 2, 2, // 7
	2, 2, 2, 2, 2, 2, 2, 2, // 8
	1, 1, 1, 1, 1, 1, 1, 1, // 9
	1, 1, 1, 1, 1, 1, 1, 1, // 10
	1, 1, 1, 1, 1, 1, 1, 1, // 11
	1, 1, 1, 1, 1, 1, 1, 1, // 12
	1, 1, 1, 1, 1, 1, 1, 1, // 13
	1, 1, 1, 1, 1, 1, 1, 1, // 14
	1, 1, 1, 1, 1, 1, 1, 1, // 15
	1, 1, 1, 1, 1, 1, 1, 1, // 16
	0, 0, 0, 0, 0, 0, 0, 0, // 17
	0, 0, 0, 0, 0, 0, 0, 0, // 18
	0, 0, 0, 0, 0, 0, 0, 0, // 19
	0, 0, 0, 0, 0, 0, 0, 0, // 20
	0, 0, 0, 0, 0, 0, 0, 0, // 21
	0, 0, 0, 0, 0, 0, 0, 0, // 22
	0, 0, 0, 0, 0, 0, 0, 0, // 23
	0, 0, 0, 0, 0, 0, 0, 0, // 24
	0, 0, 0, 0, 0, 0, 0, 0, // 25
	0, 0, 0, 0, 0, 0, 0, 0, // 26
	0, 0, 0, 0, 0, 0, 0, 0, // 27
	0, 0, 0, 0, 0, 0, 0, 0, // 28
	0, 0, 0, 0, 0, 0, 0, 0, // 29
	0, 0, 0, 0, 0, 0, 0, 0, // 30
	0, 0, 0, 0, 0, 0, 0, 0, // 31
	0, 0, 0, 0, 0, 0, 0, 0, // 32
}

// 逻辑距离 = 前导零个数
func logdist(a, b common.Hash) int {
	lz := 0
	for i := range a {
		x := a[i] ^ b[i]
		if x == 0 {
			lz += 8
		} else {
			lz += lzcount[x]
			break
		}
	}
	return len(a)*8 - lz
}

func distcmp(target, a, b common.Hash) int {
	for i := range target {
		da := a[i] ^ target[i]
		db := b[i] ^ target[i]
		if da > db {
			return 1
		} else if da < db {
			return -1
		}
	}
	return 0
}

// 11010010 10010010 00011100 01010101 10101010
//                        ^19
func hashAtDistance(a common.Hash, n int) (b common.Hash) {
	if n == 0 {
		return a
	}

	b = a
	// pos: 定位字节位置
	pos := len(a) - n/8 - 1
	// bit: 定位字节内的bit位置
	bit := byte(0x01) << (byte(n%8) - 1)
	if bit == 0 {
		pos++
		bit = 0x08
	}
	// 计算字节内容
	b[pos] = a[pos]&^bit | ^a[pos]&bit
	// 随机填充pos后面的字节位置
	for i := pos + 1; i < len(a); i++ {
		b[i] = byte(rand.Intn(255))
	}
	return b
}
