package api

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func noisyPix(n int) []uint8 {
	pix := make([]uint8, n)
	for i := range pix {
		pix[i] = uint8((i*37 + 11) % 256) // 相邻像素异值，RLE 逼近最坏 2 字节/像素
	}
	return pix
}

// memSink 收集整块输出，供逐字节比对。
type apiMemSink struct {
	parts [][]byte
}

func (m *apiMemSink) WriteBand(_, _ int, band []uint8) error {
	m.parts = append(m.parts, append([]uint8(nil), band...))
	return nil
}

func (m *apiMemSink) bytes() []uint8 {
	var out []uint8
	for _, p := range m.parts {
		out = append(out, p...)
	}
	return out
}

// capSink 在每个分块边界记录运行时实际分配的行带缓冲容量 cap(band)。
// 这是真实分配字节（不是模型估计），用于实测峰值核算。
type capSink struct {
	maxCap int64
	bands  int
}

func (c *capSink) WriteBand(_, _ int, band []uint8) error {
	if int64(cap(band)) > c.maxCap {
		c.maxCap = int64(cap(band))
	}
	c.bands++
	return nil
}

// TestMeasuredPeakWithinBudget 实测峰值 = 输入 + 2×运行时真实双缓冲容量 +
// 固定余量（band 与独立重解缓冲在比对期同时存活，各占一份）。
// 实测峰值不得超过模型峰值与预算；且分块路径必须显著低于原路径（对照实验）。
func TestMeasuredPeakWithinBudget(t *testing.T) {
	img := &Image{W: 600, H: 600, Pix: noisyPix(600 * 600)}
	body, _ := Encode(img, Options{Format: "pgm"})
	inputBytes := int64(len(body))
	const fixed = int64(64 << 10)

	// 分块路径：用真实缓冲容量核算实测峰值。
	chunkSink := &capSink{}
	st, err := DecodeTo(body, Options{Format: "pgm", MemBudget: BudgetTight}, chunkSink)
	if err != nil {
		t.Fatal(err)
	}
	if st.Path != "chunked" || chunkSink.bands < 2 {
		t.Fatalf("应走多分块路径，path=%s bands=%d", st.Path, chunkSink.bands)
	}
	measuredChunkPeak := inputBytes + 2*chunkSink.maxCap + fixed
	if measuredChunkPeak > BudgetTight {
		t.Fatalf("实测峰值 %d 超 tight 预算 %d", measuredChunkPeak, BudgetTight)
	}
	if measuredChunkPeak > st.ModelPeak {
		t.Fatalf("实测峰值 %d 超模型峰值 %d", measuredChunkPeak, st.ModelPeak)
	}
	// 实测缓冲容量应与模型的 BandBytes 一致（同口径的真实分配）。
	if chunkSink.maxCap != st.BandBytes {
		t.Fatalf("实测单块容量 %d 与模型 BandBytes %d 不一致", chunkSink.maxCap, st.BandBytes)
	}

	// 原路径对照：持有完整输出缓冲（360000 像素），实测峰值必须显著更高。
	wholeSink := &capSink{}
	st2, err := DecodeTo(body, Options{Format: "pgm", MemBudget: BudgetGenerous}, wholeSink)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Path != "whole" {
		t.Fatal("应走原路径")
	}
	if st2.ModelPeak > BudgetGenerous {
		t.Fatalf("原路径模型峰值 %d 超 generous 预算", st2.ModelPeak)
	}
	// 原路径的全量输出必须真的被分配（对照实验的核心：分块把它省掉了）。
	if st2.Pixels != int64(600*600) {
		t.Fatal("原路径像素数错误")
	}
	if st2.ModelPeak <= st.ModelPeak {
		t.Fatalf("原路径峰值 %d 应高于分块峰值 %d", st2.ModelPeak, st.ModelPeak)
	}
	t.Logf("pgm 600x600: 分块实测峰值=%d (输入%d+双缓冲2×%d+固定%d) bands=%d; 原路径模型峰值=%d; tight=%d generous=%d",
		measuredChunkPeak, inputBytes, chunkSink.maxCap, fixed, chunkSink.bands,
		st2.ModelPeak, BudgetTight, BudgetGenerous)
}

