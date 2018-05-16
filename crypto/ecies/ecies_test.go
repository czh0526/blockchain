package ecies

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"

	eth_ecies "github.com/ethereum/go-ethereum/crypto/ecies"
)

func TestEncryptDecrypt(t *testing.T) {
	// 生成密钥1
	prv1, err := eth_ecies.GenerateKey(rand.Reader, eth_ecies.DefaultCurve, nil)
	if err != nil {
		fmt.Println(err.Error())
		t.FailNow()
	}
	// 生成密钥2
	prv2, err := eth_ecies.GenerateKey(rand.Reader, eth_ecies.DefaultCurve, nil)
	if err != nil {
		fmt.Println(err.Error())
		t.FailNow()
	}

	message := []byte("Hello, world.")
	// 用公钥2加密文本
	ct, err := eth_ecies.Encrypt(rand.Reader, &prv2.PublicKey, message, nil, nil)
	if err != nil {
		fmt.Println(err.Error())
		t.FailNow()
	}

	// 用私钥2解密文本
	pt, err := prv2.Decrypt(ct, nil, nil)
	if err != nil {
		fmt.Println(err.Error())
		t.FailNow()
	}

	// 校验加、解密的结果
	if !bytes.Equal(pt, message) {
		fmt.Println("ecies: plaintext doesn't match message")
		t.FailNow()
	}

	_, err = prv1.Decrypt(ct, nil, nil)
	if err == nil {
		fmt.Println("ecies: encryption should not have succeeded")
		t.FailNow()
	}
}
