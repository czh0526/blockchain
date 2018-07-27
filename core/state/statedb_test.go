package state

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/ethdb"
)

func TestUpdateLeaks(t *testing.T) {
	db := ethdb.NewMemDatabase()
	statedb, _ := New(common.Hash{}, NewDatabase(db))

	for i := byte(0); i < 255; i++ {
		addr := common.BytesToAddress([]byte{i})
		// 设置 Balance
		statedb.AddBalance(addr, big.NewInt(int64(11*i)))
		// 设置 Nonce
		statedb.SetNonce(addr, uint64(42*i))
		if i%2 == 0 {
			// 设置 Storage
			statedb.SetState(addr, common.BytesToHash([]byte{i, i, i}), common.BytesToHash([]byte{i, i, i, i}))
		}
		if i%3 == 0 {
			// 设置 Code
			statedb.SetCode(addr, []byte{i, i, i, i, i})
		}
		statedb.IntermediateRoot(false)
	}

	for _, key := range db.Keys() {
		value, _ := db.Get(key)
		t.Errorf("State leaked into database: %x => %x ", key, value)
	}
}

func TestIntermediateLeaks(t *testing.T) {

	transDb := ethdb.NewMemDatabase()
	finalDb := ethdb.NewMemDatabase()
	transState, _ := New(common.Hash{}, NewDatabase(transDb))
	finalState, _ := New(common.Hash{}, NewDatabase(finalDb))

	// 修改一个账号包含的字段：balance, nonce, code, storage
	modify := func(state *StateDB, addr common.Address, i, tweak byte) {
		state.SetBalance(addr, big.NewInt(int64(11*i)+int64(tweak)))
		state.SetNonce(addr, uint64(42*i+tweak))
		if i%2 == 0 {
			state.SetState(addr, common.Hash{i, i, i, 0}, common.Hash{})
			state.SetState(addr, common.Hash{i, i, i, tweak}, common.Hash{i, i, i, i, tweak})
		}
		if i%3 == 0 {
			state.SetCode(addr, []byte{i, i, i, i, i, tweak})
		}
	}

	// 修改 255 个账号的字段
	for i := byte(0); i < 255; i++ {
		modify(transState, common.Address{byte(i)}, i, 0)
	}

	// 刷新 Trie & Trie Hash
	transState.IntermediateRoot(false)

	for i := byte(0); i < 255; i++ {
		modify(transState, common.Address{byte(i)}, i, 99)
		modify(finalState, common.Address{byte(i)}, i, 99)
	}

	if _, err := transState.Commit(false); err != nil {
		t.Fatalf("failed to commit transition state: %v", err)
	}
	if _, err := finalState.Commit(false); err != nil {
		t.Fatalf("failed to commit final state: %v", err)
	}

	for _, key := range finalDb.Keys() {
		if _, err := transDb.Get(key); err != nil {
			val, _ := finalDb.Get(key)
			t.Errorf("entry missing from the transition database: %x -> %x.", key, val)
		}
	}
	for _, key := range transDb.Keys() {
		if _, err := finalDb.Get(key); err != nil {
			val, _ := transDb.Get(key)
			t.Errorf("extra entry in the transition database: %x -> %x.", key, val)
		}
	}
}

func TestCopy(t *testing.T) {
	// 构建 StateDB
	orig, _ := New(common.Hash{}, NewDatabase(ethdb.NewMemDatabase()))
	// 插入 StateObject
	for i := byte(0); i < 255; i++ {
		obj := orig.GetOrNewStateObject(common.BytesToAddress([]byte{i}))
		obj.AddBalance(big.NewInt(int64(i)))
		orig.updateStateObject(obj)
	}
	orig.Finalise(false)

	// 复制出一个 StateDB, 向两个 StateDB 中的元素分别赋值
	copy := orig.Copy()
	for i := byte(0); i < 255; i++ {
		origObj := orig.GetOrNewStateObject(common.BytesToAddress([]byte{i}))
		copyObj := copy.GetOrNewStateObject(common.BytesToAddress([]byte{i}))

		origObj.AddBalance(big.NewInt(2 * int64(i)))
		copyObj.AddBalance(big.NewInt(3 * int64(i)))

		orig.updateStateObject(origObj)
		copy.updateStateObject(copyObj)
	}

	done := make(chan struct{})
	go func() {
		orig.Finalise(true)
		close(done)
	}()
	copy.Finalise(true)
	<-done

	for i := byte(0); i < 255; i++ {
		origObj := orig.GetOrNewStateObject(common.BytesToAddress([]byte{i}))
		copyObj := copy.GetOrNewStateObject(common.BytesToAddress([]byte{i}))

		if want := big.NewInt(3 * int64(i)); origObj.Balance().Cmp(want) != 0 {
			t.Errorf("orig obje %d: balnce mismatch: have %v, want %v", i, origObj.Balance(), want)
		}
		if want := big.NewInt(4 * int64(i)); copyObj.Balance().Cmp(want) != 0 {
			t.Errorf("copy obj %d: balance mismatch: have %v, want %v", i, copyObj.Balance(), want)
		}
	}
}

