package codec

import (
	"encoding/binary"
	"errors"
)

// GTBK 是固定块长的块均值+残差位打包格式：
//
//	头 9 字节：'G','T','B','K' + 宽(uint16LE) + 高(uint16LE) + 块长B(uint8)
//	每个 B×B 块依次写 4 字节头：
//	  均值 mean(uint8)、残差位深 k(uint8，1..8)、残差字节数 n(uint16LE)
//	随后 n 个残差字节，MSB 优先位打包，每像素 k 位有符号残差 r∈[-2^(k-1),2^(k-1)-1]，
//	字节内最后一个不完整的字用 0 填充。
//
// 宽高必须能被块长整除，块长必须是 2 的幂且不小于 2。
const gtbkHeader = 9

// EncodeGTBK 按给定块长做块编码。
func EncodeGTBK(w, h int, pix []uint8, block int) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if block < 2 || block&(block-1) != 0 {
		return nil, errors.New("块长必须是不小于 2 的 2 的幂")
	}
	if w%block != 0 || h%block != 0 {
		return nil, errors.New("宽高必须能被块长整除")
	}
	nbw, nbh := w/block, h/block
	out := make([]byte, 0, gtbkHeader+nbh*nbw*(3+block*block))
	out = append(out, 'G', 'T', 'B', 'K')
	out = binary.LittleEndian.AppendUint16(out, uint16(w))
	out = binary.LittleEndian.AppendUint16(out, uint16(h))
	out = append(out, uint8(block))

	bitw := newBitWriter()
	for by := 0; by < nbh; by++ {
		for bx := 0; bx < nbw; bx++ {
			// 块均值，四舍五入（与 Python round half to even 不同，这里用加半取整）。
			sum := 0
			for y := 0; y < block; y++ {
				row := (by*block+y)*w + bx*block
				for x := 0; x < block; x++ {
					sum += int(pix[row+x])
				}
			}
			mean := (sum + block*block/2) / (block * block)
			if mean > 255 {
				mean = 255
			}

			base := len(out)
			out = append(out, uint8(mean), 0, 0, 0) // 占位，回填 k、n(uint16)
			rsd := make([]int, block*block)
			amax := 0
			idx := 0
			for y := 0; y < block; y++ {
				row := (by*block+y)*w + bx*block
				for x := 0; x < block; x++ {
					r := int(pix[row+x]) - mean
					if r < -128 {
						r = -128
					}
					if r > 127 {
						r = 127
					}
					rsd[idx] = r
					idx++
					a := r
					if a < 0 {
						a = -a
					}
					if a > amax {
						amax = a
					}
				}
			}
			// 能表示 [-2^(k-1), 2^(k-1)-1] 的最小 k。
			k := 1
			if amax > 0 {
				for k < 8 && amax > (1<<(k-1))-1 {
					k++
				}
			}
			lo := -(1 << (k - 1))
			hi := -lo - 1
			bitw.reset()
			for _, r0 := range rsd {
				r := r0
				if r < lo {
					r = lo
				}
				if r > hi {
					r = hi
				}
				bitw.writeBits(uint32(r-lo), k)
			}
			packed := bitw.bytes()
			if len(packed) > 0xffff {
				return nil, errors.New("单块残差字节数超出 uint16 范围")
			}
			out[base+1] = uint8(k)
			binary.LittleEndian.PutUint16(out[base+2:], uint16(len(packed)))
			out = append(out, packed...)
		}
	}
	return out, nil
}

// DecodeGTBK 解析块编码并重建灰度图。
func DecodeGTBK(data []byte) (int, int, []uint8, error) {
	if len(data) < gtbkHeader || string(data[0:4]) != "GTBK" {
		return 0, 0, nil, errors.New("不是 GTBK 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	block := int(data[8])
	if w <= 0 || h <= 0 || block < 2 || block&(block-1) != 0 || w%block != 0 || h%block != 0 {
		return 0, 0, nil, errors.New("GTBK 头非法")
	}
	nbw, nbh := w/block, h/block
	pix := make([]uint8, w*h)
	pos := gtbkHeader
	reader := newBitReader(nil)
	for by := 0; by < nbh; by++ {
		for bx := 0; bx < nbw; bx++ {
			if pos+4 > len(data) {
				return 0, 0, nil, errors.New("GTBK 块头被截断")
			}
			mean := int(data[pos])
			k := int(data[pos+1])
			n := int(binary.LittleEndian.Uint16(data[pos+2:]))
			pos += 4
			if k < 1 || k > 8 || pos+n > len(data) {
				return 0, 0, nil, errors.New("GTBK 残差被截断或位深非法")
			}
			reader.reset(data[pos : pos+n])
			pos += n
			lo := -(1 << (k - 1))
			for y := 0; y < block; y++ {
				row := (by*block+y)*w + bx*block
				for x := 0; x < block; x++ {
					u, ok := reader.readBits(k)
					if !ok {
						return 0, 0, nil, errors.New("GTBK 残差位不足")
					}
					v := mean + int(u) + lo
					if v < 0 {
						v = 0
					}
					if v > 255 {
						v = 255
					}
					pix[row+x] = uint8(v)
				}
			}
		}
	}
	if pos != len(data) {
		return 0, 0, nil, errors.New("GTBK 存在尾部冗余字节")
	}
	return w, h, pix, nil
}

// bitWriter 按 MSB 优先打包定宽码字。
type bitWriter struct {
	buf []byte
	cur byte
	n   int
}

func newBitWriter() *bitWriter { return &bitWriter{} }

func (bw *bitWriter) reset() {
	bw.buf = bw.buf[:0]
	bw.cur, bw.n = 0, 0
}

func (bw *bitWriter) writeBits(v uint32, width int) {
	for i := width - 1; i >= 0; i-- {
		bw.cur = bw.cur<<1 | byte((v>>uint(i))&1)
		bw.n++
		if bw.n == 8 {
			bw.buf = append(bw.buf, bw.cur)
			bw.cur, bw.n = 0, 0
		}
	}
}

func (bw *bitWriter) bytes() []byte {
	if bw.n == 0 {
		return append([]byte(nil), bw.buf...)
	}
	out := append(append([]byte(nil), bw.buf...), bw.cur<<uint(8-bw.n))
	return out
}

// bitReader 按 MSB 优先读出定宽码字。
type bitReader struct {
	buf []byte
	pos int
	cur byte
	n   int
}

func newBitReader(buf []byte) *bitReader { return &bitReader{buf: buf} }

func (br *bitReader) reset(buf []byte) {
	br.buf = buf
	br.pos, br.cur, br.n = 0, 0, 0
}

func (br *bitReader) readBits(width int) (uint32, bool) {
	var v uint32
	for i := 0; i < width; i++ {
		if br.n == 0 {
			if br.pos >= len(br.buf) {
				return 0, false
			}
			br.cur = br.buf[br.pos]
			br.pos++
			br.n = 8
		}
		v = v<<1 | uint32((br.cur>>7)&1)
		br.cur <<= 1
		br.n--
	}
	return v, true
}
