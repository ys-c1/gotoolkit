package codec

import (
	"encoding/binary"
	"errors"
)

// 掩码尾段紧跟各格式的原始载荷之后，布局固定：
//
//	"GTMK"(4) | 掩码值个数 n(2, LE) | 掩码值 n 个（严格升序、互不相同） | 位图字节数 m(2, LE) | 位图 m 字节
//
// 位图中每像素 1 位：1 表示该像素透明（灰度命中任一掩码值），0 表示不透明；每字节高位在前。
// 最后一个字节的填充位必须为 0。
var maskMagic = []byte{'G', 'T', 'M', 'K'}

// maskHeadLen 是 "GTMK" 魔数(4) 加掩码值个数(2) 的长度，即掩码值数组的起点。
// 位图字节数(2) 紧跟在 n 个掩码值之后，所以尾段总长 = maskHeadLen + n + 2 + m。
const maskHeadLen = 4 + 2

// ErrNoMask 表示流中没有掩码尾段。
var ErrNoMask = errors.New("流中没有掩码尾段")

// buildMaskTrailer 依据严格升序的掩码值与 alpha 生成尾段。
// alpha 长度必须等于像素数；alpha[i]==0 表示透明。
func buildMaskTrailer(maskValues []uint8, alpha []uint8) []byte {
	n := (len(alpha) + 7) / 8
	out := make([]byte, 0, maskHeadLen+len(maskValues)+2+n)
	out = append(out, maskMagic...)
	out = append(out, byte(len(maskValues)), byte(len(maskValues)>>8))
	out = append(out, maskValues...)
	out = append(out, byte(n), byte(n>>8))
	bits := make([]byte, n)
	for i, a := range alpha {
		if a == 0 {
			bits[i>>3] |= 0x80 >> uint(i&7)
		}
	}
	return append(out, bits...)
}

// parseMaskTrailer 从 payloadEnd 处解析尾段，返回掩码值、长度恰为 nPix 的 alpha。
// 没有尾段时返回 ErrNoMask；尾段被截断、长度不符、填充位非零时返回具体错误。
func parseMaskTrailer(data []byte, payloadEnd, nPix int) ([]uint8, []uint8, error) {
	if len(data) < payloadEnd+maskHeadLen {
		return nil, nil, ErrNoMask
	}
	t := data[payloadEnd:]
	if string(t[:4]) != string(maskMagic) {
		return nil, nil, ErrNoMask
	}
	nvals := int(binary.LittleEndian.Uint16(t[4:]))
	if nvals == 0 || nvals > 256 {
		return nil, nil, errors.New("掩码值个数越界")
	}
	valsAt := maskHeadLen
	mAt := valsAt + nvals
	if len(t) < mAt+2 {
		return nil, nil, errors.New("掩码尾段被截断")
	}
	vals := make([]uint8, nvals)
	copy(vals, t[valsAt:mAt])
	for i := 1; i < len(vals); i++ {
		if vals[i] <= vals[i-1] {
			return nil, nil, errors.New("掩码值未按严格升序排列或存在重复")
		}
	}
	m := int(binary.LittleEndian.Uint16(t[mAt:]))
	if m != (nPix+7)/8 {
		return nil, nil, errors.New("掩码位图长度与画幅不符")
	}
	if len(t) != mAt+2+m {
		return nil, nil, errors.New("掩码尾段长度不符")
	}
	bits := t[mAt+2:]
	alpha := make([]uint8, nPix)
	for i := range alpha {
		if bits[i>>3]&(0x80>>uint(i&7)) != 0 {
			alpha[i] = 0
		} else {
			alpha[i] = 255
		}
	}
	for i := nPix; i < m*8; i++ {
		if bits[i>>3]&(0x80>>uint(i&7)) != 0 {
			return nil, nil, errors.New("掩码填充位非零")
		}
	}
	return vals, alpha, nil
}

func checkMaskArgs(w, h int, pix, alpha []uint8, maskValues []uint8) error {
	if w <= 0 || h <= 0 || len(pix) != w*h || len(alpha) != w*h {
		return errors.New("画幅或像素数不匹配")
	}
	if len(maskValues) == 0 || len(maskValues) > 256 {
		return errors.New("掩码值个数非法")
	}
	return nil
}
