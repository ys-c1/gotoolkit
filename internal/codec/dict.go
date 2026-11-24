package codec

import (
	"container/heap"
	"errors"
	"sort"
)

// GTD1 是带显式字典与转义的无损灰度图编码。
//
// 布局（多字节整数均为小端）：
//
//	"GTD1"                      4 字节魔数
//	宽度、高度                  各 2 字节
//	字典条目数 D                1 字节（含 ESC，1 <= D <= 256）
//	字典符号                    D 个字节（最后一个恒为 0xFF，即 ESC）
//	每符号码长                  D 个字节（规范化 Huffman 码长，单位 bit）
//	载荷比特数 L                4 字节（后续 bit 流的有效位数）
//	bit 流                      ceil(L/8) 字节，MSB 优先
//
// 编码的符号流是一阶差分：首个像素按原值，之后每像素为当前值与前一像素
// 的模 256 差。频次 >=2 且不等于 0xFF 的差分符号进字典并分配 Huffman 码；
// 其余符号（含只出现一次的符号与 0xFF）走转义：ESC 的 Huffman 码 +
// 8 bit 原符号。字典满（255 个普通成员）时频次最低者降级为转义。
const (
	gtdMagic   = "GTD1"
	gtdEsc     = uint8(0xFF)
	gtdMaxW    = 65535
	gtdMaxH    = 65535
	gtdMaxDict = 256
	gtdMaxCode = 32 // 码长上限；256 个符号的 Huffman 正常远低于此
)

// DictStats 把 GTD1 的开销按来源拆开。字节口径字段直接可对账，
// bit 口径字段用于区分字典码字与转义各自的开销。
type DictStats struct {
	HeaderBytes      int // 魔数4 + 宽高4 + 条目数1 + 载荷比特数4 = 13
	DictBytes        int // 字典符号 D + 码长 D = 2D
	BitstreamBytes   int // 仅 bit 流本身：ceil(PayloadBits/8)
	BitstreamPadBits int // bit 流按字节对齐的填充位数（0..7）
	PayloadBits      int // 载荷有效 bit 总数
	DictCodeBits     int // 字典内符号 Huffman 码占用 bit
	EscapeBits       int // 转义占用 bit（ESC 码字 + 8bit 字面量）
	EscapeEvents     int // 转义事件次数
	TotalBytes       int // 整个 GTD1 文件字节数
	DictEntries      int // D（含 ESC）
	EscapedSymbols   int // 未进字典、靠转义写出的不同符号数
}

// EncodeDict 用 GTD1 编码一张灰度图，并返回拆好的开销统计。
func EncodeDict(w, h int, pix []uint8) ([]byte, DictStats, error) {
	if w <= 0 || h <= 0 || w > gtdMaxW || h > gtdMaxH || len(pix) != w*h {
		return nil, DictStats{}, errors.New("画幅或像素数不匹配")
	}
	n := len(pix)

	// 1. 一阶差分（模 256）。
	syms := make([]uint8, n)
	syms[0] = pix[0]
	for i := 1; i < n; i++ {
		syms[i] = pix[i] - pix[i-1]
	}

	// 2. 差分符号频次。
	var freq [256]int
	for _, s := range syms {
		freq[s]++
	}

	// 3. 频次 >=2 进字典；其余走转义。0xFF 保留给 ESC，永不作为普通成员。
	type sf = huffItem
	members := make([]huffItem, 0, 256)
	escaped, escapeEvents := 0, 0
	for s, f := range freq {
		if f == 0 {
			continue
		}
		if f >= 2 && uint8(s) != gtdEsc {
			members = append(members, huffItem{uint8(s), f})
		} else {
			escaped++
			escapeEvents += f
		}
	}
	// 频次降序、同频按符号升序，输出确定。
	sort.Slice(members, func(i, j int) bool {
		if members[i].f != members[j].f {
			return members[i].f > members[j].f
		}
		return members[i].sym < members[j].sym
	})
	// ESC 必须占一个槽位；普通成员最多 255 个，满时把频次最低者降级为转义。
	for len(members) >= gtdMaxDict-1 {
		drop := members[len(members)-1]
		members = members[:len(members)-1]
		escaped++
		escapeEvents += drop.f
	}
	// ESC 占一个码字位置；无转义事件时给名义频次 1，保证它仍在码树里。
	escFreq := escapeEvents
	if escFreq == 0 {
		escFreq = 1
	}
	members = append(members, sf{gtdEsc, escFreq})
	d := len(members)

	// 4. Huffman 码长（确定性 tie-break）。
	lengths := huffmanLengths(members)
	for _, l := range lengths {
		if l > gtdMaxCode {
			return nil, DictStats{}, errors.New("Huffman 码长超过 32 bit，输入过于偏斜")
		}
	}

	// 5. 规范化码字与符号索引。
	idxOf := make([]int, 256)
	for i := range idxOf {
		idxOf[i] = -1
	}
	for i, m := range members {
		idxOf[m.sym] = i
	}
	codes, bl := canonicalCodes(lengths)

	// 6. 文件头 + 字典。
	out := make([]byte, 0, 13+2*d+(n+7)/8)
	out = append(out, gtdMagic...)
	out = append(out, byte(w), byte(w>>8), byte(h), byte(h>>8))
	out = append(out, byte(d))
	for _, m := range members {
		out = append(out, m.sym)
	}
	for _, l := range bl {
		out = append(out, byte(l))
	}

	// 7. bit 流。
	bw := newBitWriter()
	dictCodeBits, escapeBits := 0, 0
	escIdx := idxOf[gtdEsc]
	for _, s := range syms {
		k := idxOf[s]
		if k >= 0 && k != escIdx {
			bw.write(codes[k], bl[k])
			dictCodeBits += bl[k]
		} else {
			bw.write(codes[escIdx], bl[escIdx])
			bw.write(uint32(s), 8)
			escapeBits += bl[escIdx] + 8
		}
	}
	payloadBits := bw.bitsWritten()
	stream := bw.bytes()
	out = append(out,
		byte(payloadBits), byte(payloadBits>>8),
		byte(payloadBits>>16), byte(payloadBits>>24))
	out = append(out, stream...)

	st := DictStats{
		HeaderBytes:      13,
		DictBytes:        2 * d,
		BitstreamBytes:   len(stream),
		BitstreamPadBits: len(stream)*8 - payloadBits,
		PayloadBits:      payloadBits,
		DictCodeBits:     dictCodeBits,
		EscapeBits:       escapeBits,
		EscapeEvents:     escapeEvents,
		TotalBytes:       len(out),
		DictEntries:      d,
		EscapedSymbols:   escaped,
	}
	return out, st, nil
}

