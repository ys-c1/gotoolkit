package codec

import (
	"encoding/binary"
	"errors"
)

// EncodeBMP 输出 24 位灰度 BMP（三通道同值），行按 4 字节对齐，自底向上。
//
// 像素数组起始偏移 off 满足 off ≡ 0 (mod AlignUnit)：
// 名义长度 L0 = 54（BITMAPFILEHEADER 14 + BITMAPINFOHEADER 40），
// 填充 p = padFor(54) 个 0x00 字节插在 54 字节头之后，
// off = L0 + p 写入文件头 bfOffBits（偏移 10）。bfOffBits 就是规范给出的
// 像素数组起点，读取器必须按它定位，因此填充字节不会被当作像素。
// 行内填充仍按 BMP 既有规则（rowSize = (3w+3) &^ 3），不属于本次新增填充。
func EncodeBMP(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	const headerLen = 54
	off := headerLen + padFor(headerLen)
	rowSize := (w*3 + 3) &^ 3
	dataSize := rowSize * h
	total := off + dataSize
	out := make([]byte, total)
	copy(out[0:], "BM")
	binary.LittleEndian.PutUint32(out[2:], uint32(total))
	binary.LittleEndian.PutUint32(out[10:], uint32(off))
	binary.LittleEndian.PutUint32(out[14:], 40)
	binary.LittleEndian.PutUint32(out[18:], uint32(w))
	binary.LittleEndian.PutUint32(out[22:], uint32(h))
	binary.LittleEndian.PutUint16(out[26:], 1)
	binary.LittleEndian.PutUint16(out[28:], 24)
	binary.LittleEndian.PutUint32(out[34:], uint32(dataSize))
	for y := 0; y < h; y++ {
		srcRow := (h - 1 - y) * w
		base := off + y*rowSize
		for x := 0; x < w; x++ {
			v := pix[srcRow+x]
			out[base+x*3] = v
			out[base+x*3+1] = v
			out[base+x*3+2] = v
		}
	}
	return out, nil
}

// DecodeBMP 解析本仓库自己产出的 24 位 BMP。
//
// 像素数组按文件头 bfOffBits 定位，不再假设固定 54 字节。
func DecodeBMP(data []byte) (int, int, []uint8, error) {
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
	if off < 54 || off > len(data) {
		return 0, 0, nil, errors.New("BMP 像素偏移非法")
	}
	// 行内填充由画幅唯一确定，且必须与 BMP 既定规则一致。
	rowSize := (w*3 + 3) &^ 3
	if len(data) < off+rowSize*h {
		return 0, 0, nil, errors.New("像素数据不足")
	}
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		dstRow := (h - 1 - y) * w
		base := off + y*rowSize
		for x := 0; x < w; x++ {
			pix[dstRow+x] = data[base+x*3]
		}
	}
	return w, h, pix, nil
}
