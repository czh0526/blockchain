package ecies

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"

	eth_ecies "github.com/ethereum/go-ethereum/crypto/ecies"
)

func TestEncryptDecrypt(t *testing.T) {
	prv1, err := eth_ecies.GenerateKey(rand.Reader, eth_ecies.DefaultCurve, nil)
	if err != nil {
		fmt.Println(err.Error())
		t.FailNow()
	}

	prv2, err := eth_ecies.GenerateKey(rand.Reader, eth_ecies.DefaultCurve, nil)
	if err != nil {
		fmt.Println(err.Error())
		t.FailNow()
	}

	message := []byte("Hello, world.")
	ct, err := eth_ecies.Encrypt(rand.Reader, &prv2.PublicKey, message, nil, nil)
	if err != nil {
		fmt.Println(err.Error())
		t.FailNow()
	}

	pt, err := prv2.Decrypt(rand.Reader, ct, nil, nil)
	if err != nil {
		fmt.Println(err.Error())
		t.FailNow()
	}

	if !bytes.Equal(pt, message) {
		fmt.Println("ecies: plaintext doesn't match message")
		t.FailNow()
	}

	_, err = prv1.Decrypt(rand.Reader, ct, nil, nil)
	if err == nil {
		fmt.Println("ecies: encryption should not have succeeded")
		t.FailNow()
	}
}
