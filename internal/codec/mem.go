package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// memFixed 是解码工作集之外的固定余量：slice 头、结构体、runtime 自身占用。
// 所有预算判定都留这份余量，模型才不会贴着物理极限走。
const memFixed int64 = 64 << 10

// 各格式头部的保守上界，反解画幅时用于扣减。
const (
	pgmHeaderMax = 32
	bmpHeaderMax = 54
	rleHeaderMax = 8
)

// BudgetError 是解码被内存预算拒绝时的结构化错误。
// Item 指明超限的具体是哪一项，调用方可以直接展示给用户。
type BudgetError struct {
	Format string
	W, H   int
	Budget int64 // 字节
	Need   int64 // 字节：该路径的模型峰值
	Item   string
	Hint   string
}

func (e *BudgetError) Error() string {
	s := fmt.Sprintf("%s %dx%d 需要 %d 字节，预算 %d 字节；超限项=%s",
		e.Format, e.W, e.H, e.Need, e.Budget, e.Item)
	if e.Hint != "" {
		s += "；" + e.Hint
	}
	return s
}

// MemPlan 是一次解码的内存核算结果。所有数字都是字节。
type MemPlan struct {
	Format string
	W, H   int
	Pixels int64
	Input  int64 // 编码流自身（调用时已在内存中，必须计入）
	// MinInput 是按头声明画幅反推的最坏编码流大小。
	// 截断流的 Input 可能很小，但画幅需要的输入必须按 MinInput 核算，
	// 否则“巨型头 + 空壳流”会绕过预算闸门。
	MinInput  int64
	BandRows  int
	BandBytes int64
	WholePeak int64 // 原路径模型峰值
	ChunkPeak int64 // 分块流式路径模型峰值
}

// WorstEncoded 返回 n 像素在最坏情况下的编码流字节数。
// RLE 游程最短为 1，每像素最多占 2 字节；BMP 每行另有至多 3 字节对齐填充。
func WorstEncoded(format string, n int64) (int64, error) {
	switch format {
	case "pgm":
		return n + pgmHeaderMax, nil
	case "bmp":
		return 3*n + bmpHeaderMax, nil
	case "rle":
		return 2*n + rleHeaderMax, nil
	default:
		return 0, errors.New("未知格式")
	}
}

// MaxDecodablePixels 按预算反解最大可解码像素数。
// whole 为原路径上限，chunked 为流式分块路径的线性上限。
// 整图路径同时持有输入与整图输出；分块路径持输入 + 双行带缓冲
// （主缓冲 band 与独立重解缓冲 indep，各按整图像素线性估计的保守口径）。
//
//	pgm  整图 2n / 分块 3n
//	bmp  整图 4n / 分块 5n
//	rle  整图 3n / 分块 4n（最坏编码）
//
// 这是预算视角的画幅上限（“多大的图在该预算内有解”）；真实的单行
// 可行性由 PlanDecode.CheckChunk 在实际宽度上最终判定。
func MaxDecodablePixels(format string, budget int64) (whole, chunked int64) {
	if budget <= memFixed {
		return 0, 0
	}
	b := budget - memFixed
	switch format {
	case "pgm":
		whole = (b - pgmHeaderMax) / 2
		chunked = (b - pgmHeaderMax) / 3
	case "bmp":
		whole = (b - bmpHeaderMax) / 4
		chunked = (b - bmpHeaderMax) / 5
	case "rle":
		whole = (b - rleHeaderMax) / 3
		chunked = (b - rleHeaderMax) / 4
	default:
		return 0, 0
	}
	if whole < 0 {
		whole = 0
	}
	if chunked < 0 {
		chunked = 0
	}
	return whole, chunked
}

// MaxChunkedSize 是宽度感知的分块路径反解：给定宽度 w 与预算，
// 返回可流式解码的最大像素数与最大高度（行数）。
// 分块路径的工作集 = 输入流（随高度线性增长）+ 单行双缓冲 2*w（与高度无关），
// 因此窄而高的图在分块路径下可以远超整图路径的上限。
func MaxChunkedSize(format string, w int, budget int64) (pixels, height int64) {
	if w <= 0 || budget <= memFixed {
		return 0, 0
	}
	minBand := 2 * int64(w) // band + indep，各一行
	room := budget - memFixed - minBand
	if room <= 0 {
		return 0, 0
	}
	var rowBytes, hdr int64
	switch format {
	case "pgm":
		rowBytes, hdr = int64(w), pgmHeaderMax
	case "bmp":
		rowBytes, hdr = int64((3*w+3)&^3), bmpHeaderMax
	case "rle":
		rowBytes, hdr = 2*int64(w), rleHeaderMax
	default:
		return 0, 0
	}
	avail := room - hdr
	if avail < rowBytes {
		return 0, 0
	}
	maxRows := avail / rowBytes
	return maxRows * int64(w), maxRows
}

// Probe* 只解析头部、不分配像素缓冲，供预算核算使用。

