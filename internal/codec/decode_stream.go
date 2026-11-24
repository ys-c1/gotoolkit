package codec

import (
	"bytes"
	"errors"
	"fmt"
)

// BandSink 接收逐块解码出的行带。实现方必须在 WriteBand 内完成落盘/合并，
// 因为传入的 band 在返回后会被复用，不许持有其底层数组。
type BandSink interface {
	WriteBand(y0, rows int, band []uint8) error
}

// bandsEqual 是分块合并前的闸门：两条独立实现的结果逐字节相同才放行。
func bandsEqual(a, b []uint8) bool { return bytes.Equal(a, b) }

// seamChecks 返回行带四角的 (行内偏移, 列) 抽查坐标，用于分块边界像素抽查。
func seamChecks(rows, w int) [][2]int {
	return [][2]int{{0, 0}, {0, w - 1}, {rows - 1, 0}, {rows - 1, w - 1}}
}

// checkSeamPixels 抽查一个行带的边界像素：四角像素对照独立参照 ref 取值。
// ref 以全局坐标 (y,x) 给出期望值，刻意不读 band 自身，保证抽查独立性。
func checkSeamPixels(band []uint8, w, y0, rows int, ref func(y, x int) uint8, format string) error {
	for _, c := range seamChecks(rows, w) {
		r, x := c[0], c[1]
		if band[r*w+x] != ref(y0+r, x) {
			return fmt.Errorf("边界像素抽查失败: %s (%d,%d)", format, y0+r, x)
		}
	}
	return nil
}

// DecodeBandsPGM 按行带流式解码 P5：每个行带分别用两条独立实现解出，
// 逐字节比对通过后才交给 sink（先比对、后合并），并抽查分块边界像素。
func DecodeBandsPGM(data []byte, bandRows int, sink BandSink) error {
	w, h, off, err := ProbePGM(data)
	if err != nil {
		return err
	}
	if bandRows <= 0 || bandRows > h {
		return errors.New("分块行数非法")
	}
	payload := data[off:]
	if int64(len(payload)) < int64(w)*int64(h) {
		return errors.New("像素数据不足")
	}
	// 全量参照仅用于边界抽查（抽样，不参与逐块判定）。
	reference := func(y, x int) uint8 { return payload[y*w+x] }
	for y0 := 0; y0 < h; y0 += bandRows {
		rows := bandRows
		if y0+rows > h {
			rows = h - y0
		}
		n := rows * w
		band := make([]uint8, n)
		// 实现 A：切片直接拷贝（与 DecodePGM 的 copy 同源语义）。
		copy(band, payload[y0*w:y0*w+n])
		// 实现 B：逐行逐像素独立重解，刻意不复用 A 的字节路径。
		indep := make([]uint8, n)
		for r := 0; r < rows; r++ {
			for x := 0; x < w; x++ {
				indep[r*w+x] = payload[(y0+r)*w+x]
			}
		}
		if !bandsEqual(band, indep) {
			return fmt.Errorf("分块比对失败: pgm y0=%d rows=%d", y0, rows)
		}
		// 边界抽查：四角像素对照独立全量参照（不读 band 自身）。
		if err := checkSeamPixels(band, w, y0, rows, reference, "pgm"); err != nil {
			return err
		}
		if err := sink.WriteBand(y0, rows, band); err != nil {
			return err
		}
	}
	return nil
}

// DecodeBandsBMP 按行带流式解码 24 位 BMP，含逐块双实现比对与边界抽查。
func DecodeBandsBMP(data []byte, bandRows int, sink BandSink) error {
	w, h, off, err := ProbeBMP(data)
	if err != nil {
		return err
	}
	if bandRows <= 0 || bandRows > h {
		return errors.New("分块行数非法")
	}
	rowSize := (w*3 + 3) &^ 3
	if int64(len(data)) < int64(off)+int64(rowSize)*int64(h) {
		return errors.New("像素数据不足")
	}
	reference := func(y, x int) uint8 { // 全量参照，仅用于抽查
		srcY := h - 1 - y
		return data[off+srcY*rowSize+x*3]
	}
	for y0 := 0; y0 < h; y0 += bandRows {
		rows := bandRows
		if y0+rows > h {
			rows = h - y0
		}
		band := make([]uint8, rows*w)
		// 实现 A：按行指针直接取蓝通道（与 DecodeBMP 同语义）。
		for r := 0; r < rows; r++ {
			y := y0 + r
			srcY := h - 1 - y
			base := off + srcY*rowSize
			for x := 0; x < w; x++ {
				band[r*w+x] = data[base+x*3]
			}
		}
		// 实现 B：先在文件行序里重排目标行号，再以字节偏移独立取值。
		indep := make([]uint8, rows*w)
		for r := rows - 1; r >= 0; r-- { // 反向遍历，覆盖不同的循环路径
			fileRow := h - 1 - (y0 + r)
			lineStart := off + fileRow*rowSize
			for x := 0; x < w; x++ {
				indep[r*w+x] = data[lineStart+3*x]
			}
		}
		if !bandsEqual(band, indep) {
			return fmt.Errorf("分块比对失败: bmp y0=%d rows=%d", y0, rows)
		}
		if err := checkSeamPixels(band, w, y0, rows, reference, "bmp"); err != nil {
			return err
		}
		if err := sink.WriteBand(y0, rows, band); err != nil {
			return err
		}
	}
	return nil
}

