package codec

import (
	"errors"
	"fmt"
)

// EncodePGM 输出 P5 灰度。
func EncodePGM(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	out := make([]byte, 0, 15+w*h)
	out = append(out, []byte(fmt.Sprintf("P5\n%d %d\n255\n", w, h))...)
	out = append(out, pix...)
	return out, nil
}

// DecodePGM 解析 P5 灰度。
func DecodePGM(data []byte) (int, int, []uint8, error) {
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
	if len(data)-i < w*h {
		return 0, 0, nil, errors.New("像素数据不足")
	}
	pix := make([]uint8, w*h)
	copy(pix, data[i:i+w*h])
	return w, h, pix, nil
}
