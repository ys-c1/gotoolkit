package codec

import (
	"encoding/binary"
	"errors"
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

// DecodeRLE 解析游程格式。
func DecodeRLE(data []byte) (int, int, []uint8, error) {
	w, h, pix, _, err := decodeRLE(data)
	return w, h, pix, err
}

// decodeRLE 额外返回载荷结束偏移（掩码尾段紧接其后），供掩码路径定位。
func decodeRLE(data []byte) (int, int, []uint8, int, error) {
	if len(data) < 8 || string(data[0:4]) != "GTR1" {
		return 0, 0, nil, 0, errors.New("不是 GTR1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, 0, errors.New("画幅非法")
	}
	pix := make([]uint8, 0, w*h)
	end := len(data)
	for i := 8; i+1 < len(data); i += 2 {
		n, v := int(data[i]), data[i+1]
		for k := 0; k < n; k++ {
			pix = append(pix, v)
		}
		if len(pix) > w*h {
			return 0, 0, nil, 0, errors.New("游程超出画幅")
		}
		// 凑满画幅即停：合法流的游程计数之和恰为 w*h，
		// 因此掩码尾段（若有）天然不会被当成游程读取。
		if len(pix) == w*h {
			end = i + 2
			break
		}
	}
	if len(pix) != w*h {
		return 0, 0, nil, 0, errors.New("像素数不足")
	}
	return w, h, pix, end, nil
}

// rlePayloadLen 重算指定像素的游程序列长度（含 8 字节头）。
func rlePayloadLen(pix []uint8) int {
	n := 8
	for i := 0; i < len(pix); {
		j := i
		for j < len(pix) && pix[j] == pix[i] && j-i < 255 {
			j++
		}
		n += 2
		i = j
	}
	return n
}

// EncodeRLEMasked 在 GTR1 游程之后追加掩码尾段。
// 游程承载的原始灰度值不变，透明信息只存在尾段里，解码可零误差还原原值。
func EncodeRLEMasked(w, h int, pix, alpha []uint8, maskValues []uint8) ([]byte, error) {
	if err := checkMaskArgs(w, h, pix, alpha, maskValues); err != nil {
		return nil, err
	}
	body, err := EncodeRLE(w, h, pix)
	if err != nil {
		return nil, err
	}
	return append(body, buildMaskTrailer(maskValues, alpha)...), nil
}

// DecodeRLEMasked 解析带掩码尾段的 GTR1 流，返回记录的掩码值、像素与 alpha。
func DecodeRLEMasked(data []byte) (int, int, []uint8, []uint8, []uint8, error) {
	w, h, pix, end, err := decodeRLE(data)
	if err != nil {
		return 0, 0, nil, nil, nil, err
	}
	vals, alpha, err := parseMaskTrailer(data, end, w*h)
	if err != nil {
		return 0, 0, nil, nil, nil, err
	}
	return w, h, vals, pix, alpha, nil
}
