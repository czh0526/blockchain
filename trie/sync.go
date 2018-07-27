package trie

import (
	"errors"
	"fmt"

	"github.com/czh0526/blockchain/ethdb"

	"github.com/czh0526/blockchain/common"
	"gopkg.in/karalabe/cookiejar.v2/collections/prque"
)

var ErrNotRequested = errors.New("not requested")
var ErrAlreadyProcessed = errors.New("already processed")

type request struct {
	hash common.Hash
	data []byte
	raw  bool

	parents []*request
	depth   int
	deps    int

	callback LeafCallback
}

type syncMemBatch struct {
	batch map[common.Hash][]byte
	order []common.Hash
}

func newSyncMemBatch() *syncMemBatch {
	return &syncMemBatch{
		batch: make(map[common.Hash][]byte),
		order: make([]common.Hash, 0, 256),
	}
}

type SyncResult struct {
	Hash common.Hash
	Data []byte
}

// 同步请求调度器, 用于调度同步请求
type Sync struct {
	database DatabaseReader
	membatch *syncMemBatch
	requests map[common.Hash]*request // 全部的同步请求集合
	queue    *prque.Prque             // 请求的优先级队列
}

func NewSync(root common.Hash, database DatabaseReader, callback LeafCallback) *Sync {
	ts := &Sync{
		database: database,
		membatch: newSyncMemBatch(),
		requests: make(map[common.Hash]*request),
		queue:    prque.New(),
	}
	ts.AddSubTrie(root, 0, common.Hash{}, callback)
	return ts
}

// 挂起 request 请求
func (s *Sync) schedule(req *request) {
	// 如果 request 已经存在，合并 request
	if old, ok := s.requests[req.hash]; ok {
		old.parents = append(old.parents, req.parents...)
		return
	}

	// 如果 request 不存在，增加 request 到 queue & requests
	s.queue.Push(req.hash, float32(req.depth))
	s.requests[req.hash] = req
}

func (s *Sync) Process(results []SyncResult) (bool, int, error) {
	committed := false

	// 逐条处理 results
	for i, item := range results {
		// 检查 result 对应的 request 的状态
		request := s.requests[item.Hash]
		if request == nil {
			return committed, i, ErrNotRequested
		}
		if request.data != nil {
			return committed, i, ErrAlreadyProcessed
		}

		// 如果是 raw 类型的数据，处理返回 ( Account.code )
		if request.raw {
			request.data = item.Data
			s.commit(request)
			committed = true
			continue
		}

		// 如果不是 raw 类型，反序列化的 node，并处理
		node, err := decodeNode(item.Hash[:], item.Data, 0)
		if err != nil {
			return committed, i, err
		}
		request.data = item.Data

		// 处理 children node
		requests, err := s.children(request, node)
		if err != nil {
			return committed, i, err
		}
		if len(requests) == 0 && request.deps == 0 {
			s.commit(request)
			committed = true
			continue
		}
		request.deps += len(requests)
		// 递归 schedule
		for _, child := range requests {
			s.schedule(child)
		}
	}

	return committed, 0, nil
}

// 取出当前 queue 中的请求
func (s *Sync) Missing(max int) []common.Hash {
	requests := []common.Hash{}
	for !s.queue.Empty() && (max == 0 || len(requests) < max) {
		requests = append(requests, s.queue.PopItem().(common.Hash))
	}
	return requests
}

func (s *Sync) AddSubTrie(root common.Hash, depth int, parent common.Hash, callback LeafCallback) {
	if root == emptyRoot {
		return
	}
	if _, ok := s.membatch.batch[root]; ok {
		return
	}

	// 反序列化 root node
	key := root.Bytes()
	blob, _ := s.database.Get(key)
	if local, err := decodeNode(key, blob, 0); local != nil && err == nil {
		return
	}

	// 构建一个 request 对象
	req := &request{
		hash:     root,
		depth:    depth,
		callback: callback,
	}

	if parent != (common.Hash{}) {
		ancestor := s.requests[parent]
		if ancestor == nil {
			panic(fmt.Sprintf("sub-trie ancestor not found: %x", parent))
		}
		ancestor.deps++
		req.parents = append(req.parents, ancestor)
	}

	// 向 trie.Sync 对象添加一个 request
	s.schedule(req)
}

func (s *Sync) AddRawEntry(hash common.Hash, depth int, parent common.Hash) {
	if hash == emptyState {
		return
	}

	if _, ok := s.membatch.batch[hash]; ok {
		return
	}

	if ok, _ := s.database.Has(hash.Bytes()); ok {
		return
	}

	// 构建 request 对象
	req := &request{
		hash:  hash,
		raw:   true,
		depth: depth,
	}

	if parent != (common.Hash{}) {
		ancestor := s.requests[parent]
		if ancestor == nil {
			panic(fmt.Sprintf("raw-entry ancestor not found: %x", parent))
		}
		ancestor.deps++
		req.parents = append(req.parents, ancestor)
	}
	s.schedule(req)
}

// 将 requests 写入 membatch
func (s *Sync) commit(req *request) (err error) {
	// 填充 membatch
	s.membatch.batch[req.hash] = req.data
	s.membatch.order = append(s.membatch.order, req.hash)

	// 移除 request
	delete(s.requests, req.hash)

	// 向上处理 request 的 parents
	for _, parent := range req.parents {
		parent.deps--
		if parent.deps == 0 {
			if err := s.commit(parent); err != nil {
				return err
			}
		}
	}
	return nil
}

// 将 membatch 的内容刷新到 ethdb.Database 中
func (s *Sync) Commit(dbw ethdb.Putter) (int, error) {

	for i, key := range s.membatch.order {
		if err := dbw.Put(key[:], s.membatch.batch[key]); err != nil {
			return i, err
		}
	}
	written := len(s.membatch.order)

	s.membatch = newSyncMemBatch()
	return written, nil
}

// 根据 node 的子节点，生成对应的 requet 查询, 都是参数 req 的子查询
func (s *Sync) children(req *request, object node) ([]*request, error) {
	type child struct {
		node  node
		depth int
	}

	// 统计子节点
	children := []child{}
	switch node := object.(type) {
	case *shortNode:
		children = []child{{
			node:  node.Val,
			depth: req.depth + len(node.Key), // 计算深度
		}}
	case *fullNode:
		for i := 0; i < 17; i++ {
			if node.Children[i] != nil {
				children = append(children, child{
					node:  node.Children[i],
					depth: req.depth + 1, // 计算深度
				})
			}
		}
	default:
		panic(fmt.Sprintf("unknown node: %+v", node))
	}

	// 根据 children 构建 request
	requests := make([]*request, 0, len(children))
	for _, child := range children {
		// 叶节点(key. value)， 回调外部监视器
		if req.callback != nil {
			if node, ok := (child.node).(valueNode); ok {
				if err := req.callback(node, req.hash); err != nil {
					return nil, err
				}
			}
		}

		// 分支节点，递归构建 request
		if node, ok := (child.node).(hashNode); ok {
			hash := common.BytesToHash(node)
			if _, ok := s.membatch.batch[hash]; ok {
				continue
			}
			if ok, _ := s.database.Has(node); ok {
				continue
			}

			// 自上而下构建树
			requests = append(requests, &request{
				hash:     hash,
				parents:  []*request{req},
				depth:    child.depth,
				callback: req.callback,
			})
		}
	}
	return requests, nil
}
