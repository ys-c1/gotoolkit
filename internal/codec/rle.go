package codec

import (
	"encoding/binary"
	"errors"
)

// DefaultRLECap 是单条游程计数字段（1 字节）的默认上限。
const DefaultRLECap = 255

// EncodeRLE 输出自定义游程格式：GTR1 + 宽高 + (计数, 值) 序列。
// 单条游程最多容纳 255 个像素，与基准实现保持一致。
func EncodeRLE(w, h int, pix []uint8) ([]byte, error) {
	return EncodeRLEWithCap(w, h, pix, DefaultRLECap)
}

// EncodeRLEWithCap 按给定单游程上限 maxRun（1..255）做贪心最长匹配编码。
//
// 贪心最优性：把像素流切成极大同值段，段内每条游程是 1..maxRun 个同值像素，
// 且每条游程恒为 2 字节（计数+值），代价与游程长度无关。长度为 L 的段至少
// 需要 ceil(L/maxRun) 条游程（每条最多装 maxRun 个）；贪心每次取
// min(maxRun, 剩余长度) 恰好达到该下界，因此在任何上限下贪心最长匹配都取得
// 最少游程数与最小体积。
func EncodeRLEWithCap(w, h int, pix []uint8, maxRun int) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if maxRun <= 0 || maxRun > 255 {
		return nil, errors.New("游程上限越界")
	}
	out := make([]byte, 8, 8+len(pix)*2)
	copy(out[0:], "GTR1")
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	i := 0
	for i < len(pix) {
		j := i
		// 贪心最长匹配：同值且不超过单游程上限，一次取到不能再取为止。
		for j < len(pix) && pix[j] == pix[i] && j-i < maxRun {
			j++
		}
		out = append(out, uint8(j-i), pix[i])
		i = j
	}
	return out, nil
}

// DecodeRLE 解析游程格式。
func DecodeRLE(data []byte) (int, int, []uint8, error) {
	if len(data) < 8 || string(data[0:4]) != "GTR1" {
		return 0, 0, nil, errors.New("不是 GTR1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	pix := make([]uint8, 0, w*h)
	for i := 8; i+1 < len(data); i += 2 {
		n, v := int(data[i]), data[i+1]
		for k := 0; k < n; k++ {
			pix = append(pix, v)
		}
		if len(pix) > w*h {
			return 0, 0, nil, errors.New("游程超出画幅")
		}
	}
	if len(pix) != w*h {
		return 0, 0, nil, errors.New("像素数不足")
	}
	return w, h, pix, nil
}