// DecodeBandsRLE 按行带流式解码 GTR1：
//   - 先做一次纯计数扫描校验总像素数（不物化游程表，额外内存为 O(1)）；
//   - 再单次流式展开到行带缓冲；
//   - 每个行带落地前，从游程字节流头部重新独立扫描重解同一行带，
//     逐字节比对通过才合并，并抽查边界像素。
func DecodeBandsRLE(data []byte, bandRows int, sink BandSink) error {
	w, h, err := ProbeRLE(data)
	if err != nil {
		return err
	}
	if bandRows <= 0 || bandRows > h {
		return errors.New("分块行数非法")
	}
	n := w * h
	// 纯计数校验：只累加计数，不分配游程表。
	total := 0
	for i := 8; i+1 < len(data); i += 2 {
		total += int(data[i])
		if total > n {
			return errors.New("游程超出画幅")
		}
	}
	if total != n {
		return errors.New("像素数不足")
	}
	// resolve 独立重解矩形 [y0, y0+rows)：从字节流头部重新扫描，与主展开
	// 不共享任何中间状态，刻意走不同的循环结构（边界裁剪 vs 顺序 append）。
	resolve := func(y0, rows int) []uint8 {
		wantStart, wantEnd := y0*w, (y0+rows)*w
		out := make([]uint8, rows*w)
		pos := 0
		for i := 8; i+1 < len(data); i += 2 {
			c, v := int(data[i]), data[i+1]
			runEnd := pos + c
			if runEnd > wantStart && pos < wantEnd {
				lo := pos
				if lo < wantStart {
					lo = wantStart
				}
				hi := runEnd
				if hi > wantEnd {
					hi = wantEnd
				}
				for k := lo; k < hi; k++ {
					out[k-wantStart] = v
				}
			}
			pos = runEnd
			if pos >= wantEnd {
				break
			}
		}
		return out
	}
	// pixelAt 独立重扫游程流取单个全局像素的值，供边界抽查使用；
	// 与主展开和 resolve 的整带重解都不共享中间状态。
	pixelAt := func(y, x int) uint8 {
		want := y*w + x
		pos := 0
		for i := 8; i+1 < len(data); i += 2 {
			c, v := int(data[i]), data[i+1]
			if want < pos+c {
				return v
			}
			pos += c
		}
		return 0
	}
	band := make([]uint8, 0, bandRows*w)
	flush := func(y0, rows int) error {
		indep := resolve(y0, rows)
		if len(indep) != rows*w || !bandsEqual(band[:rows*w], indep) {
			return fmt.Errorf("分块比对失败: rle y0=%d rows=%d", y0, rows)
		}
		if err := checkSeamPixels(band[:rows*w], w, y0, rows, pixelAt, "rle"); err != nil {
			return err
		}
		return sink.WriteBand(y0, rows, band[:rows*w])
	}
	pos := 0
	y0 := 0
	for i := 8; i+1 < len(data); i += 2 {
		c, v := int(data[i]), data[i+1]
		for k := 0; k < c; k++ {
			band = append(band, v)
			pos++
			if len(band) == cap(band) || pos == n {
				rows := len(band) / w
				if err := flush(y0, rows); err != nil {
					return err
				}
				y0 += rows
				band = band[:0]
			}
		}
	}
	if len(band) != 0 {
		return errors.New("rle 分块残余未刷新")
	}
	return nil
}

// DecodeBands 按格式分派分块解码。
func DecodeBands(format string, data []byte, bandRows int, sink BandSink) error {
	switch format {
	case "pgm":
		return DecodeBandsPGM(data, bandRows, sink)
	case "bmp":
		return DecodeBandsBMP(data, bandRows, sink)
	default:
		return DecodeBandsRLE(data, bandRows, sink)
	}
}
