package codec

import (
	"encoding/binary"
	"errors"
	"math"
)

// BLK 是“分块 + 块内 min/max + 定长位深打包”的有损灰度格式。
//
// 设计目标是让“体积—质量”关系可以被精确推导：质量 Q 唯一决定位深 d(Q)，
// 而每个块的位流长度只由块像素数 k 与 d 决定（定长打包，不做熵编码），
// 因此整条流的字节数是 Q 的单调不减函数，二分反解才有严格依据。
//
// 文件布局：
//
//	偏移 长度 字段
//	0    4    魔数 "BLK1"
//	4    2    宽 W（uint16 LE）
//	6    2    高 H（uint16 LE）
//	8    1    分块边长 B
//	9    1    位深 d（1..8）
//	10   ...  块序列，每块 = min(1)+max(1)+ceil(k*d/8) 字节打包索引
const (
	BLKMagic       = "BLK1"
	BLKHeaderLen   = 10
	DefaultBlock   = 8
	MaxBlock       = 64
	BlockHeadBytes = 2 // 每块 min/max 两个字节
)

// NormalizeBlockSize 把非法（<=0）的分块边长归一成默认值，其余原样返回。
func NormalizeBlockSize(b int) int {
	if b <= 0 {
		return DefaultBlock
	}
	return b
}

// blkGrid 是分块网格的 O(1) 几何统计。
//
// x 方向：qx = W/B 个满宽 B 的块；若 rx = W%B > 0，再加一个宽 rx 的边块。
// y 方向同理。四个笛卡尔类别给出满块、右/下边块、角块各自的数量与像素数，
// 这样体积公式不必逐块循环，也能在解码前用常数时间校验长度。
type blkGrid struct {
	nx, ny int // 横纵块数 = ceil(W/B), ceil(H/B)
	n      int // 总块数 nx*ny
	qx, rx int // W/B, W%B
	qy, ry int
}

func blkGeometry(w, h, b int) blkGrid {
	g := blkGrid{qx: w / b, rx: w % b, qy: h / b, ry: h % b}
	g.nx = g.qx
	if g.rx > 0 {
		g.nx++
	}
	g.ny = g.qy
	if g.ry > 0 {
		g.ny++
	}
	g.n = g.nx * g.ny
	return g
}

// packedBytes 是 k 个索引按 d 位定长打包后的字节数。
func packedBytes(k, d int) int { return (k*d + 7) / 8 }

// BLKSize 给出 BLK1 流的精确字节数（含文件头与每块 min/max）。
//
// 通用式：
//
//	S = BLKHeaderLen + 2*N + Σ_j ceil(k_j*d/8)
//
// 其中 N=ceil(W/B)*ceil(H/B)，k_j 是第 j 块的像素数。
// 宽高都能被 B 整除时退化为：
//
//	S = 10 + (W/B)(H/B) * (2 + ceil(B²*d/8))
//	  = 10 + N_B*(2 + B²*d/8)   （B² 是 8 的倍数时 ceil 可去）
func BLKSize(w, h, b, d int) int {
	b = NormalizeBlockSize(b)
	if d < 1 {
		d = 1
	}
	if d > 8 {
		d = 8
	}
	g := blkGeometry(w, h, b)
	type cat struct {
		count, k int
	}
	var cats [4]cat
	cats[0] = cat{g.qx * g.qy, b * b} // 满×满
	if g.ry > 0 {
		cats[1] = cat{g.qx, b * g.ry} // 满宽×y 边
	}
	if g.rx > 0 {
		cats[2] = cat{g.qy, g.rx * b} // x 边×满高
	}
	if g.rx > 0 && g.ry > 0 {
		cats[3] = cat{1, g.rx * g.ry} // 角块
	}
	s := BLKHeaderLen + BlockHeadBytes*g.n
	for _, c := range cats {
		if c.count > 0 {
			s += c.count * packedBytes(c.k, d)
		}
	}
	return s
}

