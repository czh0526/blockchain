package discover

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	mrand "math/rand"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/crypto"
	"github.com/czh0526/blockchain/log"
	"github.com/czh0526/blockchain/p2p/netutil"
)

const (
	alpha           = 3
	bucketSize      = 16
	maxReplacements = 10

	// [0...239]：buckets[0]
	// [240]: buckets[1]
	hashBits          = len(common.Hash{}) * 8 // 地址空间的总位数：32 * 8 = 256
	nBuckets          = hashBits / 15          // 桶的个数：总位数/15 = 17
	bucketMinDistance = hashBits - nBuckets    // 最小距离： 256 - 17 = 239

	bucketIPLimit, bucketSubnet = 2, 24
	tableIPLimit, tableSubnet   = 10, 24

	maxBondingPingPongs = 16 // Limit on the number of concurrent ping/pong interactions
	maxFindnodeFailures = 5  // Nodes exceeding this limit are dropped

	seedMinTableTime = 5 * time.Minute
	seedCount        = 30
	seedMaxAge       = 5 * 24 * time.Hour
)

type bucket struct {
	entries      []*Node
	replacements []*Node
	ips          netutil.DistinctNetSet
}

func (b *bucket) bump(n *Node) bool {
	for i := range b.entries {
		if b.entries[i].ID == n.ID {
			copy(b.entries[1:], b.entries[:i])
			b.entries[0] = n
			return true
		}
	}
	return false
}

type Table struct {
	mutex   sync.Mutex
	buckets [nBuckets]*bucket
	nursery []*Node
	rand    *mrand.Rand
	ips     netutil.DistinctNetSet

	db         *nodeDB
	refreshReq chan chan struct{}
	initDone   chan struct{}
	closeReq   chan struct{}
	closed     chan struct{}

	bondmu    sync.Mutex
	bonding   map[NodeID]*bondproc
	bondslots chan struct{}

	nodeAddedHook func(*Node)

	net  transport
	self *Node
}

type bondproc struct {
	err  error
	n    *Node
	done chan struct{}
}

func (tab *Table) Close() {
	select {
	case <-tab.closed:
	case tab.closeReq <- struct{}{}:
		<-tab.closed
	}
}

type transport interface {
	ping(NodeID, *net.UDPAddr) error
	waitping(NodeID) error
	findnode(toid NodeID, addr *net.UDPAddr, target NodeID) ([]*Node, error)
	close()
}

func newTable(t transport, ourID NodeID, ourAddr *net.UDPAddr, nodeDBPath string, bootnodes []*Node) (*Table, error) {

	db, err := newNodeDB(nodeDBPath, Version, ourID)
	if err != nil {
		return nil, err
	}

	tab := &Table{
		net:        t,
		db:         db,
		self:       NewNode(ourID, ourAddr.IP, uint16(ourAddr.Port), uint16(ourAddr.Port)),
		bonding:    make(map[NodeID]*bondproc),
		bondslots:  make(chan struct{}, maxBondingPingPongs),
		refreshReq: make(chan chan struct{}),
		initDone:   make(chan struct{}),
		closeReq:   make(chan struct{}),
		closed:     make(chan struct{}),
		rand:       mrand.New(mrand.NewSource(0)),
		ips:        netutil.DistinctNetSet{Subnet: tableSubnet, Limit: tableIPLimit},
	}
	if err := tab.setFallbackNodes(bootnodes); err != nil {
		return nil, err
	}

	// 握手操作的初始化
	for i := 0; i < cap(tab.bondslots); i++ {
		tab.bondslots <- struct{}{}
	}

	// 桶的初始化
	for i := range tab.buckets {
		tab.buckets[i] = &bucket{
			ips: netutil.DistinctNetSet{Subnet: bucketSubnet, Limit: bucketIPLimit},
		}
	}

	// 初始化rand
	tab.seedRand()
	// 向桶中填充 seed 节点
	tab.loadSeedNodes(false)

	// 启动 nodeDB 的节点过期检测
	tab.db.ensureExpirer()
	// 启动内部循环，处理refresh和validate事件
	go tab.loop()
	return tab, nil
}

