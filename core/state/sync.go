package state

import (
	"bytes"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/rlp"
	"github.com/czh0526/blockchain/trie"
)

// 构建一个 trie.Sync 对象
func NewStateSync(root common.Hash, database trie.DatabaseReader) *trie.Sync {
	var syncer *trie.Sync
	callback := func(leaf []byte, parent common.Hash) error {

		// 反序列化 stateObject(Account) 对象。
		var obj Account
		if err := rlp.Decode(bytes.NewReader(leaf), &obj); err != nil {
			return err
		}
		// Account 的存储变量同步
		syncer.AddSubTrie(obj.Root, 64, parent, nil)
		// Account 的执行代码同步
		syncer.AddRawEntry(common.BytesToHash(obj.CodeHash), 64, parent)
		return nil
	}
	syncer = trie.NewSync(root, database, callback)
	return syncer
}
