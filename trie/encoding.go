package trie

// compact第3位：terminator位
// compact第4位：奇偶标志位
func hexToCompact(hex []byte) []byte {
	terminator := byte(0)
	if hasTerm(hex) {
		terminator = 1
		hex = hex[:len(hex)-1]
	}
	buf := make([]byte, len(hex)/2+1)
	buf[0] = terminator << 5

	// 奇数位处理
	if len(hex)&1 == 1 {
		buf[0] |= 1 << 4 // 第4位设成 1
		buf[0] |= hex[0] // 5，6，7，8位设成第一个 nibble
		hex = hex[1:]    // 调整 hex 的偏移量
	}
	decodeNibbles(hex, buf[1:])
	return buf
}

func compactToHex(compact []byte) []byte {
	//  分裂 compact, 构建 hex
	base := keybytesToHex(compact)
	base = base[:len(base)-1]

	if base[0] >= 2 {
		base = append(base, 16)
	}
	chop := 2 - base[0]&1
	return base[chop:]
}

func keybytesToHex(str []byte) []byte {
	l := len(str)*2 + 1
	// 分裂keybytes，构建 nibbles
	var nibbles = make([]byte, l)
	for i, b := range str {
		nibbles[i*2] = b / 16
		nibbles[i*2+1] = b % 16
	}
	// 最后一位设置成 16
	nibbles[l-1] = 16
	return nibbles
}

// hex 必须是偶数个字节
func hexToKeybytes(hex []byte) []byte {
	// 删除结尾的字节(16)
	if hasTerm(hex) {
		hex = hex[:len(hex)-1]
	}
	if len(hex)&1 != 0 {
		panic("can't convert hex key of odd length")
	}

	// 合并nibble, 构建 keybytes
	key := make([]byte, len(hex)/2)
	decodeNibbles(hex, key)
	return key
}

// 将 nibbles 合并写入 bytes
func decodeNibbles(nibbles []byte, bytes []byte) {
	for bi, ni := 0, 0; ni < len(nibbles); bi, ni = bi+1, ni+2 {
		bytes[bi] = nibbles[ni]<<4 | nibbles[ni+1]
	}
}

func hasTerm(s []byte) bool {
	return len(s) > 0 && s[len(s)-1] == 16
}

func prefixLen(a, b []byte) int {
	var i, length = 0, len(a)
	if len(b) < length {
		length = len(b)
	}
	for ; i < length; i++ {
		if a[i] != b[i] {
			break
		}
	}
	return i
}