func (tab *Table) setFallbackNodes(nodes []*Node) error {
	for _, n := range nodes {
		if err := n.validateComplete(); err != nil {
			return fmt.Errorf("bad bootstrap/fallback node %q (%v)", n, err)
		}
	}
	tab.nursery = make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		cpy := *n
		cpy.sha = crypto.Keccak256Hash(n.ID[:])
		tab.nursery = append(tab.nursery, &cpy)
	}
	return nil
}

// 使用随机数初始化rand的Seed
func (tab *Table) seedRand() {
	var b [8]byte
	crand.Read(b[:])

	tab.mutex.Lock()
	tab.rand.Seed(int64(binary.BigEndian.Uint64(b[:])))
	tab.mutex.Unlock()
}

func (tab *Table) Self() *Node {
	return tab.self
}

func (tab *Table) loop() {
	var (
		refresh        = time.NewTicker(30 * time.Minute)
		refreshDone    = make(chan struct{})
		revalidate     = time.NewTimer(tab.nextRevalidateTime())
		revalidateDone = make(chan struct{})
		copyNodes      = time.NewTicker(30 * time.Second)
		waiting        = []chan struct{}{tab.initDone}
	)
	defer refresh.Stop()
	defer revalidate.Stop()
	defer copyNodes.Stop()

	log.Info("[Discovery]: Table.loop() up, 定期的刷新和校验节点.")
	go tab.doRefresh(refreshDone)

loop:
	for {
		select {
		case <-refresh.C: // 定时刷新
			if refreshDone == nil {
				refreshDone = make(chan struct{})
				go tab.doRefresh(refreshDone)
			}
		case req := <-tab.refreshReq: // 主动刷新
			waiting = append(waiting, req)
			if refreshDone == nil {
				refreshDone = make(chan struct{})
				go tab.doRefresh(refreshDone)
			}
		case <-refreshDone: // 刷新完成
			for _, ch := range waiting {
				close(ch)
			}
			waiting, refreshDone = nil, nil
		case <-revalidate.C: // 定时校验
			go tab.doRevalidate(revalidateDone)
		case <-revalidateDone: // 校验完成
			revalidate.Reset(tab.nextRevalidateTime())
		case <-copyNodes.C: //
			go tab.copyBondedNodes()
		case <-tab.closeReq: // 关闭
			break loop
		}
	}

	if tab.net != nil {
		tab.net.close()
	}
	if refreshDone != nil {
		<-refreshDone
	}
	for _, ch := range waiting {
		close(ch)
	}
	close(tab.closed)
}

func (tab *Table) doRefresh(done chan struct{}) {
	defer close(done)
	log.Debug(fmt.Sprintf("Table.doRefresh() was called at %s", time.Now().Format("2006-01-02 15:04:05")))

	tab.loadSeedNodes(true)
	// 找到三个离自己最近的节点
	log.Debug(fmt.Sprintf("Table.doRefresh() —— find self.ID 0x%x...", tab.self.ID[:4]))
	tab.lookup(tab.self.ID, false)

	// 通过找不存在的节点，发现全网的节点
	for i := 0; i < 3; i++ {
		var target NodeID
		crand.Read(target[:])
		log.Debug(fmt.Sprintf("Table.doRefresh() —— find rand ID 0x%x...", target[:4]))
		tab.lookup(target, false)
	}
}

func (tab *Table) Lookup(targetID NodeID) []*Node {
	return tab.lookup(targetID, true)
}

