package codec

import (
	"encoding/binary"
	"errors"
	"math"
)

// DeltaMagic 是相邻帧差分负载 GTD1 的文件头魔数。
const DeltaMagic = "GTD1"

// DeltaHeaderSize 为 magic(4)+宽(2)+高(2)+量化步长(2)。
const DeltaHeaderSize = 10

// QuantLevel 把整数差分 d 按步长 q 做最近邻量化，返回量化阶编号。
//
// 半档向远离 0 的方向取整，因此 |q*level-d| <= floor(q/2)。
// q<=1 时为恒等量化。整数运算避免浮点 tie 规则在不同语言间产生歧义。
func QuantLevel(d, q int) int {
	if q <= 1 {
		return d
	}
	if d < 0 {
		return -QuantLevel(-d, q)
	}
	return (d + q/2) / q
}

// EncodeDelta 编码一帧“与前一原始帧之差”的量化阶序列。
// 调用方负责按关键帧间隔决定何时调用；本函数只负责单帧负载。
func EncodeDelta(w, h, q int, delta []int) ([]byte, error) {
	if w <= 0 || h <= 0 || w > math.MaxUint16 || h > math.MaxUint16 || len(delta) != w*h {
		return nil, errors.New("差分画幅或像素数不匹配")
	}
	if q <= 0 || q > math.MaxUint16 {
		return nil, errors.New("量化步长非法")
	}
	out := make([]byte, DeltaHeaderSize, DeltaHeaderSize+2*len(delta))
	copy(out[0:4], DeltaMagic)
	binary.LittleEndian.PutUint16(out[4:6], uint16(w))
	binary.LittleEndian.PutUint16(out[6:8], uint16(h))
	binary.LittleEndian.PutUint16(out[8:10], uint16(q))
	for _, d := range delta {
		level := QuantLevel(d, q)
		if level < math.MinInt16 || level > math.MaxInt16 {
			return nil, errors.New("量化阶超出 int16 范围")
		}
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(int16(level)))
		out = append(out, b[0], b[1])
	}
	return out, nil
}

// DecodeDelta 解析 GTD1 差分负载，返回量化步长与量化阶。
// 反量化值为 level*q；与前一重建帧相加由序列解码层完成。
func DecodeDelta(data []byte) (w, h, q int, levels []int16, err error) {
	if len(data) < DeltaHeaderSize || string(data[0:4]) != DeltaMagic {
		return 0, 0, 0, nil, errors.New("不是 GTD1 差分负载")
	}
	w = int(binary.LittleEndian.Uint16(data[4:6]))
	h = int(binary.LittleEndian.Uint16(data[6:8]))
	q = int(binary.LittleEndian.Uint16(data[8:10]))
	if w <= 0 || h <= 0 || q <= 0 {
		return 0, 0, 0, nil, errors.New("差分头非法")
	}
	n := w * h
	if len(data) != DeltaHeaderSize+2*n {
		return 0, 0, 0, nil, errors.New("差分数据长度不足或有尾随字节")
	}
	levels = make([]int16, n)
	for i := range levels {
		levels[i] = int16(binary.LittleEndian.Uint16(data[DeltaHeaderSize+2*i:]))
	}
	return w, h, q, levels, nil
}

// IsDelta reports whether blob starts with the GTD1 delta magic.
func IsDelta(data []byte) bool {
	return len(data) >= 4 && string(data[0:4]) == DeltaMagic
}
