// 独立参照实现：逐字节写出三种格式的基准，供 testdata/golden 使用。
// 与 internal/codec 的实现刻意不同：这里全部用显式字节拼接，不共享任何代码。
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func sample() []byte {
	out := make([]byte, 12)
	for i := range out {
		out[i] = byte(i * 7)
	}
	return out
}

func pgm(w, h int, pix []byte) []byte {
	head := fmt.Sprintf("P5\n%d %d\n255\n", w, h)
	return append([]byte(head), pix...)
}

func bmp(w, h int, pix []byte) []byte {
	row := (w*3 + 3) / 4 * 4
	data := row * h
	total := 54 + data
	b := make([]byte, total)
	b[0], b[1] = 'B', 'M'
	put32(b, 2, total)
	put32(b, 10, 54)
	put32(b, 14, 40)
	put32(b, 18, w)
	put32(b, 22, h)
	put16(b, 26, 1)
	put16(b, 28, 24)
	put32(b, 34, data)
	for y := 0; y < h; y++ {
		src := (h - 1 - y) * w
		base := 54 + y*row
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
	b := []byte{'G', 'T', 'R', '1'}
	b = append(b, byte(w), byte(w>>8), byte(h), byte(h>>8))
	i := 0
	for i < len(pix) {
		n := 1
		for i+n < len(pix) && pix[i+n] == pix[i] && n < 255 {
			n++
		}
		b = append(b, byte(n), pix[i])
		i += n
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