func (tab *Table) lookup(targetID NodeID, refreshIfEmpty bool) []*Node {
	var (
		target         = crypto.Keccak256Hash(targetID[:])
		asked          = make(map[NodeID]bool)
		seen           = make(map[NodeID]bool)
		reply          = make(chan []*Node, alpha)
		pendingQueries = 0
		result         *nodesByDistance
	)

	asked[tab.self.ID] = true

	// 取就近节点进行 findnode 询问
	for {
		// 从 NodeDB 中取得最多 bucketSize 个就近的节点
		tab.mutex.Lock()
		result = tab.closest(target, bucketSize)
		tab.mutex.Unlock()
		if len(result.entries) > 0 || !refreshIfEmpty {
			break
		}

		// 没有取得就近节点，执行refresh, 等NodeDB更新后，重新查询就近节点
		<-tab.refresh()
		// refreshIsEmpty 只生效一次
		refreshIfEmpty = false
	}

	log.Info(fmt.Sprintf("Table.lookup() targetId = %x", targetID[:4]))
	for {
		// 按距离Target远近，一次执行 alpha(3)个 findnode + bond, 然后等待执行结果
		for i := 0; i < len(result.entries) && pendingQueries < alpha; i++ {
			n := result.entries[i]

			if !asked[n.ID] {
				asked[n.ID] = true
				pendingQueries++

				// 启动节点查找例程
				go func() {
					log.Info(fmt.Sprintf("<FINDNODE> ==> %s, target = 0x%x ", n.addr(), targetID[:4]))
					// 发送 findnode 消息, 找到未知节点列表
					r, err := tab.net.findnode(n.ID, n.addr(), targetID)
					if err != nil {
						// Bump the failure counter
						fails := tab.db.findFails(n.ID) + 1
						tab.db.updateFindFails(n.ID, fails)
						log.Trace("Bumping findnode failure counter", "id", n.ID, "failcount", fails)

						if fails >= maxFindnodeFailures {
							log.Trace("Too many find node failures, dropping", "id", n.ID, "failcount", fails)
							tab.delete(n)
						}
					}
					// 向未知节点列表 ping/pong 握手过程
					reply <- tab.bondall(r)
				}()
			}
		}

		// result.entries 中的节点都被查询过
		if pendingQueries == 0 {
			break
		}
		// 一个 bond 过程执行完毕，将可用节点设置在 result 前面
		for _, n := range <-reply {
			if n != nil && !seen[n.ID] {
				seen[n.ID] = true
				result.push(n, bucketSize)
			}
		}
		pendingQueries--
	}
	return result.entries
}

func (tab *Table) ReadRandomNodes(buf []*Node) (n int) {
	if !tab.isInitDone() {
		return 0
	}
	tab.mutex.Lock()
	defer tab.mutex.Unlock()

	// Find all non-empty buckets and get a fresh slice of their entries.
	var buckets [][]*Node
	for _, b := range tab.buckets {
		if len(b.entries) > 0 {
			buckets = append(buckets, b.entries[:])
		}
	}
	if len(buckets) == 0 {
		return 0
	}
	// Shuffle the buckets.
	for i := len(buckets) - 1; i > 0; i-- {
		j := tab.rand.Intn(len(buckets))
		buckets[i], buckets[j] = buckets[j], buckets[i]
	}
	// Move head of each bucket into buf, removing buckets that become empty.
	var i, j int
	for ; i < len(buf); i, j = i+1, (j+1)%len(buckets) {
		b := buckets[j]
		buf[i] = &(*b[0])
		buckets[j] = b[1:]
		if len(b) == 1 {
			buckets = append(buckets[:j], buckets[j+1:]...)
		}
		if len(buckets) == 0 {
			break
		}
	}
	return i + 1
}

func (tab *Table) Resolve(targetID NodeID) *Node {
	// If the node is present in the local table, no
	// network interaction is required.
	hash := crypto.Keccak256Hash(targetID[:])
	tab.mutex.Lock()
	cl := tab.closest(hash, 1)
	tab.mutex.Unlock()
	if len(cl.entries) > 0 && cl.entries[0].ID == targetID {
		return cl.entries[0]
	}
	// Otherwise, do a network lookup.
	result := tab.Lookup(targetID)
	for _, n := range result {
		if n.ID == targetID {
			return n
		}
	}
	return nil
}

