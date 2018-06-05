package discover

import (
	"crypto/ecdsa"

	"github.com/czh0526/blockchain/crypto"
)

func newkey() *ecdsa.PrivateKey {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic("could't generate key: " + err.Error())
	}
	return key
}
