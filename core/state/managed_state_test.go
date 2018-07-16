package state

import (
	"fmt"
	"testing"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/ethdb"
)

var addr = common.BytesToAddress([]byte("test"))

func create() (*ManagedState, *account) {
	statedb, _ := New(common.Hash{}, NewDatabase(ethdb.NewMemDatabase()))
	ms := ManageState(statedb)
	ms.StateDB.SetNonce(addr, 100)
	ms.accounts[addr] = newAccount(ms.StateDB.getStateObject(addr))
	return ms, ms.accounts[addr]
}

func TestNewNonce(t *testing.T) {
	ms, _ := create()

	nonce := ms.NewNonce(addr)
	if nonce != 100 {
		t.Error("expected nonce 100. got", nonce)
	}

	nonce = ms.NewNonce(addr)
	if nonce != 101 {
		t.Error("expected nonce 101. got", nonce)
	}
}

func TestRemove(t *testing.T) {
	ms, account := create()

	nn := make([]bool, 10)
	for i := range nn {
		nn[i] = true
	}
	account.nonces = append(account.nonces, nn...)

	i := uint64(5)
	ms.RemoveNonce(addr, account.nstart+i)
	if len(account.nonces) != 5 {
		t.Error("expected", i, "'th index to be false")
	}
}

func TestReuse(t *testing.T) {
	ms, account := create()

	nn := make([]bool, 10)
	for i := range nn {
		nn[i] = true
	}
	account.nonces = append(account.nonces, nn...)

	i := uint64(5)
	ms.RemoveNonce(addr, account.nstart+i)
	nonce := ms.NewNonce(addr)
	if nonce != 105 {
		t.Error("expected nonce to be 105. got", nonce)
	}
}

func TestRemoteNonceChange(t *testing.T) {
	ms, account := create()
	nn := make([]bool, 10)
	for i := range nn {
		nn[i] = true
	}
	account.nonces = append(account.nonces, nn...)
	nc := ms.NewNonce(addr)
	fmt.Printf("nonce = %v \n", nc)

	ms.StateDB.stateObjects[addr].data.Nonce = 200
	fmt.Printf("set nonce in stateDB ==> %v \n", 200)
	nonce := ms.NewNonce(addr)
	if nonce != 200 {
		t.Error("expected nonce after remote update to be 200, got %v", nonce)
	}
	ms.NewNonce(addr)
	ms.NewNonce(addr)
	nc = ms.NewNonce(addr)
	fmt.Printf("nonce = %v \n", nc)

	ms.StateDB.stateObjects[addr].data.Nonce = 200
	fmt.Printf("set nonce in stateDB ==> %v \n", 200)
	nonce = ms.NewNonce(addr)
	if nonce != 204 {
		t.Error("expected nonce after remote update to be", 204, "got", nonce)
	}
	fmt.Printf("nonce = %v \n", nonce)
}
