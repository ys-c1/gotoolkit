package api

import (
	"encoding/hex"
	"fmt"

	"gotoolkit/internal/codec"
)

// RowPlacement 是一行逻辑像素在存储流中的落点。
// Offset 的空间由 OffsetSpace 指明：
//   - "file"    → 编码字节流里的绝对偏移（pgm、bmp）
//   - "decoded" → 游程解压后的像素流偏移（rle）
type RowPlacement struct {
	LogicalRow int    // 逻辑行号，0 = 图像顶部
	StoredRow  int    // 存储槽号，0 = 最先写出的行
	Offset     int    // 行首偏移，单位见 OffsetSpace
	PixelLen   int    // 该行有效像素字节数
	PadLen     int    // 该行行尾补位字节数
	Bytes      []byte // 该行有效像素在存储流中的字节
}

// LayoutDescription 是对“某格式 + 某排布”的可核对描述，供映射表生成器与测试使用。
type LayoutDescription struct {
	Format      string
	W, H        int
	RowOrder    string // down | up
	ByteOrder   string // le | be
	RowAlign    int    // 1 | 4
	OffsetSpace string // file | decoded
	HeaderLen   int
	HeaderHex   string
	Stride      int
	PixelStream []byte // rle 解压后的存储序像素流；pgm/bmp 为文件中的像素区

	// 三个标志在本格式/本画幅下是否真的改变了字节。false = 声明了但不生效。
	RowOrderEffective  bool
	ByteOrderEffective bool
	AlignEffective     bool
	Rows               []RowPlacement
	Notes              []string
}

// InertFlags 返回本描述里未生效的标志名；空表示三个都生效。
func (d LayoutDescription) InertFlags() []string {
	var out []string
	if !d.RowOrderEffective {
		out = append(out, "RowOrder")
	}
	if !d.ByteOrderEffective {
		out = append(out, "ByteOrder")
	}
	if !d.AlignEffective {
		out = append(out, "RowAlign")
	}
	return out
}

// Describe 给出某格式在某排布下的逐行落点表。
// pix 为自顶向下的逻辑像素；传 nil 时用全零像素（头字节与行跨度不受像素值影响）。
func Describe(format string, w, h int, pix []uint8, opt Options) (LayoutDescription, error) {
	if w <= 0 || h <= 0 {
		return LayoutDescription{}, ErrBadSize
	}
	if pix == nil {
		pix = make([]uint8, w*h)
	}
	if len(pix) != w*h {
		return LayoutDescription{}, ErrBadSize
	}
	opt.Format = format
	if err := opt.validate(); err != nil {
		return LayoutDescription{}, err
	}
	l, err := opt.Layout()
	if err != nil {
		return LayoutDescription{}, err
	}
	body, err := Encode(&Image{W: w, H: h, Pix: pix}, opt)
	if err != nil {
		return LayoutDescription{}, err
	}

	d := LayoutDescription{
		Format:      format,
		W:           w,
		H:           h,
		RowOrder:    rowOrderName(l.BottomUp),
		ByteOrder:   byteOrderName(l.BigEndian),
		RowAlign:    l.Align,
		OffsetSpace: "file",
	}
	var rowBytes int
	switch format {
	case "bmp":
		rowBytes = 3 * w
		d.HeaderLen = 54
		d.Notes = append(d.Notes,
			"BMP 头 54 字节；所有多字节整数按 ByteOrder 落字节",
			"bfHeight 符号随行序：up→正（自底向上），down→负（自上而下）")
	case "rle":
		rowBytes = w
		d.HeaderLen = 8
		d.OffsetSpace = "decoded"
		d.Notes = append(d.Notes,
			"GTR1 头 8 字节：magic[4] + width:uint16 + height:uint16，宽高按 ByteOrder 落字节",
			"Offset 是游程解压后的像素流偏移；补位零可能与相邻游程合并，故不按文件偏移核对")
	default:
		rowBytes = w
		d.HeaderLen = headerLenPGM(w, h)
		d.Notes = append(d.Notes,
			"PGM 头是 ASCII 十进制，没有多字节整数，ByteOrder 对本格式不生效")
	}
	d.Stride = l.Stride(rowBytes)
	if d.HeaderLen > len(body) {
		return LayoutDescription{}, fmt.Errorf("编码结果短于头长度")
	}
	d.HeaderHex = hex.EncodeToString(body[:d.HeaderLen])
	switch format {
	case "bmp":
		d.PixelStream = body[54:]
	case "rle":
		d.PixelStream = decodeRLEPayload(body, l, w, h)
		d.HeaderHex = hex.EncodeToString(body[:8])
		d.HeaderLen = 8
	default:
		d.PixelStream = body[d.HeaderLen:]
	}

	d.RowOrderEffective = h > 1
	d.ByteOrderEffective = format != "pgm"
	// 行对齐是否生效，取决于“换一个对齐值”会不会改变行跨度，
	// 而不是当前跨度是否大于有效像素宽。
	d.AlignEffective = l.Stride(rowBytes) != codec.Layout{Align: 1}.Stride(rowBytes) ||
		l.Stride(rowBytes) != codec.Layout{Align: 4}.Stride(rowBytes)
	base := 0
	if d.OffsetSpace == "file" {
		base = d.HeaderLen
	}
	for s := 0; s < h; s++ {
		y := l.LogicalRow(h, s)
		start := s * d.Stride
		row := append([]byte(nil), d.PixelStream[start:start+rowBytes]...)
		d.Rows = append(d.Rows, RowPlacement{
			LogicalRow: y,
			StoredRow:  s,
			Offset:     base + start,
			PixelLen:   rowBytes,
			PadLen:     d.Stride - rowBytes,
			Bytes:      row,
		})
	}
	if !d.ByteOrderEffective {
		d.Notes = append(d.Notes, "警告：ByteOrder 在本格式下不生效")
	}
	if !d.AlignEffective {
		d.Notes = append(d.Notes, "警告：RowAlign 在本画幅下不生效（行宽已是对齐倍数）")
	}
	if !d.RowOrderEffective {
		d.Notes = append(d.Notes, "警告：RowOrder 在单行图像下不生效")
	}
	return d, nil
}

// decodeRLEPayload 只为描述取回存储序像素流；输入已被 Encode 验证过。
func decodeRLEPayload(body []byte, l codec.Layout, w, h int) []byte {
	rowBytes := w
	s := l.Stride(rowBytes)
	raw := make([]byte, 0, s*h)
	for i := 8; i+1 < len(body) && len(raw) < cap(raw); i += 2 {
		n := int(body[i])
		for k := 0; k < n && len(raw) < cap(raw); k++ {
			raw = append(raw, body[i+1])
		}
	}
	return raw
}

func rowOrderName(bottomUp bool) string {
	if bottomUp {
		return "up"
	}
	return "down"
}

func byteOrderName(big bool) string {
	if big {
		return "be"
	}
	return "le"
}

func headerLenPGM(w, h int) int {
	return len(fmt.Sprintf("P5\n%d %d\n255\n", w, h))
}
