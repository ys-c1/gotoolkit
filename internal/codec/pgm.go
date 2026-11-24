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
	w, h, pix, _, err := decodePGM(data)
	return w, h, pix, err
}

// decodePGM 在 DecodePGM 基础上额外返回像素区起始偏移（即 PGM 头长度）。
func decodePGM(data []byte) (int, int, []uint8, int, error) {
	if len(data) < 10 || data[0] != 'P' || data[1] != '5' {
		return 0, 0, nil, 0, errors.New("不是 P5 流")
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
		return 0, 0, nil, 0, errors.New("PGM 头非法")
	}
	i++ // 跳过分隔符
	if len(data)-i < w*h {
		return 0, 0, nil, 0, errors.New("像素数据不足")
	}
	pix := make([]uint8, w*h)
	copy(pix, data[i:i+w*h])
	return w, h, pix, i, nil
}

// EncodePGMMasked 在 P5 灰度之后追加掩码尾段。
// 像素字节原样写出，透明信息只存在尾段里，因此解码可零误差还原原值。
func EncodePGMMasked(w, h int, pix, alpha []uint8, maskValues []uint8) ([]byte, error) {
	if err := checkMaskArgs(w, h, pix, alpha, maskValues); err != nil {
		return nil, err
	}
	body, err := EncodePGM(w, h, pix)
	if err != nil {
		return nil, err
	}
	return append(body, buildMaskTrailer(maskValues, alpha)...), nil
}

// DecodePGMMasked 解析带掩码尾段的 P5 灰度，返回记录的掩码值、像素与 alpha。
func DecodePGMMasked(data []byte) (int, int, []uint8, []uint8, []uint8, error) {
	w, h, pix, pixStart, err := decodePGM(data)
	if err != nil {
		return 0, 0, nil, nil, nil, err
	}
	vals, alpha, err := parseMaskTrailer(data, pixStart+w*h, w*h)
	if err != nil {
		return 0, 0, nil, nil, nil, err
	}
	return w, h, vals, pix, alpha, nil
}
