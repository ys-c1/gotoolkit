package codec

import (
	"encoding/binary"
	"errors"
)

// maskMagic 是掩码尾段的魔数，紧跟在各格式原始载荷之后。
// 尾段布局：GTMK(4) + 掩码值(1) + 位图字节数(2, LE) + 打包位图(每像素 1 位)。
// 位为 1 表示该像素等于掩码值（透明），位为 0 表示不透明；每字节高位在前。
var maskMagic = []byte{'G', 'T', 'M', 'K'}

const maskHeaderLen = 4 + 1 + 2

// ErrNoMask 表示流中没有掩码尾段。
var ErrNoMask = errors.New("流中没有掩码尾段")

// buildMaskTrailer 按 alpha 生成掩码尾段。
// alpha 长度必须等于像素数；alpha[i]==0 表示透明（该像素等于掩码值）。
func buildMaskTrailer(maskValue uint8, alpha []uint8) []byte {
	n := (len(alpha) + 7) / 8
	out := make([]byte, 0, maskHeaderLen+n)
	out = append(out, maskMagic...)
	out = append(out, maskValue)
	out = append(out, byte(n), byte(n>>8))
	bits := make([]byte, n)
	for i, a := range alpha {
		if a == 0 {
			bits[i>>3] |= 0x80 >> uint(i&7)
		}
	}
	return append(out, bits...)
}

// parseMaskTrailer 从 payloadEnd 处解析掩码尾段，返回掩码值与长度恰为 nPix 的 alpha。
// 透明像素 alpha 为 0，不透明为 255。没有尾段时返回 ErrNoMask。
func parseMaskTrailer(data []byte, payloadEnd, nPix int) (uint8, []uint8, error) {
	if len(data) < payloadEnd+maskHeaderLen {
		return 0, nil, ErrNoMask
	}
	t := data[payloadEnd:]
	if string(t[:4]) != string(maskMagic) {
		return 0, nil, ErrNoMask
	}
	maskValue := t[4]
	n := int(binary.LittleEndian.Uint16(t[5:]))
	if len(t) < maskHeaderLen+n {
		return 0, nil, errors.New("掩码尾段被截断")
	}
	if n != (nPix+7)/8 {
		return 0, nil, errors.New("掩码位图长度与画幅不符")
	}
	bits := t[maskHeaderLen : maskHeaderLen+n]
	alpha := make([]uint8, nPix)
	for i := range alpha {
		if bits[i>>3]&(0x80>>uint(i&7)) != 0 {
			alpha[i] = 0
		} else {
			alpha[i] = 255
		}
	}
	// 最后一个字节的填充位必须全为 0，否则尾段不是本工具编码的合法产物。
	for i := nPix; i < n*8; i++ {
		if bits[i>>3]&(0x80>>uint(i&7)) != 0 {
			return 0, nil, errors.New("掩码填充位非零")
		}
	}
	return maskValue, alpha, nil
}
