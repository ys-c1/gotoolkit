package codec

import (
	"encoding/binary"
	"errors"
)

// rleHeaderLen 是 GTR1 头长度：4 字节魔数 + 2 字节宽 + 2 字节高。
const rleHeaderLen = 8

// EncodeRLE 输出自定义游程格式：GTR1 + 宽高 + (计数, 值) 序列。
//
// 游程序列起始偏移 L 满足 L ≡ 0 (mod AlignUnit)。名义长度 L0 = 8，
// 填充 p = padFor(8) = 0，故头部后不插入任何字节。这里仍统一走 padFor，
// 以免日后头部字段变动时漏掉对齐。
func EncodeRLE(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	out := make([]byte, rleHeaderLen+padFor(rleHeaderLen), rleHeaderLen+len(pix)*2)
	out = out[:rleHeaderLen]
	copy(out[0:], "GTR1")
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	i := 0
	for i < len(pix) {
		j := i
		for j < len(pix) && pix[j] == pix[i] && j-i < 255 {
			j++
		}
		out = append(out, uint8(j-i), pix[i])
		i = j
	}
	return out, nil
}

// DecodeRLE 解析游程格式。
//
// 游程序列从 rleHeaderLen 开始，按构造该偏移是 AlignUnit 的整数倍。
func DecodeRLE(data []byte) (int, int, []uint8, error) {
	body := rleHeaderLen + padFor(rleHeaderLen)
	if len(data) < body || string(data[0:4]) != "GTR1" {
		return 0, 0, nil, errors.New("不是 GTR1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	pix := make([]uint8, 0, w*h)
	for i := body; i+1 < len(data); i += 2 {
		n, v := int(data[i]), data[i+1]
		for k := 0; k < n; k++ {
			pix = append(pix, v)
		}
		if len(pix) > w*h {
			return 0, 0, nil, errors.New("游程超出画幅")
		}
	}
	if len(pix) != w*h {
		return 0, 0, nil, errors.New("像素数不足")
	}
	return w, h, pix, nil
}
