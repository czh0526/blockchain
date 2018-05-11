package p2p

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	mrand "math/rand"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/ecies"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/ethereum/go-ethereum/crypto/sha3"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	sskLen = 16 // shared key length
	sigLen = 65 // elliptic S256
	pubLen = 64 // 512 bit pubkey in uncompressed representation without format byte
	shaLen = 32 // hash length

	authMsgLen  = sigLen + shaLen + pubLen + shaLen + 1
	authRespLen = pubLen + shaLen + 1

	eciesOverhead = 65 /* pubkey */ + 16 /* IV */ + 32 /* MAC */

	encAuthMsgLen  = authMsgLen + eciesOverhead
	encAuthRespLen = authRespLen + eciesOverhead

	handshakeTimeout = 5 * time.Second
)

type rlpx struct {
	fd       net.Conn
	rmu, wmu sync.Mutex
}

type secrets struct {
	RemoteID              discover.NodeID
	AES, MAC              []byte
	EgressMAC, IngressMAC hash.Hash
	Token                 []byte
}

type encHandshake struct {
	initiator bool
	remoteID  discover.NodeID

	remotePub            *ecies.PublicKey
	initNonce, respNonce []byte
	randomPrivKey        *ecies.PrivateKey
	remoteRandomPub      *ecies.PublicKey
}

// 由接收方调用
func (h *encHandshake) handleAuthMsg(msg *authMsgV4, prv *ecdsa.PrivateKey) error {
	// 发起方的Nonce
	h.initNonce = msg.Nonce[:]
	// 发起方的ID
	h.remoteID = msg.InitiatorPubkey
	rpub, err := h.remoteID.Pubkey()
	if err != nil {
		return fmt.Errorf("bad remoteID: %#v", err)
	}
	// 发起方的加密公钥
	h.remotePub = ecies.ImportECDSAPublic(rpub)

	// 产生随机密钥
	if h.randomPrivKey == nil {
		h.randomPrivKey, err = ecies.GenerateKey(rand.Reader, crypto.S256(), nil)
		if err != nil {
			return err
		}
	}

	// 生成共享密钥
	token, err := h.staticSharedSecret(prv)
	if err != nil {
		return err
	}
	signedMsg := xor(token, h.initNonce)

	// 根据 signedMsg + msg => remote random pub
	remoteRandomPub, err := secp256k1.RecoverPubkey(signedMsg, msg.Signature[:])
	if err != nil {
		return err
	}
	h.remoteRandomPub, _ = importPublicKey(remoteRandomPub)
	return nil
}

func (h *encHandshake) handleAuthResp(msg *authRespV4) (err error) {
	h.respNonce = msg.Nonce[:]
	h.remoteRandomPub, err = importPublicKey(msg.RandomPubkey[:])
	return err
}

func (h *encHandshake) makeAuthMsg(prv *ecdsa.PrivateKey, token []byte) (*authMsgV4, error) {
	// 远程节点的NodeID => 签名公钥
	rpub, err := h.remoteID.Pubkey()
	if err != nil {
		return nil, fmt.Errorf("bad remoteID: %v", err)
	}

	// 远程节点的签名公钥 => 加密公钥
	h.remotePub = ecies.ImportECDSAPublic(rpub)
	// 获取随机数
	h.initNonce = make([]byte, shaLen)
	if _, err := rand.Read(h.initNonce); err != nil {
		return nil, err
	}

	// 随机数 => 本地节点的随机加密私钥
	h.randomPrivKey, err = ecies.GenerateKey(rand.Reader, crypto.S256(), nil)
	if err != nil {
		return nil, err
	}

	// 本地节点的加密私钥 + 远程节点的加密公钥 => token
	token, err = h.staticSharedSecret(prv)
	if err != nil {
		return nil, err
	}
	signed := xor(token, h.initNonce)

	// 随机签名私钥 => token的签名
	signature, err := crypto.Sign(signed, h.randomPrivKey.ExportECDSA())
	if err != nil {
		return nil, err
	}

	// authMsgV4 = token的签名 + 本地节点的签名公钥 + 随机数 + 版本号
	msg := new(authMsgV4)
	copy(msg.Signature[:], signature)
	copy(msg.InitiatorPubkey[:], crypto.FromECDSAPub(&prv.PublicKey)[1:])
	copy(msg.Nonce[:], h.initNonce)
	msg.Version = 4
	return msg, nil
}

