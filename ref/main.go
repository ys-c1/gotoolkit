// 独立参照实现：逐字节写出三种格式的基准，供 testdata/golden 使用。
// 与 internal/codec 的实现刻意不同：这里全部用显式字节拼接，不共享任何代码。
//
// 对齐规则（与 internal/codec 一致，但此处独立推导）：
// 像素数据起始偏移（文件内）必须是 4 的倍数。
// 填充字节数 p = (4 - L0%4) % 4，L0 为头部名义长度。
//
//	PGM: L0 = 8 + len("<w> <h>")，p 个空格插在 "P5\n" 之后（仍是头部空白）。
//	BMP: L0 = 54，p 个 0x00 插在 54 字节头之后，off 写入 bfOffBits。
//	RLE: L0 = 8，p = 0。
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const align = 4

func padFor(off int) int { return (align - off%align) % align }

func sample() []byte {
	out := make([]byte, 12)
	for i := range out {
		out[i] = byte(i * 7)
	}
	return out
}

func pgm(w, h int, pix []byte) []byte {
	dims := fmt.Sprintf("%d %d", w, h)
	head := "P5\n" + fmt.Sprintf("%*s", padFor(8+len(dims)), "") + dims + "\n255\n"
	b := []byte(head)
	return append(b, pix...)
}

func bmp(w, h int, pix []byte) []byte {
	row := (w*3 + 3) / 4 * 4
	data := row * h
	off := 54 + padFor(54)
	total := off + data
	b := make([]byte, total)
	b[0], b[1] = 'B', 'M'
	put32(b, 2, total)
	put32(b, 10, off)
	put32(b, 14, 40)
	put32(b, 18, w)
	put32(b, 22, h)
	put16(b, 26, 1)
	put16(b, 28, 24)
	put32(b, 34, data)
	for y := 0; y < h; y++ {
		src := (h - 1 - y) * w
		base := off + y*row
		for x := 0; x < w; x++ {
			v := pix[src+x]
			b[base+x*3+0] = v
			b[base+x*3+1] = v
			b[base+x*3+2] = v
		}
	}
	return b
}

func rle(w, h int, pix []byte) []byte {
	n := 8 + padFor(8)
	b := make([]byte, n, n+len(pix)*2)
	b[0], b[1], b[2], b[3] = 'G', 'T', 'R', '1'
	b[4], b[5] = byte(w), byte(w>>8)
	b[6], b[7] = byte(h), byte(h>>8)
	i := 0
	for i < len(pix) {
		k := 1
		for i+k < len(pix) && pix[i+k] == pix[i] && k < 255 {
			k++
		}
		b = append(b, byte(k), pix[i])
		i += k
	}
	return b
}

func put16(b []byte, off, v int) {
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
}

func put32(b []byte, off, v int) {
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
	b[off+2] = byte(v >> 16)
	b[off+3] = byte(v >> 24)
}

func main() {
	dir := filepath.Join("..", "testdata", "golden")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	pix := sample()
	files := map[string][]byte{
		"pgm.bin": pgm(4, 3, pix),
		"bmp.bin": bmp(4, 3, pix),
		"rle.bin": rle(4, 3, pix),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			panic(err)
		}
		fmt.Println(name, len(body), "bytes")
	}
}
