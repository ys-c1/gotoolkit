package codec

import (
	"errors"
	"fmt"
)

// EncodePGM 输出 P5 灰度（旧行为：紧凑、自上而下）。
func EncodePGM(w, h int, pix []uint8) ([]byte, error) {
	return EncodePGMLayout(w, h, pix, Layout{})
}

// EncodePGMLayout 按给定排布输出 P5 灰度。
// 行对齐作用于像素区；PGM 头是 ASCII，字节序标志对本格式没有可作用的整数，故不改变字节。
func EncodePGMLayout(w, h int, pix []uint8, l Layout) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	out := append([]byte(nil), fmt.Sprintf("P5\n%d %d\n255\n", w, h)...)
	out = append(out, PackRows(w, h, 1, pix, l)...)
	return out, nil
}

// DecodePGM 解析旧版 P5 灰度（紧凑、自上而下）。
func DecodePGM(data []byte) (int, int, []uint8, error) {
	return DecodePGMLayout(data, Layout{})
}

// DecodePGMLayout 按给定排布解析 P5 灰度。
func DecodePGMLayout(data []byte, l Layout) (int, int, []uint8, error) {
	if err := l.validate(); err != nil {
		return 0, 0, nil, err
	}
	if len(data) < 10 || data[0] != 'P' || data[1] != '5' {
		return 0, 0, nil, errors.New("不是 P5 流")
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
		return 0, 0, nil, errors.New("PGM 头非法")
	}
	i++ // 跳过分隔符
	s := l.Stride(w)
	if len(data)-i < s*h {
		return 0, 0, nil, errors.New("像素数据不足")
	}
	pix := UnpackRows(w, h, 1, data[i:i+s*h], l)
	if err := checkRowPad(data[i:], w, h, l); err != nil {
		return 0, 0, nil, err
	}
	return w, h, pix, nil
}
