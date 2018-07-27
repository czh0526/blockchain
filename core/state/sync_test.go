package state

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/czh0526/blockchain/trie"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/crypto"
	"github.com/czh0526/blockchain/ethdb"
)

type testAccount struct {
	address common.Address
	balance *big.Int
	nonce   uint64
	code    []byte
}

func makeTestState() (Database, common.Hash, []*testAccount) {

	// 构建一个 DiskDB
	diskdb := ethdb.NewMemDatabase()
	// 构建一个 state caching DB
	db := NewDatabase(diskdb)
	// 构建一个新的 StateDB 对象
	state, _ := New(common.Hash{}, db)

	// 在 StateDB 中生成 96 个 stateObject 对象
	accounts := []*testAccount{}
	for i := byte(0); i < 96; i++ {
		obj := state.GetOrNewStateObject(common.BytesToAddress([]byte{i}))
		acc := &testAccount{address: common.BytesToAddress([]byte{i})}

		obj.AddBalance(big.NewInt(int64(11 * i)))
		acc.balance = big.NewInt(int64(11 * i))

		obj.SetNonce(uint64(42 * i))
		acc.nonce = uint64(42 * i)

		if i%3 == 0 {
			obj.SetCode(crypto.Keccak256Hash([]byte{i, i, i, i, i}), []byte{i, i, i, i, i})
			acc.code = []byte{i, i, i, i, i}
		}
		state.updateStateObject(obj)
		accounts = append(accounts, acc)
	}
	root, _ := state.Commit(false)

	return db, root, accounts
}

func checkStateAccounts(t *testing.T, dstDb ethdb.Database, srcRoot common.Hash, srcAccounts []*testAccount) {

	// 使用 dstDb 中的数据，构建根是 srcRoot 的 StateDB
	state, err := New(srcRoot, NewDatabase(dstDb))
	if err != nil {
		t.Fatalf("failed to create state trie at %x: %v", srcRoot, err)
	}
	if err := checkStateConsistency(dstDb, srcRoot); err != nil {
		t.Fatalf("inconsistent state trie at %x: %v", srcRoot, err)
	}

	for i, acc := range srcAccounts {
		if balance := state.GetBalance(acc.address); balance.Cmp(acc.balance) != 0 {
			t.Errorf("account %d: balance mismatch: have %v, want %v", i, balance, acc.balance)
		}
		if nonce := state.GetNonce(acc.address); nonce != acc.nonce {
			t.Errorf("account %d: nonce mismatch: have %v, want %v", nonce, acc.nonce)
		}
		if code := state.GetCode(acc.address); !bytes.Equal(code, acc.code) {
			t.Errorf("account %d: code mismatch: have %x, want %x", i, code, acc.code)
		}
	}
}

func checkTrieConsistency(db ethdb.Database, root common.Hash) error {
	if v, _ := db.Get(root[:]); v == nil {
		return nil
	}
	trie, err := trie.New(root, trie.NewDatabase(db))
	if err != nil {
		return err
	}

	it := trie.NodeIterator(nil)
	for it.Next(true) {
	}
	return it.Error()
}

func checkStateConsistency(db ethdb.Database, root common.Hash) error {

	if _, err := db.Get(root.Bytes()); err != nil {
		return nil
	}

	state, err := New(root, NewDatabase(db))
	if err != nil {
		return err
	}

	it := NewNodeIterator(state)
	for it.Next() {
	}
	return it.Error
}

func TestEmptyStateSync(t *testing.T) {
	// trie.emptyRoot = "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
	empty := common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	if req := NewStateSync(empty, ethdb.NewMemDatabase()).Missing(1); len(req) != 0 {
		t.Errorf("content requested for empty state: %v", req)
	}
}

func TestIterativeStateSyncIndividual(t *testing.T) {
	testIterativeStateSync(t, 1)
}

func TestIterativeStateSyncBatched(t *testing.T) {
	testIterativeStateSync(t, 100)
}

func testIterativeStateSync(t *testing.T, batch int) {

	srcDb, srcRoot, srcAccounts := makeTestState()

	dstDb := ethdb.NewMemDatabase()
	sched := NewStateSync(srcRoot, dstDb)

	queue := append([]common.Hash{}, sched.Missing(batch)...)
	for len(queue) > 0 {
		results := make([]trie.SyncResult, len(queue))
		for i, hash := range queue {
			data, err := srcDb.TrieDB().Node(hash)
			if err != nil {
				t.Fatalf("failed to retrieve node data for %x", hash)
			}
			results[i] = trie.SyncResult{Hash: hash, Data: data}
		}
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(dstDb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		queue = append(queue[:0], sched.Missing(batch)...)
	}
	checkStateAccounts(t, dstDb, srcRoot, srcAccounts)
}
