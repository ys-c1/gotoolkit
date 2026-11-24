// Package checksum 实现按文件体计算的多项式校验（CRC），
// 并在校验失败时把单字节改动定位到精确偏移。
package checksum

import (
	"fmt"
	"sort"
	"strings"
)

// Definition 是一套 CRC 参数（反射形算法）。
type Definition struct {
	Name   string // 公开配置中使用的名字
	Width  int    // 校验值位宽：16 或 32
	Poly   uint32 // 反射形多项式
	Init   uint32 // 初始值
	XorOut uint32 // 结果异或值
}

// 多项式选取依据：每个位宽档都选该档最常用、参数有公开标准来源可查的多项式。
var (
	// CRC32IEEE 即 CRC-32/ISO-HDLC：多项式 0x04C11DB7（反射 0xEDB88320），
	// 来源 ITU-T V.42 / RFC 1952，zlib、PNG、Ethernet 同款，是 32 位档最常用的多项式。
	CRC32IEEE = Definition{Name: "crc32-ieee", Width: 32, Poly: 0xEDB88320, Init: 0xFFFFFFFF, XorOut: 0xFFFFFFFF}
	// CRC16IBM 即 CRC-16/IBM（ARC）：多项式 0x8005（反射 0xA001），
	// 来源 IBM（ARC 压缩程序），Modbus、USB 的 16 位校验与其同族，是 16 位档最常用的多项式。
	CRC16IBM = Definition{Name: "crc16-ibm", Width: 16, Poly: 0xA001, Init: 0x0000, XorOut: 0x0000}
)

var registry = map[string]Definition{
	CRC32IEEE.Name: CRC32IEEE,
	CRC16IBM.Name:  CRC16IBM,
}

// Names 返回全部可选多项式名字（排序后）。
func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ByName 按名字取多项式参数；名字非法时返回带该名字的错误。
func ByName(name string) (Definition, error) {
	if d, ok := registry[name]; ok {
		return d, nil
	}
	return Definition{}, fmt.Errorf("未知多项式 %q（可选：%s）", name, strings.Join(Names(), ", "))
}

// CRC 是建表后的校验计算器。
type CRC struct {
	def   Definition
	mask  uint32
	table [256]uint32
	rev   map[uint32]byte // table[d] -> d，用于单字节定位
	top   map[byte]byte   // table[d] 的最高字节 -> d，用于反推状态
}

// New 按参数建表。所选多项式的状态转移可逆，建表时一并验证。
func New(def Definition) *CRC {
	c := &CRC{
		def:  def,
		mask: ^uint32(0) >> (32 - def.Width),
		rev:  make(map[uint32]byte, 256),
		top:  make(map[byte]byte, 256),
	}
	for i := 0; i < 256; i++ {
		v := uint32(i)
		for k := 0; k < 8; k++ {
			if v&1 != 0 {
				v = (v >> 1) ^ def.Poly
			} else {
				v >>= 1
			}
		}
		c.table[i] = v & c.mask
	}
	for i := 0; i < 256; i++ {
		c.rev[c.table[i]] = byte(i)
		t := byte(c.table[i] >> (def.Width - 8))
		if old, dup := c.top[t]; dup && old != byte(i) {
			panic(fmt.Sprintf("checksum: 多项式 %s 不支持字节级反推", def.Name))
		}
		c.top[t] = byte(i)
	}
	return c
}

// Width 返回校验值位宽。
func (c *CRC) Width() int { return c.def.Width }

// Name 返回多项式名字。
func (c *CRC) Name() string { return c.def.Name }

// Sum 计算 data 的校验值。
func (c *CRC) Sum(data []byte) uint32 {
	sum := c.def.Init
	for _, b := range data {
		sum = c.table[byte(sum)^b] ^ (sum >> 8)
	}
	return (sum ^ c.def.XorOut) & c.mask
}

// shift8inv 是“状态移出低字节并查表”的逆运算，用于从流尾向前扫描。
func (c *CRC) shift8inv(y uint32) uint32 {
	lo := c.top[byte(y>>(c.def.Width-8))]
	hi := y ^ c.table[lo]
	return ((hi << 8) | uint32(lo)) & c.mask
}

// Candidate 是一处可解释校验差分的单字节改动。
type Candidate struct {
	Offset int  // 在被校验字节流中的偏移
	Delta  byte // 该字节被异或的差值
}

// LocateAll 假设 covered 与原始数据等长且只差一个字节，
// 找出所有能使校验值重新吻合的单字节改动，按偏移从大到小返回。
//
// 原理：多项式校验对等长消息是线性的，校验值的异或差只取决于差分
// 字节与其位置，因此从流尾向前 O(n) 即可枚举全部候选；每个候选再
// 全量复算一次确认。16 位档在大文件上可能返回多个候选。
//
// 会临时修改 covered 但返回前已复原。
func (c *CRC) LocateAll(covered []byte, stored uint32) []Candidate {
	s := c.Sum(covered) ^ stored
	if s == 0 {
		return nil
	}
	var out []Candidate
	cur := s
	for posFromEnd := 0; posFromEnd < len(covered); posFromEnd++ {
		if d, hit := c.rev[cur]; hit && d != 0 {
			off := len(covered) - 1 - posFromEnd
			covered[off] ^= d
			if c.Sum(covered) == stored {
				out = append(out, Candidate{Offset: off, Delta: d})
			}
			covered[off] ^= d
		}
		cur = c.shift8inv(cur)
	}
	return out
}