func (h *encHandshake) staticSharedSecret(prv *ecdsa.PrivateKey) ([]byte, error) {
	return ecies.ImportECDSA(prv).GenerateShared(h.remotePub, sskLen, sskLen)
}

func (h *encHandshake) secrets(auth, authResp []byte) (secrets, error) {
	// 生成需要交换的共享密钥
	ecdheSecret, err := h.randomPrivKey.GenerateShared(h.remoteRandomPub, sskLen, sskLen)
	if err != nil {
		return secrets{}, err
	}

	sharedSecret := crypto.Keccak256(ecdheSecret, crypto.Keccak256(h.respNonce, h.initNonce))
	aesSecret := crypto.Keccak256(ecdheSecret, sharedSecret)
	s := secrets{
		RemoteID: h.remoteID,
		AES:      aesSecret,
		MAC:      crypto.Keccak256(ecdheSecret, aesSecret),
	}

	// 生成 Hash
	mac1 := sha3.NewKeccak256()
	mac1.Write(xor(s.MAC, h.respNonce))
	mac1.Write(auth)
	mac2 := sha3.NewKeccak256()
	mac2.Write(xor(s.MAC, h.initNonce))
	mac2.Write(authResp)
	if h.initiator {
		s.EgressMAC, s.IngressMAC = mac1, mac2
	} else {
		s.EgressMAC, s.IngressMAC = mac2, mac1
	}

	return s, nil
}

// 签名公钥 => 加密公钥
func importPublicKey(pubKey []byte) (*ecies.PublicKey, error) {
	var pubKey65 []byte
	switch len(pubKey) {
	case 64:
		pubKey65 = append([]byte{0x04}, pubKey...)
	case 65:
		pubKey65 = pubKey
	default:
		return nil, fmt.Errorf("invalid public key length %v(expect 64/65)", len(pubKey))
	}

	// []byte => ecdsa.PublicKey
	pub := crypto.ToECDSAPub(pubKey65)
	if pub.X == nil {
		return nil, fmt.Errorf("invalid public key")
	}

	// ecdsa.PublicKey => ecies.PublicKey
	return ecies.ImportECDSAPublic(pub), nil
}

type authMsgV4 struct {
	gotPlain        bool
	Signature       [sigLen]byte
	InitiatorPubkey [pubLen]byte
	Nonce           [shaLen]byte
	Version         uint
}

type authRespV4 struct {
	RandomPubkey [pubLen]byte
	Nonce        [shaLen]byte
	Version      uint
	Rest         []rlp.RawValue `rlp:"tail"`
}

func (msg *authMsgV4) decodePlain(input []byte) {
	// 提取signature
	n := copy(msg.Signature[:], input)
	// 跳过一个shaLenß
	n += shaLen
	// 提取public key
	n += copy(msg.InitiatorPubkey[:], input[n:])
	// 提取nonce
	copy(msg.Nonce[:], input[n:])
	msg.Version = 4
	msg.gotPlain = true
}

func (msg *authRespV4) decodePlain(input []byte) {
	// 提取public key
	n := copy(msg.RandomPubkey[:], input)
	// 提取nonce
	copy(msg.Nonce[:], input[n:])
	msg.Version = 4
}

func (t *rlpx) doEncHandshake(prv *ecdsa.PrivateKey, dial *discover.Node) (discover.NodeID, error) {
	var (
		sec secrets
		err error
	)

	if dial == nil {
		sec, err = receiverEncHandshake(t.fd, prv, nil)
	} else {
		sec, err = initiatorEncHandshake(t.fd, prv, dial.ID, nil)
	}
	if err != nil {
		return discover.NodeID{}, err
	}

	return sec.RemoteID, nil
}

