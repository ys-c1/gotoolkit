package subband

import (
	"encoding/binary"
	"errors"
	"math"
)

// GTS1 是子带码流标识（gotoolkit subband 1）。
// 码流布局：
//
//	magic(4) | w u16le | h u16le | levels u8 | budget u32le | mode u8 | reserved(3)
//	随后每个子带（按分解的自然顺序）写：
//	  min f64le | mean f64le | range f64le | bits u8
//	  再写该带 N 个系数的量化索引，每个索引 bits 位（bits=0 时不写）。
//	系数比特不足整字节时右补 0；两条路径都再补 0 到恰好 budget 位，保证同体积。
const magicGTS1 = "GTS1"

// ModeWaterfill / ModeEqual 选择比特分配路径。
const (
	ModeWaterfill = 0
	ModeEqual     = 1
)

const headerSize = 17 // magic4 + w2 + h2 + levels1 + budget4 + mode1 + reserved3
const bandSideSize = 25 // min f64 + mean f64 + range f64 + bits u8

type bitWriter struct {
	buf  []byte
	cur  byte
	used uint // 当前字节已用位数
}

func (w *bitWriter) writeBits(v uint, n int) {
	for n > 0 {
		space := 8 - w.used
		take := n
		if take > int(space) {
			take = int(space)
		}
		// 取 v 最低 take 位，放到当前字节高位起。
		shift := uint(n - take)
		w.cur |= byte((v>>shift)&((1<<take)-1)) << (space - uint(take))
		w.used += uint(take)
		n -= take
		if w.used == 8 {
			w.buf = append(w.buf, w.cur)
			w.cur, w.used = 0, 0
		}
	}
}

func (w *bitWriter) bytes() []byte {
	if w.used > 0 {
		w.buf = append(w.buf, w.cur)
	}
	return w.buf
}

type bitReader struct {
	buf  []byte
	pos  int
	used uint
}

func (r *bitReader) readBits(n int) (uint, error) {
	var v uint
	for n > 0 {
		if r.pos >= len(r.buf) {
			return 0, errors.New("码流系数位不足")
		}
		space := 8 - r.used
		take := n
		if take > int(space) {
			take = int(space)
		}
		bits := (r.buf[r.pos] >> (space - uint(take))) & ((1 << take) - 1)
		v = (v << uint(take)) | uint(bits)
		r.used += uint(take)
		n -= take
		if r.used == 8 {
			r.pos++
			r.used = 0
		}
	}
	return v, nil
}

// EncodeStream 按给定每带比特数把子带量化后打成 GTS1 字节流。
// budget 为总比特预算（含头与边信息之外的系数预算），用于把系数区补齐到同一体积。
func EncodeStream(pix []uint8, w, h, levels, budget, mode int, bits []int) ([]byte, error) {
	bands, err := ForwardDWT(pix, w, h, levels)
	if err != nil {
		return nil, err
	}
	if len(bits) != len(bands) {
		return nil, errors.New("比特数与子带数不符")
	}
	stats := make([]Stats, len(bands))
	for i, b := range bands {
		stats[i] = ComputeStats(b)
	}

	out := make([]byte, 0, headerSize+len(bands)*bandSideSize+budget/8+1)
	out = append(out, magicGTS1...)
	out = binary.LittleEndian.AppendUint16(out, uint16(w))
	out = binary.LittleEndian.AppendUint16(out, uint16(h))
	out = append(out, byte(levels))
	out = binary.LittleEndian.AppendUint32(out, uint32(budget))
	out = append(out, byte(mode), 0, 0, 0)

	bw := &bitWriter{}
	coefBits := 0
	for i, b := range bands {
		q := Quantize(b, stats[i], bits[i])
		// 每条边信息用独立切片，避免复用数组时旧字节残留。
		side := make([]byte, bandSideSize)
		binary.LittleEndian.PutUint64(side[0:], math.Float64bits(stats[i].Min))
		binary.LittleEndian.PutUint64(side[8:], math.Float64bits(stats[i].Mean))
		binary.LittleEndian.PutUint64(side[16:], math.Float64bits(stats[i].Range))
		side[24] = byte(bits[i])
		out = append(out, side...)

		if bits[i] > 0 {
			step := q.Step
			levelCount := 1 << uint(bits[i])
			for _, c := range b.Coef {
				idx := int((c - stats[i].Min) / step)
				if idx >= levelCount {
					idx = levelCount - 1
				}
				if idx < 0 {
					idx = 0
				}
				bw.writeBits(uint(idx), bits[i])
				coefBits += bits[i]
			}
		}
	}
	out = append(out, bw.bytes()...)
	if coefBits > budget {
		return nil, errors.New("系数比特超出预算")
	}
	// 补齐到 budget 位，使注水/平分两条路径产出同体积文件。
	target := headerSize + len(bands)*bandSideSize + (budget+7)/8
	for len(out) < target {
		out = append(out, 0)
	}
	return out, nil
}