// BudgetDepth 是“分块尺寸 ↔ 预算”的反解式子：
// 给定总字节预算 T 与分块边长 B，返回仍放得下的最大位深 d（1..8）；
// 连 d=1 都放不下时返回 0。
//
// 令块数 N=ceil(W/B)ceil(H/B)，每块可花字节 b_blk=(T-10)/N，
// 扣除 2 字节 min/max 后：
//
//	d ≤ 8*(b_blk-2)/k，满网格 k=B²
//
// 整除情形化为 d* = floor(8*((T-10)/N - 2)/B²)
//
//	= 8*(T-10)/(W*H) - 16/B²
//
// 末式第一项是总预算对应的每像素位数，第二项 16/B² 是每块 2 字节头
// 摊到 B² 像素上的每像素位开销——B 越大，块头开销越小。
func BudgetDepth(w, h, b, budgetBytes int) int {
	b = NormalizeBlockSize(b)
	g := blkGeometry(w, h, b)
	if g.n == 0 {
		return 0
	}
	perBlock := (budgetBytes - BLKHeaderLen) / g.n // 每块预算（向下取整，保守）
	payloadBits := 8 * (perBlock - BlockHeadBytes)
	if payloadBits <= 0 {
		return 0
	}
	// 用满块像素数 B² 反解，保证最大的块也放得下。
	d := payloadBits / (b * b)
	if d > 8 {
		d = 8
	}
	if d < 1 {
		return 0
	}
	return d
}

// BlockSizeForBudget 是上式的反向写法：给定每块字节预算 blkBytes 与目标位深 d，
// 允许的最大分块边长 B = floor(sqrt(8*(blkBytes-2)/d))，下限 1。
func BlockSizeForBudget(blkBytes, depth int) int {
	if depth < 1 {
		depth = 1
	}
	payloadBytes := blkBytes - BlockHeadBytes
	if payloadBytes <= 0 {
		return 1
	}
	b := int(math.Sqrt(float64(8 * payloadBytes / depth)))
	for (b+1)*(b+1)*depth <= 8*payloadBytes {
		b++
	}
	for b*b*depth > 8*payloadBytes && b > 1 {
		b--
	}
	if b < 1 {
		b = 1
	}
	if b > MaxBlock {
		b = MaxBlock
	}
	return b
}

// EncodeBLK 编码一帧 BLK1。depth 是位深 1..8；blockSize<=0 用 DefaultBlock。
func EncodeBLK(w, h int, pix []uint8, blockSize, depth int) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	b := NormalizeBlockSize(blockSize)
	if b < 1 || b > 255 {
		return nil, errors.New("分块边长非法")
	}
	if depth < 1 || depth > 8 {
		return nil, errors.New("位深必须在 [1,8]")
	}

	total := BLKSize(w, h, b, depth)
	out := make([]byte, BLKHeaderLen, total)
	copy(out[0:4], BLKMagic)
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	out[8] = uint8(b)
	out[9] = uint8(depth)

	cw := (w + b - 1) / b
	ch := (h + b - 1) / b
	for by := 0; by < ch; by++ {
		y0 := by * b
		bh := minInt(b, h-y0)
		for bx := 0; bx < cw; bx++ {
			x0 := bx * b
			bw := minInt(b, w-x0)
			k := bw * bh

			mn, mx := uint8(255), uint8(0)
			for ry := 0; ry < bh; ry++ {
				row := pix[(y0+ry)*w+x0 : (y0+ry)*w+x0+bw]
				for _, v := range row {
					if v < mn {
						mn = v
					}
					if v > mx {
						mx = v
					}
				}
			}
			out = append(out, mn, mx)

			pb := packedBytes(k, depth)
			payload := make([]byte, pb)
			levelsMinus1 := (1 << depth) - 1
			bitPos := 0
			r := int(mx) - int(mn)
			for ry := 0; ry < bh; ry++ {
				row := pix[(y0+ry)*w+x0 : (y0+ry)*w+x0+bw]
				for x := 0; x < bw; x++ {
					var idx int
					if r == 0 {
						idx = 0
					} else {
						// 均匀量化到 2^d 个级，四舍五入。
						idx = (int(row[x]-mn)*levelsMinus1 + r/2) / r
						if idx > levelsMinus1 {
							idx = levelsMinus1
						}
					}
					for bit := 0; bit < depth; bit++ {
						if idx&(1<<bit) != 0 {
							payload[bitPos/8] |= 1 << (uint(bitPos) % 8)
						}
						bitPos++
					}
				}
			}
			out = append(out, payload...)
		}
	}
	if len(out) != total {
		return nil, errors.New("BLK 编码长度与体积公式不一致")
	}
	return out, nil
}

// BlockDecoder 逐块解码 BLK1，并持有一块跨所有块复用的中间像素缓冲 buf。
//
// 复用方式（重点）：NewBlockDecoder 只分配一次 buf，容量恒为 B*B；
// 每次 Next 把当前块（可能是更小的边块）重建进 buf[:k] 并把该切片返回。
// 调用方必须在下一次 Next 之前把返回像素拷走——DecodeBLK 的做法是立即
// 按行 copy 进整图 Pix。因为底层数组始终是同一块 buf，解 N 个块不会
// 产生 N 次 B*B 分配。TestBlockDecoderReusesBuffer 会校验底层数组地址不变。
type BlockDecoder struct {
	W, H  int
	B     int
	Depth int

	data []byte
	off  int
	cw   int
	ch   int
	bx   int
	by   int

	// buf 是唯一的分块中间缓冲，len 始终为 B*B，跨 Next 复用同一底层数组。
	buf []uint8
}

