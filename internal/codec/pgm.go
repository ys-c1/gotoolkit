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

// DecodePGM 解析 P5 灰度。失败时返回 *DecodeError，精确定位到字节。
func DecodePGM(data []byte) (int, int, []uint8, error) {
	if len(data) < 2 {
		return 0, 0, nil, &DecodeError{Code: CodePGMShort, Format: "pgm",
			Offset: len(data), Want: "魔数 'P5'", Got: -1,
			Detail: fmt.Sprintf("流只有 %d 字节", len(data))}
	}
	if data[0] != 'P' {
		return 0, 0, nil, &DecodeError{Code: CodePGMMagicP, Format: "pgm",
			Offset: 0, Want: "'P' (0x50)", Got: int(data[0])}
	}
	if data[1] != '5' {
		return 0, 0, nil, &DecodeError{Code: CodePGMMagic5, Format: "pgm",
			Offset: 1, Want: "'5' (0x35)", Got: int(data[1])}
	}
	i := 2
	next := func() (val, start int, ok bool) {
		for i < len(data) && (data[i] == ' ' || data[i] == '\n' || data[i] == '\r') {
			i++
		}
		start = i
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			val = val*10 + int(data[i]-'0')
			i++
		}
		return val, start, i > start
	}
	w, ws, ok1 := next()
	h, hs, ok2 := next()
	maxv, ms, ok3 := next()
	if !ok1 || !ok2 || !ok3 {
		code, off, name := CodePGMWidth, ws, "宽度"
		if ok1 {
			code, off, name = CodePGMHeight, hs, "高度"
		}
		if ok1 && ok2 {
			code, off, name = CodePGMMaxVal, ms, "maxval"
		}
		return 0, 0, nil, &DecodeError{Code: code, Format: "pgm",
			Offset: off, Want: name + "（十进制数字 0-9）", Got: gotAt(data, off)}
	}
	if w <= 0 || h <= 0 {
		code, off := CodePGMWidth, ws
		if w > 0 {
			code, off = CodePGMHeight, hs
		}
		return 0, 0, nil, &DecodeError{Code: code, Format: "pgm",
			Offset: off, Want: "正整数画幅", Got: gotAt(data, off),
			Detail: fmt.Sprintf("解析得 %dx%d", w, h)}
	}
	if maxv != 255 {
		// 逐位对比 "255"，定位到第一个不符的字节
		const want = "255"
		off, got := ms, -1
		k := 0
		for ; k < len(want); k++ {
			if gotAt(data, ms+k) != int(want[k]) {
				off, got = ms+k, gotAt(data, ms+k)
				break
			}
		}
		if k == len(want) { // 前三位全对，是多出的数字导致不等于 255
			off, got = ms+len(want), gotAt(data, ms+len(want))
		}
		wantByte := "maxval 前三位之外的结束分隔符"
		if off-ms < len(want) {
			wantByte = fmt.Sprintf("maxval 255 的对应字节 '%c'", want[off-ms])
		}
		return 0, 0, nil, &DecodeError{Code: CodePGMMaxVal, Format: "pgm",
			Offset: off, Want: wantByte,
			Got: got, Detail: fmt.Sprintf("解析得 maxval=%d", maxv)}
	}
	i++ // 跳过分隔符
	if len(data)-i < w*h {
		return 0, 0, nil, &DecodeError{Code: CodePGMPixShort, Format: "pgm",
			Offset: len(data), Want: fmt.Sprintf("第 %d 个像素字节", len(data)-i+1), Got: -1,
			Detail: fmt.Sprintf("画幅 %dx%d 需 %d 字节像素，流内仅剩 %d", w, h, w*h, len(data)-i)}
	}
	pix := make([]uint8, w*h)
	copy(pix, data[i:i+w*h])
	return w, h, pix, nil
}