func ProbePGM(data []byte) (w, h, off int, err error) {
	if len(data) < 10 || data[0] != 'P' || data[1] != '5' {
		return 0, 0, 0, errors.New("不是 P5 流")
	}
	i := 2
	next := func() (int, bool) {
		for i < len(data) && (data[i] == ' ' || data[i] == '\n' || data[i] == '\r') {
			i++
		}
		start, val := i, 0
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			val = val*10 + int(data[i]-'0')
			i++
		}
		return val, i > start
	}
	w, ok1 := next()
	h, ok2 := next()
	maxv, ok3 := next()
	if !ok1 || !ok2 || !ok3 || w <= 0 || h <= 0 || maxv != 255 {
		return 0, 0, 0, errors.New("PGM 头非法")
	}
	i++ // 跳过分隔符
	return w, h, i, nil
}

func ProbeBMP(data []byte) (w, h, off int, err error) {
	if len(data) < 54 || string(data[0:2]) != "BM" {
		return 0, 0, 0, errors.New("不是 BMP 流")
	}
	w = int(int32(binary.LittleEndian.Uint32(data[18:])))
	h = int(int32(binary.LittleEndian.Uint32(data[22:])))
	bpp := int(binary.LittleEndian.Uint16(data[28:]))
	off = int(binary.LittleEndian.Uint32(data[10:]))
	if w <= 0 || h <= 0 || bpp != 24 {
		return 0, 0, 0, errors.New("BMP 头非法")
	}
	return w, h, off, nil
}

func ProbeRLE(data []byte) (w, h int, err error) {
	if len(data) < 8 || string(data[0:4]) != "GTR1" {
		return 0, 0, errors.New("不是 GTR1 流")
	}
	w = int(binary.LittleEndian.Uint16(data[4:]))
	h = int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, errors.New("画幅非法")
	}
	return w, h, nil
}

// PlanDecode 依据实际编码流核算两条路径的峰值，并选定分块行数。
// budget 为 0 表示不限预算（默认行为）。
func PlanDecode(format string, data []byte, budget int64, bandRowsHint int) (*MemPlan, error) {
	var (
		w, h int
		err  error
	)
	switch format {
	case "pgm":
		w, h, _, err = ProbePGM(data)
	case "bmp":
		w, h, _, err = ProbeBMP(data)
	default:
		w, h, err = ProbeRLE(data)
	}
	if err != nil {
		return nil, err
	}
	n := int64(w) * int64(h)
	if n <= 0 || n > int64(^uint(0)>>1) {
		return nil, errors.New("画幅溢出")
	}
	input := int64(len(data))
	minInput, err := WorstEncoded(format, n)
	if err != nil {
		return nil, err
	}
	// 真实完整流可能比最坏估计还大的情况不存在（WorstEncoded 取上界）；
	// 但实际输入若已超过上界（异常填充），以实际值为准。
	if input > minInput {
		minInput = input
	}
	p := &MemPlan{
		Format: format, W: w, H: h, Pixels: n, Input: input, MinInput: minInput,
		WholePeak: minInput + n + memFixed,
	}
	if bandRowsHint < 0 {
		return nil, errors.New("分块行数不能为负")
	}
	switch {
	case bandRowsHint > 0:
		p.BandRows = bandRowsHint
		if p.BandRows > h {
			p.BandRows = h
		}
	case budget > 0:
		// 自动选最大的可容纳块（至少一行）。这里绝不夹到画幅高度：
		// 巨型头 + 小预算时，整图高度本身就是放不下的对象，
		// 夹住会让单行检查失效、让拒绝闸门漏判。
		// 分块路径同时持有主缓冲 band 与独立重解缓冲 indep（双缓冲），
		// 所以每行占 2*w 字节工作集。
		room := budget - minInput - memFixed
		rows := int(room / (2 * int64(w)))
		if rows < 1 {
			rows = 1
		}
		p.BandRows = rows
	default:
		p.BandRows = h
	}
	p.BandBytes = int64(p.BandRows) * int64(w)
	// 双缓冲：band（主展开）与 indep（独立重解）在比对期间同时存活。
	p.ChunkPeak = minInput + 2*p.BandBytes + memFixed
	return p, nil
}

// Reject 构造结构化拒绝错误，item 为超限项，hint 为可选建议。
func (p *MemPlan) reject(budget int64, item, hint string) *BudgetError {
	return &BudgetError{
		Format: p.Format, W: p.W, H: p.H,
		Budget: budget, Need: p.WholePeak, Item: item, Hint: hint,
	}
}

// CheckWhole 判定原路径是否在预算内；超限时指出超限项。
func (p *MemPlan) CheckWhole(budget int64) error {
	if budget == 0 || p.WholePeak <= budget {
		return nil
	}
	item := "输出像素缓冲"
	hint := "改用 DecodeTo 走流式分块解码"
	if p.MinInput+memFixed > budget {
		item = "输入缓冲"
		hint = "编码流自身已超过预算，需缩小画幅或加大预算"
	}
	return p.reject(budget, item, hint)
}

// CheckChunk 判定分块流式路径是否在预算内；超限时指出超限项。
func (p *MemPlan) CheckChunk(budget int64) error {
	if budget == 0 || p.ChunkPeak <= budget {
		return nil
	}
	item := "分块工作集"
	hint := "即使单行工作集也放不下，需加大预算"
	if p.MinInput+memFixed > budget {
		item = "输入缓冲"
		hint = "编码流自身已超过预算，需缩小画幅或加大预算"
	}
	return &BudgetError{
		Format: p.Format, W: p.W, H: p.H,
		Budget: budget, Need: p.ChunkPeak, Item: item, Hint: hint,
	}
}
