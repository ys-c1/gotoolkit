package codec

import (
	"encoding/binary"
	"errors"
)

// EncodeRLE 输出自定义游程格式：GTR1 + 宽高 + (计数, 值) 序列。
// 等价于 EncodeRLEChunked(w, h, pix, 0)。
func EncodeRLE(w, h int, pix []uint8) ([]byte, error) {
	return EncodeRLEChunked(w, h, pix, 0)
}

// EncodeRLEChunked 输出分块游程格式。
// chunk <= 0 时不分块，输出与 EncodeRLE 逐字节一致（GTR1）。
// chunk > 0 时输出 GTR2：GTR2 + 宽高 + 块长 + 每块 [u16 负载长 + (计数, 值) 序列]，
// 游程不跨块界，块头固定 2 字节。
func EncodeRLEChunked(w, h int, pix []uint8, chunk int) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if chunk < 0 || chunk > 0xffff {
		return nil, errors.New("块长越界")
	}
	if chunk == 0 {
		out := make([]byte, 8, 8+len(pix)*2)
		copy(out[0:], "GTR1")
		binary.LittleEndian.PutUint16(out[4:], uint16(w))
		binary.LittleEndian.PutUint16(out[6:], uint16(h))
		return append(out, rleRuns(pix)...), nil
	}
	out := make([]byte, 10, 10+len(pix)*2)
	copy(out[0:], "GTR2")
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	binary.LittleEndian.PutUint16(out[8:], uint16(chunk))
	for start := 0; start < len(pix); start += chunk {
		end := start + chunk
		if end > len(pix) {
			end = len(pix)
		}
		body := rleRuns(pix[start:end])
		var hdr [2]byte
		binary.LittleEndian.PutUint16(hdr[:], uint16(len(body)))
		out = append(out, hdr[:]...)
		out = append(out, body...)
	}
	return out, nil
}

// rleRuns 把一段像素编成 (计数, 值) 序列，计数上限 255。
func rleRuns(pix []uint8) []byte {
	out := make([]byte, 0, len(pix))
	i := 0
	for i < len(pix) {
		j := i
		for j < len(pix) && pix[j] == pix[i] && j-i < 255 {
			j++
		}
		out = append(out, uint8(j-i), pix[i])
		i = j
	}
	return out
}

// DecodeRLE 解析游程格式，同时接受 GTR1（不分块）与 GTR2（分块）。
func DecodeRLE(data []byte) (int, int, []uint8, error) {
	if len(data) < 8 || string(data[0:3]) != "GTR" {
		return 0, 0, nil, errors.New("不是 GTR1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	pix := make([]uint8, 0, w*h)
	appendRun := func(n, v byte) error {
		for k := 0; k < int(n); k++ {
			pix = append(pix, v)
		}
		if len(pix) > w*h {
			return errors.New("游程超出画幅")
		}
		return nil
	}
	switch data[3] {
	case '1':
		for i := 8; i+1 < len(data); i += 2 {
			if err := appendRun(data[i], data[i+1]); err != nil {
				return 0, 0, nil, err
			}
		}
	case '2':
		if len(data) < 10 {
			return 0, 0, nil, errors.New("GTR2 头被截断")
		}
		i := 10
		for i < len(data) {
			if i+2 > len(data) {
				return 0, 0, nil, errors.New("块头被截断")
			}
			plen := int(binary.LittleEndian.Uint16(data[i:]))
			i += 2
			if i+plen > len(data) {
				return 0, 0, nil, errors.New("块负载被截断")
			}
			for j := i; j+1 < i+plen; j += 2 {
				if err := appendRun(data[j], data[j+1]); err != nil {
					return 0, 0, nil, err
				}
			}
			i += plen
		}
	default:
		return 0, 0, nil, errors.New("不是 GTR1 流")
	}
	if len(pix) != w*h {
		return 0, 0, nil, errors.New("像素数不足")
	}
	return w, h, pix, nil
}