func TestSnapshotRandom(t *testing.T) {
	config := &quick.Config{MaxCount: 1000}
	err := quick.Check((*snapshotTest).run, config)
	if cerr, ok := err.(*quick.CheckError); ok {
		test := cerr.In[0].(*snapshotTest)
		t.Errorf("%v\n%s", test.err, test)
	} else if err != nil {
		t.Error(err)
	}
}

type snapshotTest struct {
	addrs     []common.Address // 一次Test中相关的地址集合
	actions   []testAction     // 一次Test中的动作序列
	snapshots []int            // 一次Test中的快照序列
	err       error
}

type testAction struct {
	name   string
	fn     func(testAction, *StateDB)
	args   []int64
	noAddr bool
}

func newTestAction(addr common.Address, r *rand.Rand) testAction {
	actions := []testAction{
		{
			name: "SetBalance",
			fn: func(a testAction, s *StateDB) {
				s.SetBalance(addr, big.NewInt(a.args[0]))
			},
			args: make([]int64, 1),
		},
		{
			name: "AddBalance",
			fn: func(a testAction, s *StateDB) {
				s.AddBalance(addr, big.NewInt(a.args[0]))
			},
			args: make([]int64, 1),
		},
		{
			name: "SetNonce",
			fn: func(a testAction, s *StateDB) {
				s.SetNonce(addr, uint64(a.args[0]))
			},
			args: make([]int64, 1),
		},
		{
			name: "SetState",
			fn: func(a testAction, s *StateDB) {
				var key, val common.Hash
				binary.BigEndian.PutUint16(key[:], uint16(a.args[0]))
				binary.BigEndian.PutUint16(val[:], uint16(a.args[1]))
			},
			args: make([]int64, 2),
		},
		{
			name: "SetCode",
			fn: func(a testAction, s *StateDB) {
				code := make([]byte, 16)
				binary.BigEndian.PutUint64(code, uint64(a.args[0]))
				binary.BigEndian.PutUint64(code[8:], uint64(a.args[1]))
				s.SetCode(addr, code)
			},
			args: make([]int64, 2),
		},
		{
			name: "CreateAccount",
			fn: func(a testAction, s *StateDB) {
				s.CreateAccount(addr)
			},
		}, /*
			{
				name: "Suicide",
				fn: func(a testAction, s *StateDB) {
					s.Suicide(addr)
				},
			},
			{
				name: "AddRefund",
				fn: func(a testAction, s *StateDB) {
					s.AddRefund(uint64(a.args[0]))
				},
				args:   make([]int64, 1),
				noAddr: true,
			},
			{
				name: "AddLog",
				fn: func(a testAction, s *StateDB) {
					data := make([]byte, 2)
					binary.BigEndian.PutUint16(data, uint16(a.args[0]))
					s.AddLog(&types.Log{Address: addr, Data: data})
				},
				args: make([]int64, 1),
			},*/
	}

	action := actions[r.Intn(len(actions))]
	var nameargs []string
	if !action.noAddr {
		nameargs = append(nameargs, addr.Hex())
	}
	for _, i := range action.args {
		action.args[i] = rand.Int63n(100)
		nameargs = append(nameargs, fmt.Sprint(action.args[i]))
	}
	action.name += strings.Join(nameargs, ", ")
	return action
}

