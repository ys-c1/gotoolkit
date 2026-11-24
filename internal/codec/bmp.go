package codec

import (
	"encoding/binary"
	"errors"
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

// EncodeBMPMasked 在 24 位 BMP 之后追加掩码尾段。
// BMP 像素保持三通道同值不变，透明信息只存在尾段里，解码可零误差还原原值。
func EncodeBMPMasked(w, h int, pix, alpha []uint8, maskValue uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h || len(alpha) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	body, err := EncodeBMP(w, h, pix)
	if err != nil {
		return nil, err
	}
	return append(body, buildMaskTrailer(maskValue, alpha)...), nil
}

// DecodeBMPMasked 解析带掩码尾段的 BMP，返回记录的掩码值、像素与 alpha（透明为 0）。
func DecodeBMPMasked(data []byte) (int, int, uint8, []uint8, []uint8, error) {
	w, h, pix, err := DecodeBMP(data)
	if err != nil {
		return 0, 0, 0, nil, nil, err
	}
	rowSize := (w*3 + 3) &^ 3
	off := int(binary.LittleEndian.Uint32(data[10:])) // 与 DecodeBMP 同一像素区偏移
	maskValue, alpha, err := parseMaskTrailer(data, off+rowSize*h, w*h)
	if err != nil {
		return 0, 0, 0, nil, nil, err
	}
	return w, h, maskValue, pix, alpha, nil
}