func (tab *Table) closest(target common.Hash, nresults int) *nodesByDistance {
	// 构造排序数组
	close := &nodesByDistance{target: target}
	// 遍历buckets
	for _, b := range tab.buckets {
		// 遍历entries
		for _, n := range b.entries {
			// 将 node 放入排序数组
			close.push(n, nresults)
		}
	}
	return close
}

func (tab *Table) doRevalidate(done chan<- struct{}) {
	defer func() {
		done <- struct{}{}
	}()

	// 得到一个需要validate的节点
	last, bi := tab.nodeToRevalidate()
	if last == nil {
		return
	}

	// 通过ping消息握手
	err := tab.ping(last.ID, last.addr())

	tab.mutex.Lock()
	defer tab.mutex.Unlock()
	// 获取节点所在的桶
	b := tab.buckets[bi]
	if err == nil {
		// 如果握手成功，更新节点的活跃度
		b.bump(last)
		return
	}

	// 如果握手失败，用replacements节点替换它，或者直接删除
	if r := tab.replace(b, last); r != nil {
		log.Debug("Replaced dead node", "b", bi, "id", last.ID, "ip", last.IP, "r", r.ID, "rip", r.IP)
	} else {
		log.Debug("Removed dead node", "b", bi, "id", last.ID, "ip", last.IP)
	}
}

func (tab *Table) replace(b *bucket, last *Node) *Node {
	if len(b.entries) == 0 || b.entries[len(b.entries)-1].ID != last.ID {
		// Entry has moved, don't replace it.
		return nil
	}

	if len(b.replacements) == 0 {
		tab.deleteInBucket(b, last)
		return nil
	}

	// 从replacements队列中随机取出一个节点
	r := b.replacements[tab.rand.Intn(len(b.replacements))]
	// 替换last节点
	b.entries[len(b.entries)-1] = r
	tab.removeIP(b, last.IP)
	return r
}

func (tab *Table) refresh() <-chan struct{} {
	done := make(chan struct{})
	select {
	case tab.refreshReq <- done:
	case <-tab.closed:
		close(done)
	}
	return done
}

func (tab *Table) loadSeedNodes(bond bool) {
	seeds := tab.db.querySeeds(seedCount, seedMaxAge)
	seeds = append(seeds, tab.nursery...)
	if bond {
		seeds = tab.bondall(seeds)
	}
	for i := range seeds {
		seed := seeds[i]
		tab.add(seed)
	}
}

func (tab *Table) copyBondedNodes() {
	tab.mutex.Lock()
	defer tab.mutex.Unlock()

	now := time.Now()
	for _, b := range tab.buckets {
		for _, n := range b.entries {
			if now.Sub(n.addedAt) >= seedMinTableTime {
				tab.db.updateNode(n)
			}
		}
	}
}

// 发送 ping 消息, 并等待返回结果（收到pong, 或得到error）
func (tab *Table) ping(id NodeID, addr *net.UDPAddr) error {
	tab.db.updateLastPing(id, time.Now())
	if err := tab.net.ping(id, addr); err != nil {
		return err
	}
	tab.db.updateBondTime(id, time.Now())
	return nil
}

//   收到ping消息： 发送ping
// 没收到ping消息： 发送ping, waitping()
func (tab *Table) pingpong(w *bondproc, pinged bool, id NodeID, addr *net.UDPAddr, tcpPort uint16) {
	// 获取slots资源
	<-tab.bondslots
	defer func() {
		tab.bondslots <- struct{}{}
	}()

	// 主动进行ping/pong握手
	if w.err = tab.ping(id, addr); w.err != nil {
		close(w.done)
		return
	}

	// 进入被动等待状态
	if !pinged {
		log.Trace(fmt.Sprintf("等待远程发起的 bond: <== %v:%v/%v ", addr.IP, addr.Port, tcpPort))
		tab.net.waitping(id)
	}

	// 返回节点
	w.n = NewNode(id, addr.IP, uint16(addr.Port), tcpPort)
	close(w.done)
}