func (*snapshotTest) Generate(r *rand.Rand, size int) reflect.Value {
	// 构建 50 个地址
	addrs := make([]common.Address, 50)
	for i := range addrs {
		addrs[i][0] = byte(i)
	}

	// 构建 size 个 testAction 序列
	actions := make([]testAction, size)
	for i := range actions {
		addr := addrs[r.Intn(len(addrs))]
		actions[i] = newTestAction(addr, r)
	}
	// 根据 size 个动作序列，构建 nsnapshots 个快照序列
	nsnapshots := int(math.Sqrt(float64(size)))
	if size > 0 && nsnapshots == 0 {
		nsnapshots = 1
	}
	snapshots := make([]int, nsnapshots)
	snaplen := len(actions) / nsnapshots
	// 在每段 actions 中，随机挑选一个 action 做快照
	for i := range snapshots {
		// Try to place the snapshots some number of actions apart from each other.
		snapshots[i] = (i * snaplen) + r.Intn(snaplen)
	}

	// 构建 snapshotTest
	return reflect.ValueOf(&snapshotTest{addrs, actions, snapshots, nil})
}

func (test *snapshotTest) run() bool {
	var (
		state, _     = New(common.Hash{}, NewDatabase(ethdb.NewMemDatabase()))
		snapshotRevs = make([]int, len(test.snapshots))
		sindex       = 0
	)

	// 从前往后，将 action 执行一遍，并顺序记录 snapshot
	for i, action := range test.actions {
		// 如果匹配最小的 snapshot，执行快照
		if len(test.snapshots) > sindex && i == test.snapshots[sindex] {
			snapshotRevs[sindex] = state.Snapshot()
			sindex++
		}
		action.fn(action, state)
	}

	// 从后往前，恢复 snapshot
	for sindex--; sindex >= 0; sindex-- {
		// 构建 checkstate, 从前向后执行 action, 执行到下一个 snapshot
		checkstate, _ := New(common.Hash{}, state.Database())
		for _, action := range test.actions[:test.snapshots[sindex]] {
			action.fn(action, checkstate)
		}
		// state 从后向前回滚到 snapshot
		state.RevertToSnapshot(snapshotRevs[sindex])

		// 比较两个状态是否相等
		if err := test.checkEqual(state, checkstate); err != nil {
			test.err = fmt.Errorf("state mismatch after revert to snapshot %d\n%v", sindex, err)
			return false
		}
	}
	return true
}

func (test *snapshotTest) checkEqual(state, checkstate *StateDB) error {
	for _, addr := range test.addrs {
		var err error
		checkeq := func(op string, a, b interface{}) bool {
			if err == nil && !reflect.DeepEqual(a, b) {
				err = fmt.Errorf("got %s(%s) == %v, want %v", op, addr.Hex(), a, b)
				return false
			}
			return true
		}

		checkeq("Exist", state.Exist(addr), checkstate.Exist(addr))
		// suicide
		checkeq("HasSuicided", state.HasSuicided(addr), checkstate.HasSuicided(addr))
		// balance
		checkeq("GetBalance", state.GetBalance(addr), checkstate.GetBalance(addr))
		// nonce
		checkeq("GetNonce", state.GetNonce(addr), checkstate.GetNonce(addr))
		// code
		checkeq("GetCode", state.GetCode(addr), checkstate.GetCode(addr))
		// code hash
		checkeq("GetCodeHash", state.GetCodeHash(addr), checkstate.GetCodeHash(addr))
		// code size
		checkeq("GetCodeSize", state.GetCodeSize(addr), checkstate.GetCodeSize(addr))

		// 对于每个地址对应的 stateObject
		if obj := state.getStateObject(addr); obj != nil {
			state.ForEachStorage(addr, func(key, val common.Hash) bool {
				return checkeq("GetState("+key.Hex()+")", val, checkstate.GetState(addr, key))
			})
			checkstate.ForEachStorage(addr, func(key, checkval common.Hash) bool {
				return checkeq("GetState("+key.Hex()+")", state.GetState(addr, key), checkval)
			})
		}

		if err != nil {
			return err
		}
	}

	return nil
}