// NewBlockDecoder 解析 BLK1 头并校验整体长度，返回可逐块 Next 的解码器。
func NewBlockDecoder(data []byte) (*BlockDecoder, error) {
	if len(data) < BLKHeaderLen || string(data[0:4]) != BLKMagic {
		return nil, errors.New("不是 BLK1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return nil, errors.New("BLK 画幅非法")
	}
	b := int(data[8])
	d := int(data[9])
	if b < 1 || d < 1 || d > 8 {
		return nil, errors.New("BLK 分块或位深非法")
	}
	want := BLKSize(w, h, b, d)
	if len(data) < want {
		return nil, errors.New("BLK 块数据不足")
	}
	return &BlockDecoder{
		W: w, H: h, B: b, Depth: d,
		data: data, off: BLKHeaderLen,
		cw:  (w + b - 1) / b,
		ch:  (h + b - 1) / b,
		buf: make([]uint8, b*b), // 全程唯一一次分块缓冲分配
	}, nil
}

// Buffer 返回这块会被复用的中间缓冲（容量 B*B），主要用于观测/测试复用。
func (d *BlockDecoder) Buffer() []uint8 { return d.buf }

// Next 解码下一个块到复用缓冲。返回该块在整图中的左上角、实际宽高，
// 以及别名到内部缓冲的像素切片（长度 bw*bh，下次 Next 即被覆写）。
// 全部解完时返回 done=true。
func (d *BlockDecoder) Next() (x0, y0, bw, bh int, pixels []uint8, done bool, err error) {
	if d.by >= d.ch {
		return 0, 0, 0, 0, nil, true, nil
	}
	y0 = d.by * d.B
	x0 = d.bx * d.B
	bh = minInt(d.B, d.H-y0)
	bw = minInt(d.B, d.W-x0)
	k := bw * bh

	if d.off+2 > len(d.data) {
		return 0, 0, 0, 0, nil, false, errors.New("BLK 块头被截断")
	}
	mn := d.data[d.off]
	mx := d.data[d.off+1]
	d.off += 2

	pb := packedBytes(k, d.Depth)
	if d.off+pb > len(d.data) {
		return 0, 0, 0, 0, nil, false, errors.New("BLK 块载荷被截断")
	}
	payload := d.data[d.off : d.off+pb]
	d.off += pb

	// 关键：复用 d.buf 的同一底层数组，只重切片到本块 k，然后逐像素覆写。
	block := d.buf[:k]
	levelsMinus1 := (1 << d.Depth) - 1
	r := int(mx) - int(mn)
	bitPos := 0
	for i := 0; i < k; i++ {
		idx := 0
		for bit := 0; bit < d.Depth; bit++ {
			if payload[bitPos/8]&(1<<(uint(bitPos)%8)) != 0 {
				idx |= 1 << bit
			}
			bitPos++
		}
		if r == 0 {
			block[i] = mn
			continue
		}
		v := int(mn) + (idx*r+levelsMinus1/2)/levelsMinus1
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		block[i] = uint8(v)
	}

	// 推进块坐标（行优先）。
	d.bx++
	if d.bx >= d.cw {
		d.bx = 0
		d.by++
	}
	return x0, y0, bw, bh, block, false, nil
}

// DecodeBLK 解析整条 BLK1 流。它创建一个 BlockDecoder，并让所有块共用
// 同一个中间缓冲；每解出一块立即按行拷进整图 pix。
func DecodeBLK(data []byte) (int, int, []uint8, error) {
	bd, err := NewBlockDecoder(data)
	if err != nil {
		return 0, 0, nil, err
	}
	w, h := bd.W, bd.H
	pix := make([]uint8, w*h)
	for {
		x0, y0, bw, bh, block, done, err := bd.Next()
		if err != nil {
			return 0, 0, nil, err
		}
		if done {
			break
		}
		for ry := 0; ry < bh; ry++ {
			src := block[ry*bw : (ry+1)*bw]
			dst := pix[(y0+ry)*w+x0 : (y0+ry)*w+x0+bw]
			copy(dst, src) // 拷走后，block 缓冲即可在下一块被覆写/复用
		}
	}
	return w, h, pix, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