// DecodeDict 解析 GTD1 并还原像素；截断或自相矛盾一律报错。
func DecodeDict(data []byte) (int, int, []uint8, error) {
	if len(data) < 13 || string(data[0:4]) != gtdMagic {
		return 0, 0, nil, errors.New("不是 GTD1 流")
	}
	w := int(uint16(data[4]) | uint16(data[5])<<8)
	h := int(uint16(data[6]) | uint16(data[7])<<8)
	if w <= 0 || h <= 0 || w > gtdMaxW || h > gtdMaxH {
		return 0, 0, nil, errors.New("画幅非法")
	}
	d := int(data[8])
	if d < 1 || len(data) < 9+2*d+4 {
		return 0, 0, nil, errors.New("GTD1 字典被截断")
	}
	syms := make([]uint8, d)
	copy(syms, data[9:9+d])
	if syms[d-1] != gtdEsc {
		return 0, 0, nil, errors.New("字典末项必须是 ESC(0xFF)")
	}
	lengths := make([]int, d)
	for i := 0; i < d; i++ {
		lengths[i] = int(data[9+d+i])
		if lengths[i] <= 0 || lengths[i] > gtdMaxCode {
			return 0, 0, nil, errors.New("GTD1 码长非法")
		}
	}
	if !kraftOK(lengths) {
		return 0, 0, nil, errors.New("GTD1 码长不满足 Kraft 不等式")
	}
	codes, bl := canonicalCodes(lengths)
	off := 9 + 2*d
	payloadBits := int(uint32(data[off]) | uint32(data[off+1])<<8 |
		uint32(data[off+2])<<16 | uint32(data[off+3])<<24)
	stream := data[off+4:]
	if payloadBits > len(stream)*8 {
		return 0, 0, nil, errors.New("GTD1 载荷比特数超出流长度")
	}

	table := make(map[uint64]int, d)
	for i, c := range codes {
		table[canonicalKey(c, bl[i])] = i
	}
	br := newBitReader(stream, payloadBits)
	n := w * h
	pix := make([]uint8, n)
	var prev uint8
	for i := 0; i < n; i++ {
		code, nb, err := br.readCanonical(bl)
		if err != nil {
			return 0, 0, nil, err
		}
		k, ok := table[canonicalKey(code, nb)]
		if !ok {
			return 0, 0, nil, errors.New("GTD1 出现未定义码字")
		}
		var delta uint8
		if syms[k] == gtdEsc {
			v, err := br.read(8)
			if err != nil {
				return 0, 0, nil, errors.New("GTD1 转义字面量被截断")
			}
			delta = uint8(v)
		} else {
			delta = syms[k]
		}
		if i == 0 {
			pix[0] = delta
			prev = delta
		} else {
			prev += delta
			pix[i] = prev
		}
	}
	if br.consumed() != payloadBits {
		return 0, 0, nil, errors.New("GTD1 载荷长度与像素数不符")
	}
	return w, h, pix, nil
}

// ---- Huffman ----

type huffItem struct {
	sym uint8
	f   int
}

type huffNode struct {
	freq        int
	leaf        int // >=0 表示叶子，对应 members 下标；-1 表示内部节点
	left, right *huffNode
	seq         int // 合并序号，用于确定性 tie-break
}

type huffHeap []*huffNode

