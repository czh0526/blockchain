package vm

type Memory struct {
	store       []byte
	lastGasCost uint64
}

func NewMemory() *Memory {
	return &Memory{}
}

func (m *Memory) Len() int {
	return len(m.store)
}

func (m *Memory) Set(offset, size uint64, value []byte) {
	if size > uint64(len(m.store)) {
		panic("INVALID memory: store empty")
	}

	if size > 0 {
		copy(m.store[offset:offset+size], value)
	}
}

func (m *Memory) Resize(size uint64) {
	if uint64(m.Len()) < size {
		m.store = append(m.store, make([]byte, size-uint64(m.Len()))...)
	}
}

// 返回 memory 里面的数据拷贝
func (m *Memory) Get(offset, size int64) (cpy []byte) {
	if size == 0 {
		return nil
	}

	if len(m.store) > int(offset) {
		cpy = make([]byte, size)
		copy(cpy, m.store[offset:offset+size])

		return
	}
	return
}

// 返回 memory 里的 slice
func (m *Memory) GetPtr(offset, size int64) []byte {
	if size == 0 {
		return nil
	}

	if len(m.store) > int(offset) {
		return m.store[offset : offset+size]
	}
	return nil
}

func (m *Memory) Data() []byte {
	return m.store
}
