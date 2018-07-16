package state

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/crypto"
	"github.com/czh0526/blockchain/rlp"
)

var emptyCodeHash = crypto.Keccak256(nil)

type Code []byte
type Storage map[common.Hash]common.Hash

type Account struct {
	Nonce    uint64
	Balance  *big.Int
	Root     common.Hash
	CodeHash []byte
}

type stateObject struct {
	address       common.Address
	addrHash      common.Hash
	data          Account
	code          Code
	cachedStorage Storage
	dirtyStorage  Storage

	db   *StateDB
	trie Trie

	dbErr     error
	deleted   bool
	dirtyCode bool
	suicided  bool
}

func newObject(db *StateDB, address common.Address, data Account) *stateObject {
	if data.Balance == nil {
		data.Balance = new(big.Int)
	}
	if data.CodeHash == nil {
		data.CodeHash = emptyCodeHash
	}
	return &stateObject{
		db:            db,
		address:       address,
		addrHash:      crypto.Keccak256Hash(address[:]),
		data:          data,
		cachedStorage: make(Storage),
		dirtyStorage:  make(Storage),
	}
}

// 获取 Storage Trie
func (c *stateObject) getTrie(db Database) Trie {
	if c.trie == nil {
		var err error
		// 加载 account 的 Storage Trie
		c.trie, err = db.OpenStorageTrie(c.addrHash, c.data.Root)
		if err != nil {
			c.trie, _ = db.OpenStorageTrie(c.addrHash, common.Hash{})
			c.setError(fmt.Errorf("can't create storage trie: %v", err))
		}
	}
	return c.trie
}

// 更新 Storage Trie
func (self *stateObject) updateTrie(db Database) Trie {
	tr := self.getTrie(db)
	for key, value := range self.dirtyStorage {
		delete(self.dirtyStorage, key)
		if (value == common.Hash{}) {
			self.setError(tr.TryDelete(key[:]))
			continue
		}
		v, _ := rlp.EncodeToBytes(bytes.TrimLeft(value[:], "\x00"))
		self.setError(tr.TryUpdate(key[:], v))
	}
	return tr
}

// 更新 self.data.Root ==> Storage Trie Hash
func (self *stateObject) updateRoot(db Database) {
	self.updateTrie(db)
	self.data.Root = self.trie.Hash()
}

func (self *stateObject) CommitTrie(db Database) error {
	self.updateTrie(db)
	if self.dbErr != nil {
		return self.dbErr 
	}
	root, err := self.trie.Commit(nil)
	if err == nil {
		self.data.Root = root 
	}
	return err
}

func (self *stateObject) setError(err error) {
	if self.dbErr == nil {
		self.dbErr = err
	}
}

func (s *stateObject) empty() bool {
	// Nonce == 0
	// Balance == 0
	// CodeHash = nil
	return s.data.Nonce == 0 && s.data.Balance.Sign() == 0 && bytes.Equal(s.data.CodeHash, emptyCodeHash)
}

func (c *stateObject) touch() {
	c.db.journal.append(touchChange{
		account: &c.address,
	})
	if c.address == ripemd {
		c.db.journal.dirty(c.address)
	}
}

func (c *stateObject) Address() common.Address {
	return c.address
}

func (self *stateObject) Nonce() uint64 {
	return self.data.Nonce
}

func (self *stateObject) SetNonce(nonce uint64) {
	self.db.journal.append(nonceChange{
		account: &self.address,
		prev:    self.data.Nonce,
	})
	self.setNonce(nonce)
}

func (self *stateObject) setNonce(nonce uint64) {
	self.data.Nonce = nonce
}

func (self *stateObject) Balance() *big.Int {
	return self.data.Balance
}

func (c *stateObject) AddBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		if c.empty() {
			c.touch()
		}
		return
	}
	c.SetBalance(new(big.Int).Add(c.Balance(), amount))
}

func (c *stateObject) SubBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	c.SetBalance(new(big.Int).Sub(c.Balance(), amount))
}

func (self *stateObject) SetBalance(amount *big.Int) {
	self.db.journal.append(balanceChange{
		account: &self.address,
		prev:    new(big.Int).Set(self.data.Balance),
	})
	self.setBalance(amount)
}

func (self *stateObject) setBalance(amount *big.Int) {
	self.data.Balance = amount
}

func (self *stateObject) CodeHash() []byte {
	return self.data.CodeHash
}

func (self *stateObject) GetState(db Database, key common.Hash) common.Hash {
	// 缓存中加载数据
	value, exists := self.cachedStorage[key]
	if exists {
		return value
	}

	// 数据库中加载数据
	enc, err := self.getTrie(db).TryGet(key[:])
	if err != nil {
		self.setError(err)
		return common.Hash{}
	}
	// 反序列化
	if len(enc) > 0 {
		_, content, _, err := rlp.Split(enc)
		if err != nil {
			self.setError(err)
		}
		value.SetBytes(content)
	}
	// 填充缓存
	self.cachedStorage[key] = value
	return value
}

func (self *stateObject) SetState(db Database, key, value common.Hash) {
	self.db.journal.append(storageChange{
		account:  &self.address,
		key:      key,
		prevalue: self.GetState(db, key),
	})
	self.setState(key, value)
}

func (self *stateObject) setState(key, value common.Hash) {
	self.cachedStorage[key] = value
	self.dirtyStorage[key] = value
}

func (self *stateObject) SetCode(codeHash common.Hash, code []byte) {
	self.setCode(codeHash, code)
}

func (self *stateObject) setCode(codeHash common.Hash, code []byte) {
	self.code = code
	self.data.CodeHash = codeHash[:]
	self.dirtyCode = true
}
