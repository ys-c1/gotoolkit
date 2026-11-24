package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// EncodeRLE 输出自定义游程格式：GTR1 + 宽高 + (计数, 值) 序列。
func EncodeRLE(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	out := make([]byte, 8, 8+len(pix)*2)
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

// DecodeRLE 解析游程格式。失败时返回 *DecodeError，精确定位到字节。
func DecodeRLE(data []byte) (int, int, []uint8, error) {
	const magic = "GTR1"
	if len(data) < 4 {
		return 0, 0, nil, &DecodeError{Code: CodeRLEShort, Format: "rle",
			Offset: len(data), Want: "魔数 'GTR1'", Got: -1,
			Detail: fmt.Sprintf("流只有 %d 字节", len(data))}
	}
	for k := 0; k < 4; k++ {
		if data[k] != magic[k] {
			return 0, 0, nil, &DecodeError{Code: CodeRLEMagic, Format: "rle",
				Offset: k, Want: fmt.Sprintf("'%c' (0x%02X)", magic[k], magic[k]), Got: int(data[k])}
		}
	}
	if len(data) < 8 {
		return 0, 0, nil, &DecodeError{Code: CodeRLEShort, Format: "rle",
			Offset: len(data), Want: "8 字节头（魔数 + 宽高）", Got: -1,
			Detail: fmt.Sprintf("流只有 %d 字节", len(data))}
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		off := 4
		if w > 0 {
			off = 6
		}
		return 0, 0, nil, &DecodeError{Code: CodeRLEDim, Format: "rle",
			Offset: off, Want: "正的画幅（uint16 小端）", Got: gotAt(data, off),
			Detail: fmt.Sprintf("解析得 %dx%d", w, h)}
	}
	if (len(data)-8)%2 != 0 {
		return 0, 0, nil, &DecodeError{Code: CodeRLEOddTail, Format: "rle",
			Offset: len(data) - 1, Want: "游程值字节（计数-值须成对）", Got: int(data[len(data)-1]),
			Detail: "尾部落单字节"}
	}
	pix := make([]uint8, 0, w*h)
	for i := 8; i+1 < len(data); i += 2 {
		n, v := int(data[i]), data[i+1]
		if len(pix)+n > w*h {
			return 0, 0, nil, &DecodeError{Code: CodeRLEOverrun, Format: "rle",
				Offset: i, Want: fmt.Sprintf("计数 ≤ %d（剩余画幅）", w*h-len(pix)), Got: n,
				Detail: fmt.Sprintf("游程总长将达 %d，画幅只要 %d", len(pix)+n, w*h)}
		}
		for k := 0; k < n; k++ {
			pix = append(pix, v)
		}
	}
	if len(pix) != w*h {
		return 0, 0, nil, &DecodeError{Code: CodeRLEPixShort, Format: "rle",
			Offset: len(data), Want: fmt.Sprintf("还需 %d 个像素的游程", w*h-len(pix)), Got: -1,
			Detail: fmt.Sprintf("游程总长 %d，画幅要 %d", len(pix), w*h)}
	}
	return w, h, pix, nil
}
