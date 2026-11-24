package codec

import (
	"encoding/binary"
	"errors"
)

// EncodeIDX 输出调色板索引格式 GTID：
//
//	magic(4)="GTID" + 宽(2) + 高(2) + 桶数(1) + 调色板(buckets 字节) + 索引(W*H 字节)
//
// 索引每像素一项，不做去重、不做游程合并。
func EncodeIDX(w, h int, palette, index []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(index) != w*h {
		return nil, errors.New("画幅或索引数不匹配")
	}
	if len(palette) == 0 || len(palette) > 255 {
		return nil, errors.New("调色板桶数必须在 1..255")
	}
	for _, k := range index {
		if int(k) >= len(palette) {
			return nil, errors.New("索引超出调色板范围")
		}
	}
	out := make([]byte, 0, 9+len(palette)+w*h)
	out = append(out, 'G', 'T', 'I', 'D')
	out = binary.LittleEndian.AppendUint16(out, uint16(w))
	out = binary.LittleEndian.AppendUint16(out, uint16(h))
	out = append(out, uint8(len(palette)))
	out = append(out, palette...)
	out = append(out, index...)
	return out, nil
}

// DecodeIDX 解析 GTID，并按调色板回填出灰度像素。
func DecodeIDX(data []byte) (int, int, []uint8, error) {
	if len(data) < 9 || string(data[0:4]) != "GTID" {
		return 0, 0, nil, errors.New("不是 GTID 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	buckets := int(data[8])
	if w <= 0 || h <= 0 || buckets == 0 {
		return 0, 0, nil, errors.New("GTID 头非法")
	}
	palEnd := 9 + buckets
	if len(data) < palEnd+w*h {
		return 0, 0, nil, errors.New("索引数据不足")
	}
	palette := data[9:palEnd]
	pix := make([]uint8, w*h)
	for i, k := range data[palEnd : palEnd+w*h] {
		if int(k) >= buckets {
			return 0, 0, nil, errors.New("索引超出调色板范围")
		}
		pix[i] = palette[k]
	}
	return w, h, pix, nil
}
