package codec

import (
	"encoding/binary"
	"errors"
	"io"
)

// bmpHeader 填好 54 字节文件头与信息头。
func bmpHeader(w, h int) []byte {
	rowSize := (w*3 + 3) &^ 3
	dataSize := rowSize * h
	total := 54 + dataSize
	out := make([]byte, 54)
	copy(out[0:], "BM")
	binary.LittleEndian.PutUint32(out[2:], uint32(total))
	binary.LittleEndian.PutUint32(out[10:], 54)
	binary.LittleEndian.PutUint32(out[14:], 40)
	binary.LittleEndian.PutUint32(out[18:], uint32(w))
	binary.LittleEndian.PutUint32(out[22:], uint32(h))
	binary.LittleEndian.PutUint16(out[26:], 1)
	binary.LittleEndian.PutUint16(out[28:], 24)
	binary.LittleEndian.PutUint32(out[34:], uint32(dataSize))
	return out
}

// EncodeBMP 输出 24 位灰度 BMP（三通道同值），行按 4 字节对齐，自底向上。
func EncodeBMP(w, h int, pix []uint8) ([]byte, error) {
	if err := checkSize(w, h, pix); err != nil {
		return nil, err
	}
	rowSize := (w*3 + 3) &^ 3
	out := make([]byte, 54+rowSize*h)
	copy(out, bmpHeader(w, h))
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

// EncodeBMPTo 把 BMP 逐行写入 w，只保留一行输出缓冲。
func EncodeBMPTo(dst io.Writer, w, h int, pix []uint8) error {
	if err := checkSize(w, h, pix); err != nil {
		return err
	}
	if _, err := dst.Write(bmpHeader(w, h)); err != nil {
		return err
	}
	rowSize := (w*3 + 3) &^ 3
	row := make([]byte, rowSize)
	for y := 0; y < h; y++ {
		srcRow := (h - 1 - y) * w
		for x := 0; x < w; x++ {
			v := pix[srcRow+x]
			row[x*3] = v
			row[x*3+1] = v
			row[x*3+2] = v
		}
		for i := w * 3; i < rowSize; i++ {
			row[i] = 0
		}
		if _, err := dst.Write(row); err != nil {
			return err
		}
	}
	return nil
}

// DecodeBMP 解析本仓库自己产出的 24 位 BMP。
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
