package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// EncodeBMP 输出 24 位灰度 BMP（三通道同值），行按 4 字节对齐，自底向上。
func EncodeBMP(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	rowSize := (w*3 + 3) &^ 3
	dataSize := rowSize * h
	total := 54 + dataSize
	out := make([]byte, total)
	copy(out[0:], "BM")
	binary.LittleEndian.PutUint32(out[2:], uint32(total))
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
			v := pix[srcRow+x]
			out[base+x*3] = v
			out[base+x*3+1] = v
			out[base+x*3+2] = v
		}
	}
	return out, nil
}

// DecodeBMP 解析本仓库自己产出的 24 位 BMP。失败时返回 *DecodeError，精确定位到字节。
func DecodeBMP(data []byte) (int, int, []uint8, error) {
	if len(data) < 2 {
		return 0, 0, nil, &DecodeError{Code: CodeBMPShort, Format: "bmp",
			Offset: len(data), Want: "魔数 'BM'", Got: -1,
			Detail: fmt.Sprintf("流只有 %d 字节", len(data))}
	}
	if data[0] != 'B' {
		return 0, 0, nil, &DecodeError{Code: CodeBMPMagic, Format: "bmp",
			Offset: 0, Want: "'B' (0x42)", Got: int(data[0])}
	}
	if data[1] != 'M' {
		return 0, 0, nil, &DecodeError{Code: CodeBMPMagic, Format: "bmp",
			Offset: 1, Want: "'M' (0x4D)", Got: int(data[1])}
	}
	if len(data) < 54 {
		return 0, 0, nil, &DecodeError{Code: CodeBMPShort, Format: "bmp",
			Offset: len(data), Want: "54 字节 BMP 头", Got: -1,
			Detail: fmt.Sprintf("流只有 %d 字节", len(data))}
	}
	w := int(int32(binary.LittleEndian.Uint32(data[18:])))
	h := int(int32(binary.LittleEndian.Uint32(data[22:])))
	bpp := int(binary.LittleEndian.Uint16(data[28:]))
	off := int(binary.LittleEndian.Uint32(data[10:]))
	if w <= 0 || h <= 0 {
		off18 := 18
		if w > 0 {
			off18 = 22
		}
		return 0, 0, nil, &DecodeError{Code: CodeBMPDim, Format: "bmp",
			Offset: off18, Want: "正的画幅（int32 小端）", Got: gotAt(data, off18),
			Detail: fmt.Sprintf("解析得 %dx%d", w, h)}
	}
	if bpp != 24 {
		boff := 28 // 低字节应为 0x18
		if data[28] == 24 {
			boff = 29 // 低字节对，则高字节应为 0x00
		}
		return 0, 0, nil, &DecodeError{Code: CodeBMPBpp, Format: "bmp",
			Offset: boff, Want: "bpp=24（偏移 28 为 0x18，偏移 29 为 0x00）", Got: int(data[boff]),
			Detail: fmt.Sprintf("解析得 bpp=%d", bpp)}
	}
	rowSize := (w*3 + 3) &^ 3
	if len(data) < off+rowSize*h {
		return 0, 0, nil, &DecodeError{Code: CodeBMPPixShort, Format: "bmp",
			Offset: len(data), Want: fmt.Sprintf("第 %d 个像素数据字节", len(data)-off+1), Got: -1,
			Detail: fmt.Sprintf("像素区起于偏移 %d，需 %d 字节，流内仅剩 %d", off, rowSize*h, len(data)-off)}
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
