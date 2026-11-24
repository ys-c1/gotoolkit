package codec

import "errors"

// EncodeBMP 输出 24 位灰度 BMP（三通道同值），4 字节对齐、自底向上、小端（旧行为）。
func EncodeBMP(w, h int, pix []uint8) ([]byte, error) {
	return EncodeBMPLayout(w, h, pix, Layout{BottomUp: true, Align: 4})
}

// EncodeBMPLayout 按给定排布输出 24 位灰度 BMP。
// 三个标志在本格式里都真正改变字节：
//   - 行序同时决定 bfHeight 的符号（正 = 自底向上，负 = 自上而下）与行写入顺序；
//   - 字节序决定 54 字节头里每个多字节整数的落字节顺序；
//   - 行对齐决定每行跨度，以及 bfSize / biSizeImage。
func EncodeBMPLayout(w, h int, pix []uint8, l Layout) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	rowSize := l.Stride(w * 3)
	dataSize := rowSize * h
	total := 54 + dataSize
	out := make([]byte, total)
	copy(out[0:], "BM")
	big := l.BigEndian
	put32(out, 2, uint32(total), big)
	put32(out, 10, 54, big)
	put32(out, 14, 40, big)
	put32(out, 18, uint32(w), big)
	// BMP 规范：正高度 = 自底向上存储，负高度 = 自上而下存储。
	height := int32(h)
	if !l.BottomUp {
		height = -height
	}
	put32(out, 22, uint32(height), big)
	put16(out, 26, 1, big)
	put16(out, 28, 24, big)
	put32(out, 34, uint32(dataSize), big)
	for y := 0; y < h; y++ {
		srcRow := l.LogicalRow(h, y) * w
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

// DecodeBMP 解析旧版 24 位 BMP（4 字节对齐、自底向上、小端）。
func DecodeBMP(data []byte) (int, int, []uint8, error) {
	return DecodeBMPLayout(data, Layout{BottomUp: true, Align: 4})
}

// DecodeBMPLayout 按给定排布解析 24 位灰度 BMP。
// 头里的 bfHeight 符号是文件自带的行序声明；调用方给的 BottomUp 必须与之一致，
// 否则返回错误并指名“行序”标志，而不是静默按一端解释。
func DecodeBMPLayout(data []byte, l Layout) (int, int, []uint8, error) {
	if err := l.validate(); err != nil {
		return 0, 0, nil, err
	}
	if len(data) < 54 || string(data[0:2]) != "BM" {
		return 0, 0, nil, errors.New("不是 BMP 流")
	}
	big := l.BigEndian
	w := int(int32(get32(data, 18, big)))
	rawH := int32(get32(data, 22, big))
	bpp := int(get16(data, 28, big))
	off := int(get32(data, 10, big))
	if w <= 0 || rawH == 0 || bpp != 24 {
		return 0, 0, nil, errors.New("BMP 头非法（检查字节序标志：头字段无法按声明解码）")
	}
	fileBottomUp := rawH > 0
	if fileBottomUp != l.BottomUp {
		return 0, 0, nil, errors.New("行序标志与 BMP 头高度符号不一致（行序标志未生效或被错误指定）")
	}
	h := int(rawH)
	if h < 0 {
		h = -h
	}
	rowSize := l.Stride(w * 3)
	if len(data) < off+rowSize*h {
		return 0, 0, nil, errors.New("像素数据不足（检查行对齐标志）")
	}
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		dstRow := l.LogicalRow(h, y) * w
		base := off + y*rowSize
		for x := 0; x < w; x++ {
			pix[dstRow+x] = data[base+x*3]
		}
	}
	if err := checkRowPad(data[off:], w*3, h, l); err != nil {
		return 0, 0, nil, err
	}
	return w, h, pix, nil
}
