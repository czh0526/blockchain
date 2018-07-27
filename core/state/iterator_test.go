package state

import (
	"bytes"
	"testing"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/ethdb"
)

func TestNodeIteratorCoverage(t *testing.T) {
	// 构建一个带数据的 StateDB
	db, root, _ := makeTestState()
	state, err := New(root, db)
	if err != nil {
		t.Fatalf("failed to create state trie at %x: %v", root, err)
	}

	// 构建 StateDB 的 Iterator, 遍历全部的 Key
	hashes := make(map[common.Hash]struct{})
	for it := NewNodeIterator(state); it.Next(); {
		if it.Hash != (common.Hash{}) {
			hashes[it.Hash] = struct{}{}
		}
	}

	// 检查 trie.Database 是否包含了 hashes
	for hash := range hashes {
		if _, err := db.TrieDB().Node(hash); err != nil {
			t.Errorf("failed to retrieve reported node %x", hash)
		}
	}

	// 检查 hashes 是否包含了 trie.Database
	for _, hash := range db.TrieDB().Nodes() {
		if _, ok := hashes[hash]; !ok {
			t.Errorf("state entry not reported %x", hash)
		}
	}

	db.TrieDB().Commit(state.trie.Hash(), false)
	for _, key := range db.TrieDB().DiskDB().(*ethdb.MemDatabase).Keys() {
		if bytes.HasPrefix(key, []byte("secure-key-")) {
			continue
		}
		if _, ok := hashes[common.BytesToHash(key)]; !ok {
			t.Errorf("state entry not reported %x", key)
		}
	}
}
