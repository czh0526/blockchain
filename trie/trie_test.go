package trie

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/ethdb"
)

func newEmpty() *Trie {
	trie, _ := New(common.Hash{}, NewDatabase(ethdb.NewMemDatabase()))
	return trie
}

func TestEmptyTrie(t *testing.T) {
	var trie Trie
	res := trie.Hash()
	exp := emptyRoot
	if res != exp {
		t.Errorf("expected %x got %x", exp, res)
	}
	fmt.Printf("empty trie hash = %x \n", res.Bytes())
}

func TestNull(t *testing.T) {
	var trie Trie
	key := make([]byte, 32)
	value := []byte("test")
	trie.Update(key, value)
	if !bytes.Equal(trie.Get(key), value) {
		t.Fatal("wrong value")
	}
}

func TestMissingRoot(t *testing.T) {
	trie, err := New(common.HexToHash("0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33"), NewDatabase(ethdb.NewMemDatabase()))
	if trie != nil {
		t.Error("New returned non-nil trie for invalid root")
	}
	if _, ok := err.(*MissingNodeError); !ok {
		t.Errorf("New returned wrong error: %v", err)
	}
}

func TestMissingNodeDisk(t *testing.T) {
	testMissingNode(t, false)
}

func TestMissingNodeMemonly(t *testing.T) {
	testMissingNode(t, true)
}

func testMissingNode(t *testing.T, memonly bool) {
	diskdb := ethdb.NewMemDatabase()
	triedb := NewDatabase(diskdb)

	trie, _ := New(common.Hash{}, triedb)
	updateString(trie, "120", "test1")
	trie.tryGet(trie.root, keybytesToHex([]byte("120")), 0)
	updateString(trie, "120000", "qwerqwerqwerqwer")
	trie.tryGet(trie.root, keybytesToHex([]byte("120000")), 0)
	updateString(trie, "120099", "test99")
	trie.tryGet(trie.root, keybytesToHex([]byte("120099")), 0)
	updateString(trie, "123456", "asdfasdfasdfasdf")
	trie.tryGet(trie.root, keybytesToHex([]byte("123456")), 0)
	// trie 的写操作
	root, _ := trie.Commit(nil)
	if !memonly {
		// triedb 的写操作
		triedb.Commit(root, true)
	}

	trie, _ = New(root, triedb)
	val, err := trie.TryGet([]byte("120000"))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	fmt.Printf("120000 ==> %v \n", string(val))

	trie, _ = New(root, triedb)
	val, err = trie.TryGet([]byte("120099"))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	fmt.Printf("120099 ==> %v \n", string(val))

	trie, _ = New(root, triedb)
	val, err = trie.TryGet([]byte("123456"))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	fmt.Printf("123456 ==> %v \n", string(val))

	trie, _ = New(root, triedb)
	err = trie.TryUpdate([]byte("120099"), []byte("zxcvzxcvzxcv"))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

}

func updateString(trie *Trie, k, v string) {
	trie.Update([]byte(k), []byte(v))
}