func (h huffHeap) Len() int { return len(h) }
func (h huffHeap) Less(i, j int) bool {
	if h[i].freq != h[j].freq {
		return h[i].freq < h[j].freq
	}
	return h[i].seq < h[j].seq
}
func (h huffHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *huffHeap) Push(x any)   { *h = append(*h, x.(*huffNode)) }
func (h *huffHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// huffmanLengths 返回与 members 同序的码长。
func huffmanLengths(members []huffItem) []int {
	m := len(members)
	if m == 1 {
		return []int{1}
	}
	pq := make(huffHeap, 0, m)
	for i, it := range members {
		pq = append(pq, &huffNode{freq: it.f, leaf: i, seq: i})
	}
	heap.Init(&pq)
	seq := m
	for pq.Len() > 1 {
		a := heap.Pop(&pq).(*huffNode)
		b := heap.Pop(&pq).(*huffNode)
		heap.Push(&pq, &huffNode{
			freq: a.freq + b.freq, leaf: -1,
			left: a, right: b, seq: seq,
		})
		seq++
	}
	depth := make([]int, m)
	var walk func(t *huffNode, d int)
	walk = func(t *huffNode, d int) {
		if t.leaf >= 0 {
			depth[t.leaf] = d
			return
		}
		walk(t.left, d+1)
		walk(t.right, d+1)
	}
	walk(pq[0], 0)
	return depth
}

// kraftOK 用 32 位二进制定点数校验 Kraft 不等式 sum(2^-l) <= 1。
// 码长不超过 32、条目不超过 256，累加不会溢出 uint64。
func kraftOK(lengths []int) bool {
	const unit = uint64(1) << 32
	var sum uint64
	for _, l := range lengths {
		sum += unit >> uint(l)
	}
	return sum <= unit
}

// canonicalCodes 由码长生成规范化码字。
func canonicalCodes(lengths []int) ([]uint32, []int) {
	m := len(lengths)
	bl := append([]int(nil), lengths...)
	if m == 1 {
		return []uint32{0}, bl
	}
	maxL := 0
	for _, l := range bl {
		if l > maxL {
			maxL = l
		}
	}
	var count [33]int
	for _, l := range bl {
		count[l]++
	}
	var first [33]uint32
	code := uint32(0)
	for L := 1; L <= maxL; L++ {
		first[L] = code
		code = (code + uint32(count[L])) << 1
	}
	codes := make([]uint32, m)
	next := first
	for i, l := range bl {
		codes[i] = next[l]
		next[l]++
	}
	return codes, bl
}

func canonicalKey(code uint32, nbits int) uint64 {
	return uint64(code) | uint64(nbits)<<32
}

// ---- bit IO：MSB 优先 ----

type bitWriter struct {
	buf   []byte
	cur   byte
	n     int
	total int
}

func newBitWriter() *bitWriter { return &bitWriter{} }

func (w *bitWriter) write(v uint32, nbits int) {
	for k := nbits - 1; k >= 0; k-- {
		w.cur = (w.cur << 1) | byte((v>>uint(k))&1)
		w.n++
		w.total++
		if w.n == 8 {
			w.buf = append(w.buf, w.cur)
			w.cur, w.n = 0, 0
		}
	}
}

func (w *bitWriter) bitsWritten() int { return w.total }

func (w *bitWriter) bytes() []byte {
	if w.n == 0 {
		return append([]byte(nil), w.buf...)
	}
	out := make([]byte, 0, len(w.buf)+1)
	out = append(out, w.buf...)
	out = append(out, w.cur<<uint(8-w.n))
	return out
}

type bitReader struct {
	buf   []byte
	limit int // 有效 bit 数
	pos   int // 已消费 bit 数
}

func newBitReader(buf []byte, limit int) *bitReader {
	return &bitReader{buf: buf, limit: limit}
}

func (r *bitReader) read(nbits int) (uint32, error) {
	if r.pos+nbits > r.limit {
		return 0, errors.New("bit 流越界")
	}
	var v uint32
	for k := 0; k < nbits; k++ {
		bi := r.pos
		v = (v << 1) | uint32((r.buf[bi/8]>>uint(7-bi%8))&1)
		r.pos++
	}
	return v, nil
}

// readCanonical 逐位读出一个前缀码：同码长码字连续，每读一位就检查
// 当前前缀是否落入某个码长的码字区间。
func (r *bitReader) readCanonical(lengths []int) (uint32, int, error) {
	maxL := 0
	var count [33]int
	for _, l := range lengths {
		count[l]++
		if l > maxL {
			maxL = l
		}
	}
	var first [33]uint32
	code := uint32(0)
	for L := 1; L <= maxL; L++ {
		first[L] = code
		code = (code + uint32(count[L])) << 1
	}
	var cur uint32
	for L := 1; L <= maxL; L++ {
		bit, err := r.read(1)
		if err != nil {
			return 0, 0, err
		}
		cur = (cur << 1) | bit
		if count[L] > 0 && cur >= first[L] && cur < first[L]+uint32(count[L]) {
			return cur, L, nil
		}
	}
	return 0, 0, errors.New("GTD1 前缀码无法解码")
}

func (r *bitReader) consumed() int { return r.pos }
