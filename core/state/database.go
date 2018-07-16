package state

import (
	"fmt"
	"sync"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/ethdb"
	"github.com/czh0526/blockchain/trie"
	lru "github.com/hashicorp/golang-lru"
)

var MaxTrieCacheGen = uint16(120)

const (
	maxPastTries      = 12
	codeSizeCacheSize = 100000
)

type Trie interface {
	TryGet(key []byte) ([]byte, error)
	TryUpdate(key, value []byte) error
	TryDelete(key []byte) error

	Commit(onleaf trie.LeafCallback) (common.Hash, error)

	Hash() common.Hash
}

type Database interface {
	OpenTrie(root common.Hash) (Trie, error)
	OpenStorageTrie(addrHash, root common.Hash) (Trie, error)
	CopyTrie(t Trie) Trie

	TrieDB() *trie.Database
}

func NewDatabase(db ethdb.Database) Database {
	csc, _ := lru.New(codeSizeCacheSize)
	return &cachingDB{
		db:            trie.NewDatabase(db),
		codeSizeCache: csc,
	}
}

type cachingDB struct {
	db            *trie.Database
	mu            sync.Mutex
	pastTries     []*trie.SecureTrie
	codeSizeCache *lru.Cache
}

func (db *cachingDB) TrieDB() *trie.Database {
	return db.db
}

func (db *cachingDB) OpenTrie(root common.Hash) (Trie, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	for i := len(db.pastTries) - 1; i >= 0; i-- {
		if db.pastTries[i].Hash() == root {
			return cachedTrie{db.pastTries[i].Copy(), db}, nil
		}
	}
	tr, err := trie.NewSecure(root, db.db, MaxTrieCacheGen)
	if err != nil {
		return nil, err
	}
	return cachedTrie{tr, db}, nil
}

func (db *cachingDB) OpenStorageTrie(addrHash, root common.Hash) (Trie, error) {
	return trie.NewSecure(root, db.db, 0)
}

func (db *cachingDB) CopyTrie(t Trie) Trie {
	switch t := t.(type) {
	case cachedTrie:
		return cachedTrie{t.SecureTrie.Copy(), db}
	case *trie.SecureTrie:
		return t.Copy()
	default:
		panic(fmt.Errorf("unknown trie type %T", t))
	}
}

type cachedTrie struct {
	*trie.SecureTrie
	db *cachingDB
}
