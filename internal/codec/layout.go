package codec

import "errors"

// Layout 把“逻辑像素（行优先、自上而下）”映射到“字节流里的存储行”。
// 三个维度互相正交：
//
//	BottomUp  行序    false = 第 0 行先存（自上而下）；true = 第 h-1 行先存（自下而上）
//	BigEndian 字节序  true = 头里的多字节整数按大端落字节；false = 小端
//	Align     行对齐  1 = 紧凑；4 = 每个存储行补齐到 4 的倍数（补零）。0 等价于 1
//
// 8 位灰度样本本身没有字节序，所以 BigEndian 只作用于格式头里的整数。
// 对 PGM 这种纯 ASCII 头、无多字节整数的格式，字节序标志不改变任何字节，
// 属于“声明了但不生效”，见 docs/layout.md。
type Layout struct {
	BottomUp  bool
	BigEndian bool
	Align     int
}

// RowAlign 返回有效的行对齐字节数。
func (l Layout) RowAlign() int {
	if l.Align <= 1 {
		return 1
	}
	return l.Align
}

// Stride 返回一行占用的存储字节数（含行尾补零）。
func (l Layout) Stride(rowBytes int) int {
	a := l.RowAlign()
	return (rowBytes + a - 1) / a * a
}

// LogicalRow 把“第 s 个被存储的物理行”映射回逻辑行号。
// 行序翻转是自逆的，因此编码与解码两侧共用同一个函数。
func (l Layout) LogicalRow(h, s int) int {
	if l.BottomUp {
		return h - 1 - s
	}
	return s
}

var errBadLayout = errors.New("排布参数非法")

func (l Layout) validate() error {
	if l.Align != 0 && l.Align != 1 && l.Align != 4 {
		return errBadLayout
	}
	return nil
}

func put16(b []byte, off int, v uint16, big bool) {
	if big {
		b[off] = byte(v >> 8)
		b[off+1] = byte(v)
		return
	}
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
}

func put32(b []byte, off int, v uint32, big bool) {
	if big {
		b[off] = byte(v >> 24)
		b[off+1] = byte(v >> 16)
		b[off+2] = byte(v >> 8)
		b[off+3] = byte(v)
		return
	}
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
	b[off+2] = byte(v >> 16)
	b[off+3] = byte(v >> 24)
}

func get16(b []byte, off int, big bool) uint16 {
	if big {
		return uint16(b[off])<<8 | uint16(b[off+1])
	}
	return uint16(b[off]) | uint16(b[off+1])<<8
}

func get32(b []byte, off int, big bool) uint32 {
	if big {
		return uint32(b[off])<<24 | uint32(b[off+1])<<16 | uint32(b[off+2])<<8 | uint32(b[off+3])
	}
	return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
}

// PackRows 按排布打包：每行 rowBytes 字节，行尾补零到 Stride。
// 输入 pix 是自顶向下的逻辑像素；输出是存储序的行优先字节。
func PackRows(w, h, bpp int, pix []uint8, l Layout) []byte {
	rowBytes := w * bpp
	s := l.Stride(rowBytes)
	out := make([]byte, 0, s*h)
	for y := 0; y < h; y++ {
		src := l.LogicalRow(h, y)
		out = append(out, pix[src*rowBytes:(src+1)*rowBytes]...)
		for len(out)%s != 0 {
			out = append(out, 0)
		}
	}
	return out
}

// UnpackRows 是 PackRows 的逆；payload 长度须为 Stride*h。
func UnpackRows(w, h, bpp int, payload []byte, l Layout) []uint8 {
	rowBytes := w * bpp
	s := l.Stride(rowBytes)
	pix := make([]uint8, rowBytes*h)
	for y := 0; y < h; y++ {
		dst := l.LogicalRow(h, y)
		copy(pix[dst*rowBytes:(dst+1)*rowBytes], payload[y*s:y*s+rowBytes])
	}
	return pix
}

// checkRowPad 校验每行行尾补位为零；对齐为 1 时不检查。
func checkRowPad(payload []byte, rowBytes, h int, l Layout) error {
	a := l.RowAlign()
	if a == 1 {
		return nil
	}
	s := l.Stride(rowBytes)
	if len(payload) < s*h {
		return errors.New("像素数据不足")
	}
	for y := 0; y < h; y++ {
		for _, b := range payload[y*s+rowBytes : (y+1)*s] {
			if b != 0 {
				return errors.New("行尾补位非零")
			}
		}
	}
	return nil
}
