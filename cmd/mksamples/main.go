// Command mksamples 生成三份样本，各存两种行序：
//
//	sampleN.src.pgm   逻辑像素基准（自上而下）
//	sampleN.<legacy>  旧文件：不带任何标志的原始编码
//	sampleN.alt.<ext> 同一份数据、另一种行序的编码
//
// 三种格式的字节都用显式拼接独立写出，不调用 internal/codec，
// 因此这些文件可以作为“旧文件”的独立基线。
//
// 用法：go run ./cmd/mksamples -out testdata/samples
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type sample struct {
	name string
	w, h int
	pix  []byte
	ext  string // legacy / alt 的扩展名
}

func (s sample) value(x, y int) byte {
	// 每个逻辑行都有独特取值，行序翻错时逐行都能看出来。
	return byte((x*37 + y*61 + s.w*11 + s.h*7) & 0xff)
}

func (s sample) pixels() []byte {
	if s.pix != nil {
		return s.pix
	}
	p := make([]byte, s.w*s.h)
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			p[y*s.w+x] = s.value(x, y)
		}
	}
	return p
}

func main() {
	out := flag.String("out", "testdata/samples", "输出目录")
	flag.Parse()
	outDir = *out
	if err := os.MkdirAll(*out, 0o755); err != nil {
		panic(err)
	}
	samples := []sample{
		{name: "sample1", w: 5, h: 5, ext: "pgm"},
		{name: "sample2", w: 6, h: 4, ext: "bmp"},
		{name: "sample3", w: 7, h: 3, ext: "gtr"},
	}
	for _, s := range samples {
		pix := s.pixels()
		write(s.name+".src.pgm", pgm(s.w, s.h, pix, false))
		switch s.ext {
		case "pgm":
			write(s.name+".pgm", pgm(s.w, s.h, pix, false))
			write(s.name+".alt.pgm", pgm(s.w, s.h, pix, true))
		case "bmp":
			write(s.name+".bmp", bmp(s.w, s.h, pix, true))
			write(s.name+".alt.bmp", bmp(s.w, s.h, pix, false))
		case "gtr":
			write(s.name+".gtr", rle(s.w, s.h, pix, false))
			write(s.name+".alt.gtr", rle(s.w, s.h, pix, true))
		}
		fmt.Printf("%s %dx%d %s\n", s.name, s.w, s.h, s.ext)
	}
}

func write(name string, b []byte) {
	if err := os.WriteFile(filepath.Join(outDir, name), b, 0o644); err != nil {
		panic(err)
	}
}

var outDir string

// pgm: P5 头（ASCII）+ 原始像素；bottomUp 时先写逻辑末行。
func pgm(w, h int, pix []byte, bottomUp bool) []byte {
	b := []byte(fmt.Sprintf("P5\n%d %d\n255\n", w, h))
	for y := 0; y < h; y++ {
		src := y
		if bottomUp {
			src = h - 1 - y
		}
		b = append(b, pix[src*w:(src+1)*w]...)
	}
	return b
}

// bmp: 24 位灰度、每行补到 4 字节；height 正 = 自底向上，负 = 自上而下。
func bmp(w, h int, pix []byte, bottomUp bool) []byte {
	row := (w*3 + 3) &^ 3
	data := row * h
	total := 54 + data
	b := make([]byte, total)
	b[0], b[1] = 'B', 'M'
	put32(b, 2, total)
	put32(b, 10, 54)
	put32(b, 14, 40)
	put32(b, 18, w)
	if bottomUp {
		put32(b, 22, h)
	} else {
		put32(b, 22, -h)
	}
	put16(b, 26, 1)
	put16(b, 28, 24)
	put32(b, 34, data)
	for y := 0; y < h; y++ {
		src := y
		if bottomUp {
			src = h - 1 - y
		}
		base := 54 + y*row
		for x := 0; x < w; x++ {
			v := pix[src*w+x]
			b[base+x*3], b[base+x*3+1], b[base+x*3+2] = v, v, v
		}
	}
	return b
}

// rle: GTR1 + w,h 小端 + (count,value)，按存储行序展开；紧凑，无行补位。
func rle(w, h int, pix []byte, bottomUp bool) []byte {
	b := []byte{'G', 'T', 'R', '1', byte(w), byte(w >> 8), byte(h), byte(h >> 8)}
	stream := make([]byte, 0, w*h)
	for y := 0; y < h; y++ {
		src := y
		if bottomUp {
			src = h - 1 - y
		}
		stream = append(stream, pix[src*w:(src+1)*w]...)
	}
	i := 0
	for i < len(stream) {
		n := 1
		for i+n < len(stream) && stream[i+n] == stream[i] && n < 255 {
			n++
		}
		b = append(b, byte(n), stream[i])
		i += n
	}
	return b
}

func put16(b []byte, off, v int) {
	b[off], b[off+1] = byte(v), byte(v>>8)
}

func put32(b []byte, off, v int) {
	b[off], b[off+1], b[off+2], b[off+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}
