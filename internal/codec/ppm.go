package codec

import (
	"errors"
	"fmt"
)

// EncodePPM 输出 P6 二进制彩色 PPM（每像素 R,G,B 三字节，行无填充）。
// 与 PGM 同属 netpbm，头部为 ASCII、空格外加单个空白分隔。
func EncodePPM(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h*3 {
		return nil, errors.New("画幅或像素数不匹配")
	}
	out := make([]byte, 0, 15+w*h*3)
	out = append(out, []byte(fmt.Sprintf("P6\n%d %d\n255\n", w, h))...)
	out = append(out, pix...)
	return out, nil
}

// DecodePPM 解析本仓库自己产出的 P6 PPM。
func DecodePPM(data []byte) (int, int, []uint8, error) {
	if len(data) < 10 || data[0] != 'P' || data[1] != '6' {
		return 0, 0, nil, errors.New("不是 P6 流")
	}
	i := 2
	next := func() (int, bool) {
		for i < len(data) && (data[i] == ' ' || data[i] == '\n' || data[i] == '\r' || data[i] == '\t') {
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
		return 0, 0, nil, errors.New("PPM 头非法")
	}
	i++ // 跳过分隔符（maxval 后恰好一个空白）
	if len(data)-i < w*h*3 {
		return 0, 0, nil, errors.New("像素数据不足")
	}
	pix := make([]uint8, w*h*3)
	copy(pix, data[i:i+w*h*3])
	return w, h, pix, nil
}
