package discover

import (
	"crypto/ecdsa"
	"fmt"
	"testing"
	"time"

	"github.com/czh0526/blockchain/crypto"
)

func TestChannel(t *testing.T) {
	reply := make(chan []int, 3)
	routines := 0
	for {
		for i := 0; i < 10 && routines < 3; i++ {
			routines++
			fmt.Printf("routines = %d \n", routines)
			go func(base int) {
				reply <- []int{base * 3, base*3 + 1, base*3 + 2}
				fmt.Printf("%d) reply <- \n", base)
			}(i)
		}

		for _, i := range <-reply {
			fmt.Printf("%d \n", i)
		}
		routines--
	}
	<-time.After(time.Second * 5)
}

func TestChannel2(t *testing.T) {
	reply := make(chan int, 3)
	for _, i := range []int{0, 1, 2} {
		go func(v int) {
			//fmt.Printf("reply <- %d \n", v)
			reply <- v
		}(i)
	}

	for i := range reply {
		fmt.Println(i)
	}
}

func newkey() *ecdsa.PrivateKey {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic("could't generate key: " + err.Error())
	}
	return key
}
