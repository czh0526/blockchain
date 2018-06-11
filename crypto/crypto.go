package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math/big"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/crypto/sha3"
)

var (
	secp256k1N, _  = new(big.Int).SetString("fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16)
	secp256k1halfN = new(big.Int).Div(secp256k1N, big.NewInt(2))
)

func GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(S256(), rand.Reader)
}

/**
(数字签名算法) Digital Signature Algorithm
*/

// []byte ==> ecdsa.PublicKey
func ToECDSAPub(pub []byte) *ecdsa.PublicKey {
	if len(pub) == 0 {
		return nil
	}
	x, y := elliptic.Unmarshal(S256(), pub)
	return &ecdsa.PublicKey{Curve: S256(), X: x, Y: y}
}

// ecdsa.PublicKey ==> []byte
func FromECDSAPub(pub *ecdsa.PublicKey) []byte {
	if pub == nil || pub.X == nil || pub.Y == nil {
		return nil
	}
	return elliptic.Marshal(S256(), pub.X, pub.Y)
}

func Keccak256(data ...[]byte) []byte {
	d := sha3.NewKeccak256()
	for _, b := range data {
		d.Write(b)
	}
	return d.Sum(nil)
}

func Keccak256Hash(data ...[]byte) (h common.Hash) {
	d := sha3.NewKeccak256()
	for _, b := range data {
		d.Write(b)
	}
	d.Sum(h[:0])
	return h
}

func zeroBytes(bytes []byte) {
	for i := range bytes {
		bytes[i] = 0
	}
}