func (t *rlpx) close(err error) {
	t.wmu.Lock()
	defer t.wmu.Unlock()

	t.fd.Close()
}

func newRLPX(fd net.Conn) transport {
	fd.SetDeadline(time.Now().Add(handshakeTimeout))
	return &rlpx{fd: fd}
}

func initiatorEncHandshake(conn io.ReadWriter, prv *ecdsa.PrivateKey, remoteID discover.NodeID, token []byte) (s secrets, err error) {
	// 构建 encHandshake
	h := &encHandshake{initiator: true, remoteID: remoteID}
	// 构建 authMsgV4
	authMsg, err := h.makeAuthMsg(prv, token)
	if err != nil {
		return s, err
	}

	// authMsgV4 => packet / []byte
	authPacket, err := sealEIP8(authMsg, h)
	if err != nil {
		return s, err
	}

	// 发送 packet
	if _, err = conn.Write(authPacket); err != nil {
		return s, err
	}

	// 读取 authRespMsg
	authRespMsg := new(authRespV4)
	authRespPacket, err := readHandshakeMsg(authRespMsg, encAuthRespLen, prv, conn)
	if err != nil {
		return s, err
	}

	if err := h.handleAuthResp(authRespMsg); err != nil {
		return s, err
	}

	return h.secrets(authPacket, authRespPacket)
}

func receiverEncHandshake(conn io.ReadWriter, prv *ecdsa.PrivateKey, token []byte) (s secrets, err error) {
	authMsg := new(authMsgV4)
	authPacket, err := readHandshakeMsg(authMsg, encAuthMsgLen, prv, conn)
	if err != nil {
		return s, err
	}

	h := new(encHandshake)
	if err := h.handleAuthMsg(authMsg, prv); err != nil {
		return s, err
	}

	authRespMsg, err := h.makeAuthResp()
	if err != nil {
		return s, err
	}
}

func xor(one, other []byte) (xor []byte) {
	xor = make([]byte, len(one))
	for i := 0; i < len(one); i++ {
		xor[i] = one[i] ^ other[i]
	}
	return xor
}

var padSpace = make([]byte, 300)

func sealEIP8(msg interface{}, h *encHandshake) ([]byte, error) {
	buf := new(bytes.Buffer)
	// 写入 msg
	if err := rlp.Encode(buf, msg); err != nil {
		return nil, err
	}

	// 写入填充字节
	pad := padSpace[:mrand.Intn(len(padSpace)-100)+100]
	buf.Write(pad)
	// prefix
	prefix := make([]byte, 2)
	binary.BigEndian.PutUint64(prefix, uint64(buf.Len()+eciesOverhead))

	// 加密
	enc, err := ecies.Encrypt(rand.Reader, h.remotePub, buf.Bytes(), nil, prefix)
	return append(prefix, enc...), err
}

type plainDecoder interface {
	decodePlain([]byte)
}

func readHandshakeMsg(msg plainDecoder, plainSize int, prv *ecdsa.PrivateKey, r io.Reader) ([]byte, error) {
	// 读取消息的前半部分
	buf := make([]byte, plainSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return buf, err
	}

	// 获取本地节点的加密私钥
	key := ecies.ImportECDSA(prv)
	// 解密私钥
	if dec, err := key.Decrypt(buf, nil, nil); err == nil {
		msg.decodePlain(dec)
		return buf, nil
	}

	// 处理前两个字节 ==> size
	prefix := buf[:2]
	size := binary.BigEndian.Uint16(prefix)
	if size < uint16(plainSize) {
		return buf, fmt.Errorf("size underflow, need at least %d bytes", plainSize)
	}

	// 读取消息的后半部分
	buf = append(buf, make([]byte, size-uint16(plainSize)+2)...)
	if _, err := io.ReadFull(r, buf[plainSize:]); err != nil {
		return buf, err
	}
	// 解密消息
	dec, err := key.Decrypt(buf[2:], nil, prefix)
	if err != nil {
		return buf, err
	}

	// 解码消息
	s := rlp.NewStream(bytes.NewReader(dec), 0)
	return buf, s.Decode(msg)
}
