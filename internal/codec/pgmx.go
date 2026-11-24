package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// PGMOptions 是 PGM 头结构支路的两个开关。
//   - ExtHeader：扩展头开关，开启后写入位深、标志、校验和三个字段
//   - FlipRows：行序开关，开启后像素按行底朝上存储，并在头中置行序标志
type PGMOptions struct {
	ExtHeader bool
	FlipRows  bool
}

// PGMMeta 是解码端完全按文件头还原出的开关状态。
type PGMMeta struct {
	ExtHeader bool
	FlipRows  bool
}

// 扩展头支路错误码。
var (
	ErrFieldOutOfRange = errors.New("字段越界")
	ErrFlagConflict    = errors.New("标志冲突")
	ErrChecksum        = errors.New("校验和不匹配")
	ErrTruncated       = errors.New("数据被截断")
)

// 扩展块固定布局常量，解码端只允许通过这些偏移读字段。
const (
	extMagic = "#GTKX\n" // 扩展头魔数，PGM 注释形式，跟在标准头之后
	rowMagic = "#GTKR\n" // 仅行序开关开启（扩展头关闭）时的独立标志
	extSize  = 12        // 扩展块总长度

	offMagic     = 0 // 魔数偏移
	offBitDepth  = 6 // 位深字段偏移
	offFlags     = 7 // 标志字段偏移
	offChecksum  = 8 // 校验和字段偏移
	magicLen     = 6
	bitDepthLen  = 1
	flagsLen     = 1
	checksumLen  = 4
	flagFlipped  = 0x01 // 标志字节 bit0：1=底朝上
	validBitDepth = 8  // 当前像素为 8 位灰度，位深字段只允许 8
)

// 扩展块字段排布表（偏移相对扩展块首字节，解码端按本表固定偏移读取）：
//
//	字段    类型                长度(字节)  偏移
//	魔数    [6]byte "#GTKX\n"   6          0
//	位深    uint8               1          6
//	标志    uint8               1          7
//	校验和  uint32 大端         4          8
//
// 三字段互不覆盖，扩展块总长 12 字节。标志字节仅 bit0 合法（1=底朝上），
// bit1..bit7 为保留位，出现非零值即标志冲突。校验和为像素区（按存储行序）
// 的 CRC-32/IEEE。
// 行序单独开启时不产生二进制扩展块，标准头与像素之间只放 "#GTKR\n" 标志。

// EncodePGMEx 在经典 P5 编码上接出头结构支路。
func EncodePGMEx(w, h int, pix []uint8, opt PGMOptions) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	stored := pix
	if opt.FlipRows {
		stored = reverseRows(w, h, pix)
	}
	out := make([]byte, 0, len("P5\n\n\n\n")+16+extSize+w*h)
	out = append(out, []byte(fmt.Sprintf("P5\n%d %d\n255\n", w, h))...)
	switch {
	case opt.ExtHeader:
		block := make([]byte, extSize)
		copy(block[offMagic:offMagic+magicLen], extMagic)
		block[offBitDepth] = validBitDepth
		if opt.FlipRows {
			block[offFlags] = flagFlipped
		}
		binary.BigEndian.PutUint32(block[offChecksum:offChecksum+checksumLen], crc32.ChecksumIEEE(stored))
		out = append(out, block...)
	case opt.FlipRows:
		out = append(out, rowMagic...)
	}
	out = append(out, stored...)
	return out, nil
}

// DecodePGMEx 按文件头还原扩展字段；任何字段被改坏都返回带偏移位置的错误码。
func DecodePGMEx(data []byte) (int, int, []uint8, PGMMeta, error) {
	meta := PGMMeta{}
	if len(data) < 10 || data[0] != 'P' || data[1] != '5' {
		return 0, 0, nil, meta, errors.New("不是 P5 流")
	}
	i := 2
	next := func() (int, bool) {
		for i < len(data) && (data[i] == ' ' || data[i] == '\n' || data[i] == '\r') {
			i++
		}
		start, val := i, 0
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			val = val*10 + int(data[i]-'0')
			i++
		}
		return val, i > start
	}
	w, ok1 := next()
	h, ok2 := next()
	maxv, ok3 := next()
	if !ok1 || !ok2 || !ok3 || w <= 0 || h <= 0 || maxv != 255 {
		return 0, 0, nil, meta, errors.New("PGM 头非法")
	}
	i++ // 跳过分隔符

	switch {
	case len(data)-i >= magicLen && string(data[i:i+magicLen]) == extMagic:
		if len(data)-i < extSize {
			return 0, 0, nil, meta, fmt.Errorf("%w: 扩展块不完整（应 %d 字节，偏移 %d 起）", ErrTruncated, extSize, i)
		}
		block := data[i : i+extSize]
		meta.ExtHeader = true
		if bd := block[offBitDepth]; bd != validBitDepth {
			return 0, 0, nil, meta, fmt.Errorf("%w: 位深字段（偏移 %d）值 %d 非法，仅允许 %d",
				ErrFieldOutOfRange, offBitDepth, bd, validBitDepth)
		}
		fl := block[offFlags]
		if fl&^flagFlipped != 0 {
			return 0, 0, nil, meta, fmt.Errorf("%w: 行序标志（偏移 %d）保留位非零: %#02x",
				ErrFlagConflict, offFlags, fl)
		}
		meta.FlipRows = fl&flagFlipped != 0
		wantSum := binary.BigEndian.Uint32(block[offChecksum : offChecksum+checksumLen])
		i += extSize
		if len(data)-i < w*h {
			return 0, 0, nil, meta, fmt.Errorf("%w: 像素数据不足（偏移 %d 起）", ErrTruncated, i)
		}
		stored := data[i : i+w*h]
		if got := crc32.ChecksumIEEE(stored); got != wantSum {
			return 0, 0, nil, meta, fmt.Errorf("%w: 校验和字段（偏移 %d）头中 %#08x 实算 %#08x",
				ErrChecksum, offChecksum, wantSum, got)
		}
	case len(data)-i >= magicLen && string(data[i:i+magicLen]) == rowMagic:
		meta.FlipRows = true
		i += magicLen
		if len(data)-i < w*h {
			return 0, 0, nil, meta, fmt.Errorf("%w: 像素数据不足（偏移 %d 起）", ErrTruncated, i)
		}
	default:
		if len(data)-i < w*h {
			return 0, 0, nil, meta, errors.New("像素数据不足")
		}
	}

	pix := make([]uint8, w*h)
	stored := data[i : i+w*h]
	if meta.FlipRows {
		for y := 0; y < h; y++ {
			copy(pix[y*w:(y+1)*w], stored[(h-1-y)*w:(h-y)*w])
		}
	} else {
		copy(pix, stored)
	}
	return w, h, pix, meta, nil
}

func reverseRows(w, h int, pix []uint8) []uint8 {
	out := make([]uint8, len(pix))
	for y := 0; y < h; y++ {
		copy(out[(h-1-y)*w:(h-y)*w], pix[y*w:(y+1)*w])
	}
	return out
}
