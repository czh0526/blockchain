package trie

import (
	"sync"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/ethdb"
	"github.com/czh0526/blockchain/log"
)

var secureKeyPrefix = []byte("secure-key-")

const secureKeyLength = 11 + 32

type DatabaseReader interface {
	Get(key []byte) (value []byte, err error)
	Has(key []byte) (bool, error)
}

type Database struct {
	diskdb ethdb.Database

	nodes  map[common.Hash]*cachedNode
	oldest common.Hash
	newest common.Hash

	preimages map[common.Hash][]byte
	seckeybuf [secureKeyLength]byte

	nodesSize     common.StorageSize
	preimagesSize common.StorageSize

	lock sync.RWMutex
}

type cachedNode struct {
	blob      []byte
	parents   int
	children  map[common.Hash]int
	flushPrev common.Hash
	flushNext common.Hash
}

func NewDatabase(diskdb ethdb.Database) *Database {
	return &Database{
		diskdb: diskdb,
		nodes: map[common.Hash]*cachedNode{
			{}: {children: make(map[common.Hash]int)},
		},
		preimages: make(map[common.Hash][]byte),
	}
}

func (db *Database) Node(hash common.Hash) ([]byte, error) {
	db.lock.RLock()
	node := db.nodes[hash]
	defer db.lock.RUnlock()

	if node != nil {
		return node.blob, nil
	}

	return db.diskdb.Get(hash[:])
}

func (db *Database) Insert(hash common.Hash, blob []byte) {
	db.lock.Lock()
	defer db.lock.Unlock()

	db.insert(hash, blob)
}

func (db *Database) insert(hash common.Hash, blob []byte) {
	// 检查之前是否已经插入
	if _, ok := db.nodes[hash]; ok {
		return
	}

	// 构造 cachedNode 对象，放入缓存
	db.nodes[hash] = &cachedNode{
		blob:      common.CopyBytes(blob),
		children:  make(map[common.Hash]int),
		flushPrev: db.newest,
	}
	if db.oldest == (common.Hash{}) {
		db.oldest, db.newest = hash, hash
	} else {
		db.nodes[db.newest].flushNext, db.newest = hash, hash
	}

	// 累加存储计数器 ==> key + value
	db.nodesSize += common.StorageSize(common.HashLength + len(blob))
}

func (db *Database) Reference(child common.Hash, parent common.Hash) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	db.reference(child, parent)
}

func (db *Database) reference(child common.Hash, parent common.Hash) {
	// child node 不存在
	node, ok := db.nodes[child]
	if !ok {
		return
	}

	// child <==> parent 的关系已经存在，不再计数
	if _, ok = db.nodes[parent].children[child]; ok && parent != (common.Hash{}) {
		return
	}

	// 增加双向的关联关系
	node.parents++
	db.nodes[parent].children[child]++
}

func (db *Database) Commit(node common.Hash, report bool) error {
	db.lock.RLock()

	batch := db.diskdb.NewBatch()

	// 提交 sec-hash, key
	for hash, preimage := range db.preimages {
		if err := batch.Put(db.secureKey(hash[:]), preimage); err != nil {
			log.Error("Failed to commit preimage from trie database", "err", err)
			db.lock.RUnlock()
			return err
		}

		if batch.ValueSize() > ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}

	// 提交 node
	//nodes, storage := len(db.nodes), db.nodesSize
	if err := db.commit(node, batch); err != nil {
		log.Error("Failed to commit trie from trie database", "err", err)
		db.lock.RUnlock()
		return err
	}

	// 刷新缓存
	if err := batch.Write(); err != nil {
		log.Error("Failed to write trie to disk", "err", err)
		db.lock.RUnlock()
		return err
	}
	db.lock.RUnlock()

	db.lock.Lock()
	defer db.lock.Unlock()

	db.preimages = make(map[common.Hash][]byte)
	db.preimagesSize = 0

	db.uncache(node)

	return nil
}

func (db *Database) commit(hash common.Hash, batch ethdb.Batch) error {
	// 如果节点不存在，返回
	node, ok := db.nodes[hash]
	if !ok {
		return nil
	}

	// 递归提交全部的 children
	for child := range node.children {
		if err := db.commit(child, batch); err != nil {
			return err
		}
	}

	// 退出递归的条件：将 node 数据写入 batch
	if err := batch.Put(hash[:], node.blob); err != nil {
		return err
	}

	// 如果缓存数据大于100K, 写操作
	if batch.ValueSize() >= ethdb.IdealBatchSize {
		if err := batch.Write(); err != nil {
			return err
		}
		batch.Reset()
	}
	return nil
}

func (db *Database) uncache(hash common.Hash) {

	node, ok := db.nodes[hash]
	if !ok {
		return
	}

	if hash == db.oldest {
		// 链表的头部处理
		db.oldest = node.flushNext
	} else {
		db.nodes[node.flushPrev].flushNext = node.flushNext
		db.nodes[node.flushNext].flushPrev = node.flushPrev
	}

	for child := range node.children {
		db.uncache(child)
	}
	delete(db.nodes, hash)
	db.nodesSize -= common.StorageSize(common.HashLength + len(node.blob))
}

func (db *Database) secureKey(key []byte) []byte {
	buf := append(db.seckeybuf[:0], secureKeyPrefix...)
	buf = append(buf, key...)
	return buf
}

func (db *Database) insertPreimage(hash common.Hash, preimage []byte) {
	if _, ok := db.preimages[hash]; ok {
		return
	}
	db.preimages[hash] = common.CopyBytes(preimage)
	db.preimagesSize += common.StorageSize(common.HashLength + len(preimage))
}

func (db *Database) preimage(hash common.Hash) ([]byte, error) {
	db.lock.RLock()
	preimage := db.preimages[hash]
	db.lock.RUnlock()

	if preimage != nil {
		return preimage, nil
	}

	return db.diskdb.Get(db.secureKey(hash[:]))
}

func (db *Database) DiskDB() DatabaseReader {
	return db.diskdb
}

func (db *Database) Nodes() []common.Hash {
	db.lock.RLock()
	defer db.lock.RUnlock()

	var hashes = make([]common.Hash, 0, len(db.nodes))
	for hash := range db.nodes {
		if hash != (common.Hash{}) {
			hashes = append(hashes, hash)
		}
	}
	return hashes
}
