package codec

// EncodePGM 输出 P5 灰度（头结构支路两开关全关，字节布局与原基准一致）。
func EncodePGM(w, h int, pix []uint8) ([]byte, error) {
	return EncodePGMEx(w, h, pix, PGMOptions{})
}

// DecodePGM 解析 P5 灰度（自动识别头结构支路，返回还原后的像素）。
func DecodePGM(data []byte) (int, int, []uint8, error) {
	w, h, pix, _, err := DecodePGMEx(data)
	return w, h, pix, err
}
