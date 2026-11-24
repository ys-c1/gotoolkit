package codec

import (
	"errors"
	"fmt"
)

// EncodePGM 输出 P5 灰度。
//
// 像素起始偏移 L 满足同余方程 L ≡ 0 (mod AlignUnit)。
// 头部名义长度 L0 = len("P5\n") + len(dims) + len("\n255\n")，填充 p = padFor(L0)
// 个空格插在 "P5\n" 之后。空白仍是头部的一部分（P5 语法允许各记号之间任意
// 空白），因此填充字节不会落到像素区，标准 P5 读取器读到的第一个像素仍是
// 源图第 0 个像素。
func EncodePGM(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	dims := fmt.Sprintf("%d %d", w, h)
	nominal := len("P5\n") + len(dims) + len("\n255\n") // 8 + len(dims)
	pad := padFor(nominal)
	out := make([]byte, 0, nominal+pad+w*h)
	out = append(out, "P5\n"...)
	for i := 0; i < pad; i++ {
		out = append(out, ' ')
	}
	out = append(out, dims...)
	out = append(out, "\n255\n"...)
	out = append(out, pix...)
	return out, nil
}

// DecodePGM 解析 P5 灰度。
//
// 返回的 Pix 直接切片自 data（零拷贝），与入参共享底层数组；调用方若需
// 独立所有权应自行复制。像素起点 = 头部长度，按构造为 AlignUnit 的整数倍，
// 因此无需再复制到对齐的中转缓冲。
func DecodePGM(data []byte) (int, int, []uint8, error) {
	if len(data) < 10 || data[0] != 'P' || data[1] != '5' {
		return 0, 0, nil, errors.New("不是 P5 流")
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
		return 0, 0, nil, errors.New("PGM 头非法")
	}
	// maxval 之后有且仅有一个空白字符作为分隔符（P5 规范），像素紧随其后。
	if i >= len(data) || !isSpace(data[i]) {
		return 0, 0, nil, errors.New("PGM 头缺分隔符")
	}
	i++
	if len(data)-i < w*h {
		return 0, 0, nil, errors.New("像素数据不足")
	}
	return w, h, data[i : i+w*h], nil
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t'
}
