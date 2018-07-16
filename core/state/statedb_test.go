package state

import (
	"math/big"
	"testing"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/ethdb"
)

func TestUpdateLeaks(t *testing.T) {
	db := ethdb.NewMemDatabase()
	statedb, _ := New(common.Hash{}, NewDatabase(db))

	for i := byte(0); i < 255; i++ {
		addr := common.BytesToAddress([]byte{i})
		// 设置 Balance
		statedb.AddBalance(addr, big.NewInt(int64(11*i)))
		// 设置 Nonce
		statedb.SetNonce(addr, uint64(42*i))
		if i%2 == 0 {
			// 设置 Storage
			statedb.SetState(addr, common.BytesToHash([]byte{i, i, i}), common.BytesToHash([]byte{i, i, i, i}))
		}
		if i%3 == 0 {
			// 设置 Code
			statedb.SetCode(addr, []byte{i, i, i, i, i})
		}
		statedb.IntermediateRoot(false)
	}

	for _, key := range db.Keys() {
		value, _ := db.Get(key)
		t.Errorf("State leaked into database: %x => %x ", key, value)
	}
}

func TestIntermediateLeaks(t *testing.T) {

	transDb := ethdb.NewMemDatabase()
	finalDb := ethdb.NewMemDatabase()
	transState, _ := New(common.Hash{}, NewDatabase(transDb))
	finalState, _ := New(common.Hash{}, NewDatabase(finalDb))

	// 修改一个账号包含的字段：balance, nonce, code, storage
	modify := func(state *StateDB, addr common.Address, i, tweak byte) {
		state.SetBalance(addr, big.NewInt(int64(11*i)+int64(tweak)))
		state.SetNonce(addr, uint64(42*i+tweak))
		if i%2 == 0 {
			state.SetState(addr, common.Hash{i, i, i, 0}, common.Hash{})
			state.SetState(addr, common.Hash{i, i, i, tweak}, common.Hash{i, i, i, i, tweak})
		}
		if i%3 == 0 {
			state.SetCode(addr, []byte{i, i, i, i, i, tweak})
		}
	}

	// 修改 255 个账号的字段
	for i := byte(0); i < 255; i++ {
		modify(transState, common.Address{byte(i)}, i, 0)
	}

	// 刷新 Trie & Trie Hash
	transState.IntermediateRoot(false)

	for i := byte(0); i < 255; i++ {
		modify(transState, common.Address{byte(i)}, i, 99)
		modify(finalState, common.Address{byte(i)}, i, 99)
	}

	if _, err := transState.Commit(false); err != nil {
		t.Fatalf("failed to commit transition state: %v", err)
	}

}
