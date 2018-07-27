package trie

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/ethdb"
)

func makeTestTrie() (*Database, *Trie, map[string][]byte) {
	triedb := NewDatabase(ethdb.NewMemDatabase())
	trie, _ := New(common.Hash{}, triedb)

	content := make(map[string][]byte)
	for i := byte(0); i < 255; i++ {
		key, val := common.LeftPadBytes([]byte{1, i}, 32), []byte{i}
		content[string(key)] = val
		trie.Update(key, val)

		key, val = common.LeftPadBytes([]byte{2, i}, 32), []byte{i}
		content[string(key)] = val
		trie.Update(key, val)

		for j := byte(3); j < 13; j++ {
			key, val = common.LeftPadBytes([]byte{j, i}, 32), []byte{j, i}
			content[string(key)] = val
			trie.Update(key, val)
		}
	}
	trie.Commit(nil)

	return triedb, trie, content
}

func checkTrieContents(t *testing.T, db *Database, root []byte, content map[string][]byte) {

	trie, err := New(common.BytesToHash(root), db)
	if err != nil {
		t.Fatalf("failed to create trie at %x: %v", root, err)
	}
	if err := checkTrieConsistency(db, common.BytesToHash(root)); err != nil {
		t.Fatalf("inconsistent trie at %x: %v", root, err)
	}

	for key, val := range content {
		if have := trie.Get([]byte(key)); !bytes.Equal(have, val) {
			t.Errorf("entry %x: content mismatch: have %x, want %x", key, have, val)
		}
	}
}

func checkTrieConsistency(db *Database, root common.Hash) error {
	trie, err := New(root, db)
	if err != nil {
		return nil
	}
	it := trie.NodeIterator(nil)
	for it.Next(true) {
	}
	return it.Error()
}

func TestEmptySync(t *testing.T) {
	dbA := NewDatabase(ethdb.NewMemDatabase())
	dbB := NewDatabase(ethdb.NewMemDatabase())
	emptyA, _ := New(common.Hash{}, dbA)
	emptyB, _ := New(emptyRoot, dbB)

	for i, trie := range []*Trie{emptyA, emptyB} {
		if req := NewSync(trie.Hash(), trie.db.DiskDB(), nil).Missing(1); len(req) != 0 {
			t.Errorf("test %d: content requested for mpty trie: %v", i, req)
		}
	}
}

func TestIterativeSyncIndividual(t *testing.T) {
	testIterativeSync(t, 1)
}

func TestIterativeSyncBatched(t *testing.T) {
	testIterativeSync(t, 100)
}

func testIterativeSync(t *testing.T, batch int) {

	// 构造同步操作的 Trie源
	srcDb, srcTrie, srcData := makeTestTrie()

	// 构造同步操作的 Trie目标
	diskdb := ethdb.NewMemDatabase()
	triedb := NewDatabase(diskdb)

	// 构造同步操作对象 trie.Sync
	sched := NewSync(srcTrie.Hash(), diskdb, nil)

	queue := append([]common.Hash{}, sched.Missing(batch)...)
	for len(queue) > 0 {
		fmt.Println()
		fmt.Printf("           request size = %v, queue size = %v \n", len(sched.requests), len(queue))
		// 根据 queue, 构建 results， 模拟同步完成
		results := make([]SyncResult, len(queue))
		for i, hash := range queue {
			data, err := srcDb.Node(hash)
			if err != nil {
				t.Fatalf("failed to retrieve node data for %x: %v", hash, err)
			}
			results[i] = SyncResult{hash, data}
		}
		// 处理同步结果，设置 request 内容
		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%: %v", index, err)
		}
		fmt.Printf("[Process]: request size = %v, results size = %v \n", len(sched.requests), len(results))

		if index, err := sched.Commit(diskdb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		fmt.Printf("[Commit]:  sched.queue size = %v \n", sched.queue.Size())

		queue = append(queue[:0], sched.Missing(batch)...)
		fmt.Printf("[Missing]: sched.queue size = %v, queue = %v, requests = %v \n",
			sched.queue.Size(), len(queue), len(sched.requests))
	}

	checkTrieContents(t, triedb, srcTrie.Root(), srcData)
}

func TestIncompleteSync(t *testing.T) {

	srcDb, srcTrie, _ := makeTestTrie()

	diskdb := ethdb.NewMemDatabase()
	triedb := NewDatabase(diskdb)
	sched := NewSync(srcTrie.Hash(), diskdb, nil)

	added := []common.Hash{}
	queue := append([]common.Hash{}, sched.Missing(1)...)
	for len(queue) > 0 {
		// 收集队列中需要处理的 Result
		results := make([]SyncResult, len(queue))
		for i, hash := range queue {
			data, err := srcDb.Node(hash)
			if err != nil {
				t.Fatalf("failed to retrieve node data for %x: %v", hash, err)
			}
			results[i] = SyncResult{hash, data}
		}

		if _, index, err := sched.Process(results); err != nil {
			t.Fatalf("failed to process result #%d: %v", index, err)
		}
		if index, err := sched.Commit(diskdb); err != nil {
			t.Fatalf("failed to commit data #%d: %v", index, err)
		}
		for _, result := range results {
			added = append(added, result.Hash)
		}

		for _, root := range added {
			if err := checkTrieConsistency(triedb, root); err != nil {
				t.Fatalf("trie inconsistent: %v", err)
			}
		}
		queue = append(queue[:0], sched.Missing(1)...)
	}

	for _, node := range added[1:] {
		key := node.Bytes()
		value, _ := diskdb.Get(key)

		// 删除数据，进行一致性检查
		diskdb.Delete(key)
		if err := checkTrieConsistency(triedb, added[0]); err == nil {
			t.Fatalf("trie inconsistency not caught, missing: %x", key)
		}
		// 恢复数据
		diskdb.Put(key, value)
	}
}