// DecodeStream 解析 GTS1 并重构像素。
func DecodeStream(data []byte) (w, h, levels, budget, mode int, pix []uint8, err error) {
	if len(data) < headerSize || string(data[:4]) != magicGTS1 {
		return 0, 0, 0, 0, 0, nil, errors.New("不是 GTS1 子带流")
	}
	w = int(binary.LittleEndian.Uint16(data[4:]))
	h = int(binary.LittleEndian.Uint16(data[6:]))
	levels = int(data[8])
	budget = int(binary.LittleEndian.Uint32(data[9:]))
	mode = int(data[13])
	if w <= 0 || h <= 0 || levels <= 0 {
		return 0, 0, 0, 0, 0, nil, errors.New("GTS1 头非法")
	}
	nbands := 1 + 3*levels
	sideEnd := headerSize + nbands*bandSideSize
	if len(data) < sideEnd {
		return 0, 0, 0, 0, 0, nil, errors.New("边信息被截断")
	}

	br := &bitReader{buf: data[sideEnd:]}
	bands := make([]Band, 0, nbands)
	for k := 0; k < nbands; k++ {
		base := headerSize + k*bandSideSize
		mn := math.Float64frombits(binary.LittleEndian.Uint64(data[base:]))
		mean := math.Float64frombits(binary.LittleEndian.Uint64(data[base+8:]))
		rng := math.Float64frombits(binary.LittleEndian.Uint64(data[base+16:]))
		bits := int(data[base+24])
		if bits > MaxBitsPerCoef {
			return 0, 0, 0, 0, 0, nil, errors.New("单带比特越界")
		}
		level, name := bandOrder(levels, k)
		_, bwSize, bhSize, rerr := bandRect(w, h, level, name)
		if rerr != nil {
			return 0, 0, 0, 0, 0, nil, rerr
		}
		n := bwSize * bhSize
		coef := make([]float64, n)
		if bits == 0 {
			for i := range coef {
				coef[i] = mean
			}
		} else {
			levelCount := 1 << uint(bits)
			step := rng / float64(levelCount)
			for i := range coef {
				idx, e := br.readBits(bits)
				if e != nil {
					return 0, 0, 0, 0, 0, nil, e
				}
				coef[i] = mn + (float64(idx)+0.5)*step
			}
		}
		bands = append(bands, Band{Name: name, Level: level, Coef: coef, N: n,
			Weight: float64(n) / float64(w*h)})
	}
	pix, err = InverseDWT(bands, w, h, levels)
	return w, h, levels, budget, mode, pix, err
}

// bandOrder 把码流中的带序号 k 映射回 (level, name)，与 extractBands 的顺序一致。
func bandOrder(levels, k int) (int, string) {
	for level := levels; level >= 1; level-- {
		names := []string{"LH", "HL", "HH"}
		if level == levels {
			names = []string{"LL", "LH", "HL", "HH"}
		}
		if k < len(names) {
			return level, names[k]
		}
		k -= len(names)
	}
	return 0, ""
}