func (tab *Table) bondall(nodes []*Node) (result []*Node) {
	// 并行操作，主动向全部的nodes节点发起连接
	rc := make(chan *Node, len(nodes))
	for i := range nodes {
		go func(n *Node) {
			// 发起主动的的握手过程
			nn, _ := tab.bond(false, n.ID, n.addr(), n.TCP)
			// 握手完成后，将结果写入rc
			rc <- nn
		}(nodes[i])
	}

	// 收集结果，并返回
	for range nodes {
		if n := <-rc; n != nil {
			result = append(result, n)
		}
	}
	return result
}

// 操作 tab.bonding，进行bonding的状态管理
func (tab *Table) bond(pinged bool, id NodeID, addr *net.UDPAddr, tcpPort uint16) (*Node, error) {
	if id == tab.self.ID {
		return nil, errors.New("is self")
	}
	if pinged && !tab.isInitDone() {
		return nil, errors.New("still initializing")
	}

	// 检查目标节点的状态是否已经失效
	node, fails := tab.db.node(id), tab.db.findFails(id)
	age := time.Since(tab.db.bondTime(id))
	var result error
	if fails > 0 || age > nodeDBNodeExpiration {
		log.Trace(fmt.Sprintf("Starting bonding ping/pong, id = %x known = %v", id.Bytes()[:4], node != nil))

		// 检查目标节点的 bondproc 状态
		tab.bondmu.Lock()
		w := tab.bonding[id]
		if w != nil {
			// 如果上一轮握手还未结束，等待返回上轮结束
			tab.bondmu.Unlock()
			<-w.done
		} else {
			// 如果是新的握手，构造bondproc -> bonding
			w = &bondproc{done: make(chan struct{})}
			tab.bonding[id] = w
			tab.bondmu.Unlock()
			// 执行双向的ping/pong握手
			tab.pingpong(w, pinged, id, addr, tcpPort)
			tab.bondmu.Lock()
			delete(tab.bonding, id)
			tab.bondmu.Unlock()
		}

		result = w.err
		if result == nil {
			node = w.n
		}
	}

	if node != nil {
		tab.add(node)
		log.Info(fmt.Sprintf("向 NodeDB 中添加节点：%v:%v/%v", node.IP, node.TCP, node.UDP))
		tab.db.updateFindFails(id, 0)
	}
	return node, result
}

func (tab *Table) isInitDone() bool {
	select {
	case <-tab.initDone:
		return true
	default:
		return false
	}
}

// 得到一个节点，重新 validate。
// 返回节点和桶号
func (tab *Table) nodeToRevalidate() (n *Node, bi int) {
	tab.mutex.Lock()
	defer tab.mutex.Unlock()

	// 随机取几个bucket
	for _, bi = range tab.rand.Perm(len(tab.buckets)) {
		b := tab.buckets[bi]
		if len(b.entries) > 0 {
			// 得到第一个最不活跃的Node,返回
			last := b.entries[len(b.entries)-1]
			return last, bi
		}
	}
	return nil, 0
}

func (tab *Table) add(new *Node) {
	tab.mutex.Lock()
	defer tab.mutex.Unlock()

	b := tab.bucket(new.sha)
	if !tab.bumpOrAdd(b, new) {
		// 如果因为数量已满， 将新节点放入replacement队列
		tab.addReplacement(b, new)
	}
}

func (tab *Table) delete(node *Node) {
	tab.mutex.Lock()
	defer tab.mutex.Unlock()

	tab.deleteInBucket(tab.bucket(node.sha), node)
}

