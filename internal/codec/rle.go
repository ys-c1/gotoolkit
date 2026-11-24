package codec

import (
	"encoding/binary"
	"errors"
	"io"
)

// rleHeader 输出 GTR1 + 宽高。
func rleHeader(w, h int) []byte {
	out := make([]byte, 8)
	copy(out[0:], "GTR1")
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	return out
}

// EncodeRLE 输出自定义游程格式：GTR1 + 宽高 + (计数, 值) 序列。
func EncodeRLE(w, h int, pix []uint8) ([]byte, error) {
	if err := checkSize(w, h, pix); err != nil {
		return nil, err
	}
	out := make([]byte, 8, 8+len(pix)*2)
	copy(out, rleHeader(w, h))
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

// EncodeRLETo 把游程逐段写入 w，不保留整块输出缓冲。
func EncodeRLETo(dst io.Writer, w, h int, pix []uint8) error {
	if err := checkSize(w, h, pix); err != nil {
		return err
	}
	if _, err := dst.Write(rleHeader(w, h)); err != nil {
		return err
	}
	var pair [2]byte
	i := 0
	for i < len(pix) {
		j := i
		for j < len(pix) && pix[j] == pix[i] && j-i < 255 {
			j++
		}
		pair[0], pair[1] = uint8(j-i), pix[i]
		if _, err := dst.Write(pair[:]); err != nil {
			return err
		}
		i = j
	}
	return nil
}

// DecodeRLE 解析游程格式。
func DecodeRLE(data []byte) (int, int, []uint8, error) {
	if len(data) < 8 || string(data[0:4]) != "GTR1" {
		return 0, 0, nil, errors.New("不是 GTR1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	pix := make([]uint8, 0, w*h)
	for i := 8; i+1 < len(data); i += 2 {
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
