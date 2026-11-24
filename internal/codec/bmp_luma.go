package codec

import (
	"encoding/binary"
	"errors"

	"gotoolkit/internal/expand"
)

// lumaMark 写进 BMP 的 biReserved（偏移 6..9，共 4 字节）。正常 24 位
// BMP 这个字段恒为 0；带上它就能在解码时无须外部参数区分「三通道同值
// 的旧灰度 BMP」与「按亮度守恒展开的 BMP」。头长度不变，仍是 54 字节。
const lumaMark = "LUM1"

// IsLumaBMP 报告一份 BMP 是否带有亮度展开标记。
func IsLumaBMP(data []byte) bool {
	return len(data) >= 10 && string(data[6:10]) == lumaMark
}

// EncodeBMPLuma 输出按亮度守恒展开的 24 位 BMP：
//
//	R,G,B = Expand(灰度)，见 internal/expand。
//
// 与 EncodeBMP 的唯一差别是每像素三个通道不再同值；头字段、行对齐、
// 自底向上的排列完全一致，因此两种输出的总字节数恒等。
func EncodeBMPLuma(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	rowSize := (w*3 + 3) &^ 3
	dataSize := rowSize * h
	total := 54 + dataSize
	out := make([]byte, total)
	copy(out[0:], "BM")
	binary.LittleEndian.PutUint32(out[2:], uint32(total))
	copy(out[6:], lumaMark)
	binary.LittleEndian.PutUint32(out[10:], 54)
	binary.LittleEndian.PutUint32(out[14:], 40)
	binary.LittleEndian.PutUint32(out[18:], uint32(w))
	binary.LittleEndian.PutUint32(out[22:], uint32(h))
	binary.LittleEndian.PutUint16(out[26:], 1)
	binary.LittleEndian.PutUint16(out[28:], 24)
	binary.LittleEndian.PutUint32(out[34:], uint32(dataSize))
	for y := 0; y < h; y++ {
		srcRow := (h - 1 - y) * w
		base := 54 + y*rowSize
		for x := 0; x < w; x++ {
			r, g, b, _ := expand.Expand(pix[srcRow+x])
			out[base+x*3] = r
			out[base+x*3+1] = g
			out[base+x*3+2] = b
		}
	}
	return out, nil
}

// DecodeBMPLuma 解析带 lumaMark 的 BMP，并按亮度权重把三通道收回灰度。
// 不带标记的输入报错，调用方应改用 DecodeBMP。
func DecodeBMPLuma(data []byte) (int, int, []uint8, error) {
	if !IsLumaBMP(data) {
		return 0, 0, nil, errors.New("不是亮度展开 BMP")
	}
	if len(data) < 54 || string(data[0:2]) != "BM" {
		return 0, 0, nil, errors.New("不是 BMP 流")
	}
	w := int(int32(binary.LittleEndian.Uint32(data[18:])))
	h := int(int32(binary.LittleEndian.Uint32(data[22:])))
	bpp := int(binary.LittleEndian.Uint16(data[28:]))
	off := int(binary.LittleEndian.Uint32(data[10:]))
	if w <= 0 || h <= 0 || bpp != 24 {
		return 0, 0, nil, errors.New("BMP 头非法")
	}
	rowSize := (w*3 + 3) &^ 3
	if off < 0 || len(data) < off+rowSize*h {
		return 0, 0, nil, errors.New("像素数据不足")
	}
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		dstRow := (h - 1 - y) * w
		base := off + y*rowSize
		for x := 0; x < w; x++ {
			pix[dstRow+x] = expand.Collapse(
				data[base+x*3], data[base+x*3+1], data[base+x*3+2])
		}
	}
	return w, h, pix, nil
}

// DecodeBMPAny 按 biReserved 标记分派：带 lumaMark 走展开解码，否则走
// 原灰度路径 DecodeBMP。旧灰度 BMP 的字节流与返回值完全不受影响。
func DecodeBMPAny(data []byte) (int, int, []uint8, error) {
	if IsLumaBMP(data) {
		return DecodeBMPLuma(data)
	}
	return DecodeBMP(data)
}
