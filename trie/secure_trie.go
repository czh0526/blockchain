package trie

import (
	"errors"

	"github.com/czh0526/blockchain/common"
)

type SecureTrie struct {
	hashKeyBuf  [common.HashLength]byte
	secKeyCache map[string][]byte
}

func NewSecure(root common.Hash, db *Database, cachelimit uint16) (*SecureTrie, error) {
	if db == nil {
		panic("trie.NewSecure called without a database")
	}

	return &SecureTrie{}, nil
}

func (t *SecureTrie) TryGet(key []byte) ([]byte, error) {
	return nil, errors.New("[SecureTrie]: TryGet() has not be implemented.")
}

func (t *SecureTrie) TryUpdate(key, value []byte) error {
	return errors.New("[SecureTrie]: TryUpdate() has not be implemented.")
}

func (t *SecureTrie) TryDelete(key []byte) error {
	return errors.New("[SecureTrie]: TryDelete() has not be implemented.")
}

func (t *SecureTrie) Hash() common.Hash {
	return common.BytesToHash([]byte("secure trie hash"))
}

func (t *SecureTrie) Copy() *SecureTrie {
	cpy := *t
	return &cpy
}

func (t *SecureTrie) Commit(onleaf LeafCallback) (root common.Hash, err error) {
	return common.Hash{}, errors.New("[SecureTrie]: Commit() has not be implemented.")
}
