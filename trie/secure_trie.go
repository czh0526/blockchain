package trie

import (
	"github.com/czh0526/blockchain/common"
)

type SecureTrie struct {
	trie             Trie
	hashKeyBuf       [common.HashLength]byte
	secKeyCache      map[string][]byte
	secKeyCacheOwner *SecureTrie
}

func NewSecure(root common.Hash, db *Database, cachelimit uint16) (*SecureTrie, error) {
	if db == nil {
		panic("trie.NewSecure called without a database")
	}
	trie, err := New(root, db)
	if err != nil {
		return nil, err
	}
	trie.SetCacheLimit(cachelimit)
	return &SecureTrie{trie: *trie}, nil
}

func (t *SecureTrie) GetKey(shaKey []byte) []byte {
	if key, ok := t.getSecKeyCache()[string(shaKey)]; ok {
		return key
	}
	key, _ := t.trie.db.preimage(common.BytesToHash(shaKey))
	return key
}

func (t *SecureTrie) TryGet(key []byte) ([]byte, error) {
	return t.trie.TryGet(t.hashKey(key))
}

func (t *SecureTrie) TryUpdate(key, value []byte) error {
	hk := t.hashKey(key)
	err := t.trie.TryUpdate(hk, value)
	if err != nil {
		return err
	}
	t.getSecKeyCache()[string(hk)] = common.CopyBytes(key)
	return nil
}

func (t *SecureTrie) TryDelete(key []byte) error {
	hk := t.hashKey(key)
	delete(t.getSecKeyCache(), string(hk))
	return t.trie.TryDelete(hk)
}

func (t *SecureTrie) Hash() common.Hash {
	return t.trie.Hash()
}

func (t *SecureTrie) Copy() *SecureTrie {
	cpy := *t
	return &cpy
}

func (t *SecureTrie) Commit(onleaf LeafCallback) (root common.Hash, err error) {
	// 更新缓存
	if len(t.getSecKeyCache()) > 0 {
		t.trie.db.lock.Lock()
		for hk, key := range t.secKeyCache {
			t.trie.db.insertPreimage(common.BytesToHash([]byte(hk)), key)
		}
		t.trie.db.lock.Unlock()

		t.secKeyCache = make(map[string][]byte)
	}
	// 更新 trie
	return t.trie.Commit(onleaf)
}

func (t *SecureTrie) hashKey(key []byte) []byte {
	h := newHasher(0, 0, nil)
	h.sha.Reset()
	h.sha.Write(key)
	buf := h.sha.Sum(t.hashKeyBuf[:0])
	returnHasherToPool(h)
	return buf
}

func (t *SecureTrie) getSecKeyCache() map[string][]byte {
	if t != t.secKeyCacheOwner {
		t.secKeyCacheOwner = t
		t.secKeyCache = make(map[string][]byte)
	}
	return t.secKeyCache
}

func (t *SecureTrie) NodeIterator(start []byte) NodeIterator {
	return t.trie.NodeIterator(start)
}
