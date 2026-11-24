package codec

import "errors"

// EncodeRLE 输出自定义游程格式：GTR1 + 宽高 + (计数, 值) 序列（旧行为：小端、自上而下、紧凑）。
func EncodeRLE(w, h int, pix []uint8) ([]byte, error) {
	return EncodeRLELayout(w, h, pix, Layout{})
}

// EncodeRLELayout 按给定排布输出游程格式。
//   - 行序：游程按存储行顺序展开；BottomUp 时先展开最后一行。
//   - 字节序：头里的宽、高按标志落字节。
//   - 行对齐：每个存储行在展开成字节前补齐到边框，补位为 0；
//     由于游程本身不带行边界，解码侧按 Stride 逐行还原，再丢掉补位。
func EncodeRLELayout(w, h int, pix []uint8, l Layout) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	out := make([]byte, 8, 8+len(pix)*2)
	copy(out[0:], "GTR1")
	put16(out, 4, uint16(w), l.BigEndian)
	put16(out, 6, uint16(h), l.BigEndian)
	stream := PackRows(w, h, 1, pix, l)
	i := 0
	for i < len(stream) {
		j := i
		for j < len(stream) && stream[j] == stream[i] && j-i < 255 {
			j++
		}
		out = append(out, uint8(j-i), stream[i])
		i = j
	}
	return out, nil
}

// DecodeRLE 解析旧版游程格式（小端、自上而下、紧凑）。
func DecodeRLE(data []byte) (int, int, []uint8, error) {
	return DecodeRLELayout(data, Layout{})
}

// DecodeRLELayout 按给定排布解析游程格式。
func DecodeRLELayout(data []byte, l Layout) (int, int, []uint8, error) {
	if err := l.validate(); err != nil {
		return 0, 0, nil, err
	}
	if len(data) < 8 || string(data[0:4]) != "GTR1" {
		return 0, 0, nil, errors.New("不是 GTR1 流")
	}
	w := int(get16(data, 4, l.BigEndian))
	h := int(get16(data, 6, l.BigEndian))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	rowBytes := w
	s := l.Stride(rowBytes)
	want := s * h
	raw := make([]uint8, 0, want)
	for i := 8; i+1 < len(data); i += 2 {
		n, v := int(data[i]), data[i+1]
		for k := 0; k < n; k++ {
			raw = append(raw, v)
		}
		if len(raw) > want {
			return 0, 0, nil, errors.New("游程超出画幅")
		}
	}
	if len(raw) != want {
		return 0, 0, nil, errors.New("像素数不足")
	}
	if err := checkRowPad(raw, rowBytes, h, l); err != nil {
		return 0, 0, nil, err
	}
	pix := UnpackRows(w, h, 1, raw, l)
	return w, h, pix, nil
}
