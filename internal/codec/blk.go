package codec

import (
	"encoding/binary"
	"errors"
)

// DefaultBlockLen 是块压缩默认块长（边长，像素）。
// 取 8：8×8 方块是常见折中档；必须同时整除画幅宽高。
const DefaultBlockLen = 8

// EncodeBLK 输出自定义块均值格式 GTB1。
//
// 流布局：GTB1 + 宽(2) + 高(2) + 块长 L(1) + 各 L×L 块的均值（uint8）。
// 每块只存一个像素（块均值，四舍五入到 [0,255]），解码时整块回填，
// 因此是有损压缩：幅 w×h 共 (w/L)*(h/L) 个块、同样多的输出字节。
// 要求 L 同时整除 w 与 h，否则画幅无法被整块铺满，直接报错。
func EncodeBLK(w, h, L int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if L <= 0 || L > 255 {
		return nil, errors.New("块长必须在 [1,255]")
	}
	if w%L != 0 || h%L != 0 {
		return nil, errors.New("块长必须同时整除画幅宽高")
	}
	nx, ny := w/L, h/L
	out := make([]byte, 0, 9+nx*ny)
	out = append(out, []byte("GTB1")...)
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint16(hdr[0:], uint16(w))
	binary.LittleEndian.PutUint16(hdr[2:], uint16(h))
	out = append(out, hdr...)
	out = append(out, uint8(L))
	for by := 0; by < ny; by++ {
		for bx := 0; bx < nx; bx++ {
			var sum int
			for y := by * L; y < (by+1)*L; y++ {
				row := y * w
				for x := bx * L; x < (bx+1)*L; x++ {
					sum += int(pix[row+x])
				}
			}
			// 块长平方最大 255²=65025，和最大 65025*255 仍在 int 范围内。
			mean := (sum + L*L/2) / (L * L) // 四舍五入
			out = append(out, uint8(mean))
		}
	}
	return out, nil
}

// DecodeBLK 解析块均值格式，把每块的均值回填到整块像素。
func DecodeBLK(data []byte) (int, int, int, []uint8, error) {
	if len(data) < 10 || string(data[0:4]) != "GTB1" {
		return 0, 0, 0, nil, errors.New("不是 GTB1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	L := int(data[8])
	if w <= 0 || h <= 0 || L <= 0 {
		return 0, 0, 0, nil, errors.New("画幅或块长非法")
	}
	if w%L != 0 || h%L != 0 {
		return 0, 0, 0, nil, errors.New("块长与画幅不整除")
	}
	nx, ny := w/L, h/L
	if len(data)-9 != nx*ny {
		return 0, 0, 0, nil, errors.New("块数与画幅不匹配")
	}
	pix := make([]uint8, w*h)
	for by := 0; by < ny; by++ {
		for bx := 0; bx < nx; bx++ {
			v := data[9+by*nx+bx]
			for y := by * L; y < (by+1)*L; y++ {
				row := y * w
				for x := bx * L; x < (bx+1)*L; x++ {
					pix[row+x] = v
				}
			}
		}
	}
	return w, h, L, pix, nil
}