func (tab *Table) deleteInBucket(b *bucket, n *Node) {
	b.entries = deleteNode(b.entries, n)
	tab.removeIP(b, n.IP)
}

func (tab *Table) bucket(sha common.Hash) *bucket {
	d := logdist(tab.self.sha, sha)
	if d < bucketMinDistance {
		return tab.buckets[0]
	}
	return tab.buckets[d-bucketMinDistance-1]
}

func (tab *Table) bumpOrAdd(b *bucket, n *Node) bool {
	// 如果n在列表中，变为最活跃的节点
	if b.bump(n) {
		return true
	}

	if len(b.entries) > bucketSize || !tab.addIP(b, n.IP) {
		return false
	}

	// 否则，将n加入entries列表
	b.entries, _ = pushNode(b.entries, n, bucketSize)
	// 确保 replacements中不存在n
	b.replacements = deleteNode(b.replacements, n)
	// 设置节点的时间戳
	n.addedAt = time.Now()
	return true
}

func (tab *Table) addReplacement(b *bucket, n *Node) {
	for _, e := range b.replacements {
		if e.ID == n.ID {
			return
		}
	}
	if !tab.addIP(b, n.IP) {
		return
	}
	var removed *Node
	b.replacements, removed = pushNode(b.replacements, n, maxReplacements)
	if removed != nil {
		tab.removeIP(b, removed.IP)
	}
}

// 从列表中删除n，并返回新列表
func deleteNode(list []*Node, n *Node) []*Node {
	for i := range list {
		if list[i].ID == n.ID {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// 把n放在[0], 把最后面的元素删除，并返回
func pushNode(list []*Node, n *Node, max int) ([]*Node, *Node) {
	if len(list) < max {
		list = append(list, nil)
	}
	removed := list[len(list)-1]
	copy(list[1:], list)
	list[0] = n
	return list, removed
}

// 将 nodes 放入 tab 中
func (tab *Table) stuff(nodes []*Node) {
	tab.mutex.Lock()
	defer tab.mutex.Unlock()

	for _, n := range nodes {
		if n.ID == tab.self.ID {
			continue
		}
		b := tab.bucket(n.sha)
		if len(b.entries) < bucketSize {
			tab.bumpOrAdd(b, n)
		}
	}
}

func (tab *Table) addIP(b *bucket, ip net.IP) bool {
	if netutil.IsLAN(ip) {
		return true
	}
	if !tab.ips.Add(ip) {
		log.Debug("IP exceeds table limit", "ip", ip)
		return false
	}
	if !b.ips.Add(ip) {
		log.Debug("IP exceeds bucket limit", "ip", ip)
		tab.ips.Remove(ip)
		return false
	}
	return true
}

func (tab *Table) removeIP(b *bucket, ip net.IP) {
	if netutil.IsLAN(ip) {
		return
	}
	tab.ips.Remove(ip)
	b.ips.Remove(ip)
}

func (tab *Table) nextRevalidateTime() time.Duration {
	tab.mutex.Lock()
	defer tab.mutex.Unlock()

	return time.Duration(tab.rand.Int63n(int64(10 * time.Minute)))
}

type nodesByDistance struct {
	entries []*Node
	target  common.Hash
}

// entries 中的节点距离 Target 由近及远。
func (h *nodesByDistance) push(n *Node, maxElems int) {
	// 通过给定函数，进行n的定位ix
	ix := sort.Search(len(h.entries), func(i int) bool {
		// node i --> target > n --> target
		return distcmp(h.target, h.entries[i].sha, n.sha) > 0
	})

	// 将n放在最后
	if len(h.entries) < maxElems {
		h.entries = append(h.entries, n)
	}
	// 将n移动到ix位置
	if ix == len(h.entries) {

	} else {
		copy(h.entries[ix+1:], h.entries[ix:])
		h.entries[ix] = n
	}
}