// TestPathControlExperiment 对照实验：同一输入在充足预算走原路径、
// 紧张预算走分块路径，两路输出与无预算的历史 Decode 输出逐字节相同。
func TestPathControlExperiment(t *testing.T) {
	cases := map[string]struct {
		w, h int
	}{
		"pgm": {600, 600},
		"bmp": {300, 500},
		"rle": {300, 700},
	}
	for f, c := range cases {
		t.Run(f, func(t *testing.T) {
			img := &Image{W: c.w, H: c.h, Pix: noisyPix(c.w * c.h)}
			body, err := Encode(img, Options{Format: f})
			if err != nil {
				t.Fatal(err)
			}
			// 历史路径：无预算 Decode。
			ref, err := Decode(body, Options{Format: f})
			if err != nil {
				t.Fatal(err)
			}
			// 充足预算：必须走 whole 原路径。
			var genBuf bytes.Buffer
			stGen, err := DecodeToPGM(body, Options{Format: f, MemBudget: BudgetGenerous}, &genBuf)
			if err != nil {
				t.Fatal(err)
			}
			if stGen.Path != "whole" {
				t.Fatalf("充足预算应走原路径，实际 %s", stGen.Path)
			}
			// 紧张预算：必须走 chunked 分块路径。
			var tightBuf bytes.Buffer
			stTight, err := DecodeToPGM(body, Options{Format: f, MemBudget: BudgetTight}, &tightBuf)
			if err != nil {
				t.Fatal(err)
			}
			if stTight.Path != "chunked" {
				t.Fatalf("紧张预算应走分块路径，实际 %s", stTight.Path)
			}
			// 三路输出逐字节相同（PGM 封装后比对）。
			refPGM, err := Encode(ref, Options{Format: "pgm"})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(genBuf.Bytes(), refPGM) {
				t.Fatal("充足预算输出与历史路径不一致")
			}
			if !bytes.Equal(tightBuf.Bytes(), refPGM) {
				t.Fatal("紧张预算输出与历史路径不逐字节相同")
			}
			// 分块边界像素单独抽查：每块首尾行的行首尾像素对照历史输出。
			// 块高取小值 8，确保 BMP 这种输入占比高的格式也落在 tight 档内。
			sink := &apiMemSink{}
			st, err := DecodeTo(body, Options{Format: f, MemBudget: BudgetTight, BandRows: 8}, sink)
			if err != nil {
				t.Fatal(err)
			}
			if st.Path != "chunked" {
				t.Fatalf("应走分块路径，实际 %s", st.Path)
			}
			off := 0
			for _, part := range sink.parts {
				rows := len(part) / c.w
				seam := [][2]int{{0, 0}, {0, c.w - 1}, {rows - 1, 0}, {rows - 1, c.w - 1}}
				for _, q := range seam {
					r, x := q[0], q[1]
					if part[r*c.w+x] != ref.Pix[(off/c.w+r)*c.w+x] {
						t.Fatalf("分块边界像素 (%d,%d) 不一致", off/c.w+r, x)
					}
				}
				off += len(part)
			}
			if !bytes.Equal(sink.bytes(), ref.Pix) {
				t.Fatal("分块合并结果与整图不逐字节相同")
			}
		})
	}
}

// TestOriginalPathUnchanged 无预算时行为与历史完全一致（含 golden 口径）。
func TestOriginalPathUnchanged(t *testing.T) {
	img := &Image{W: 4, H: 3, Pix: noisyPix(12)}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, _ := Encode(img, Options{Format: f})
		a, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		st, err := DecodeToPGM(body, Options{Format: f}, &buf) // MemBudget=0
		if err != nil {
			t.Fatal(err)
		}
		if st.Path != "whole" {
			t.Fatalf("%s 无预算必须走原路径", f)
		}
		pgm, _ := Encode(a, Options{Format: "pgm"})
		if !bytes.Equal(buf.Bytes(), pgm) {
			t.Fatalf("%s 无预算输出被改变", f)
		}
	}
}

// TestBudgetRejection 超上限必须拒绝，并指出超限项；巨型头不得触发分配崩溃。
func TestBudgetRejection(t *testing.T) {
	// 巨型头 + 空壳流：必须在 make 之前被预算闸门拦下。
	giant := []byte("P5\n100000 100000\n255\nX")
	_, err := Decode(giant, Options{Format: "pgm", MemBudget: BudgetTight})
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("应返回 ErrBudget，得到 %v", err)
	}
	if msg := err.Error(); !contains(msg, "输入缓冲") {
		t.Fatalf("错误未指明超限项: %s", msg)
	}
	// 真实图：整图超 tight 档时，Decode 拒绝且指明输出像素缓冲超限。
	img := &Image{W: 600, H: 600, Pix: noisyPix(600 * 600)}
	body, _ := Encode(img, Options{Format: "pgm"})
	_, err = Decode(body, Options{Format: "pgm", MemBudget: BudgetTight})
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("应拒绝整图路径: %v", err)
	}
	if !contains(err.Error(), "输出像素缓冲") {
		t.Fatalf("应指明输出像素缓冲超限: %s", err)
	}
	// 同图分块路径可行。
	if _, err := DecodeToPGM(body, Options{Format: "pgm", MemBudget: BudgetTight}, io.Discard); err != nil {
		t.Fatalf("分块路径应可行: %v", err)
	}
	// BMP 流自身超 tight 档：分块也拒绝，指明输入缓冲。
	big := &Image{W: 600, H: 600, Pix: noisyPix(600 * 600)}
	bbody, _ := Encode(big, Options{Format: "bmp"})
	_, err = DecodeToPGM(bbody, Options{Format: "bmp", MemBudget: BudgetTight}, io.Discard)
	if !errors.Is(err, ErrBudget) || !contains(err.Error(), "输入缓冲") {
		t.Fatalf("BMP 应以输入缓冲超限拒绝，得到 %v", err)
	}
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}
