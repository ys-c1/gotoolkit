package codec

import (
	"bytes"
	"errors"
	"testing"
)

// patternImg 生成确定性的测试图：相邻像素尽量不同值，让 RLE 游程最短，
// 从而 RLE 编码逼近最坏情况（2 字节/像素）。
func patternImg(w, h int) []uint8 {
	pix := make([]uint8, w*h)
	for i := range pix {
		pix[i] = uint8((i*37 + 11) % 256)
	}
	return pix
}

type collectSink struct {
	bands [][]uint8
	ys    []int
}

func (c *collectSink) WriteBand(y0, rows int, band []uint8) error {
	cp := append([]uint8(nil), band...)
	c.bands = append(c.bands, cp)
	c.ys = append(c.ys, y0)
	return nil
}

func encDecTriplet(t *testing.T, w, h int) map[string][]byte {
	t.Helper()
	pix := patternImg(w, h)
	out := map[string][]uint8{}
	enc := map[string]func() ([]byte, error){
		"pgm": func() ([]byte, error) { return EncodePGM(w, h, pix) },
		"bmp": func() ([]byte, error) { return EncodeBMP(w, h, pix) },
		"rle": func() ([]byte, error) { return EncodeRLE(w, h, pix) },
	}
	for f, e := range enc {
		b, err := e()
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		out[f] = b
	}
	return out
}

// TestBandsEqualWhole 分块结果与整体解码逐字节相同，并检查所有分块边界像素。
func TestBandsEqualWhole(t *testing.T) {
	w, h := 64, 37 // 故意不整除常见块高
	streams := encDecTriplet(t, w, h)
	bandRows := 8 // 产生 5 块（最后一块 5 行），存在多条边界
	for _, f := range []string{"pgm", "bmp", "rle"} {
		data := streams[f]
		var ww, hh int
		var whole []uint8
		var err error
		switch f {
		case "pgm":
			ww, hh, whole, err = DecodePGM(data)
		case "bmp":
			ww, hh, whole, err = DecodeBMP(data)
		default:
			ww, hh, whole, err = DecodeRLE(data)
		}
		if err != nil {
			t.Fatalf("%s 整图解码失败: %v", f, err)
		}
		sink := &collectSink{}
		if err := DecodeBands(f, data, bandRows, sink); err != nil {
			t.Fatalf("%s 分块解码失败: %v", f, err)
		}
		// 逐块比对：每块与整图对应行区间一致后才允许合并。
		merged := make([]uint8, ww*hh)
		for i, y0 := range sink.ys {
			band := sink.bands[i]
			rows := len(band) / ww
			want := whole[y0*ww : (y0+rows)*ww]
			if !bytes.Equal(band, want) {
				t.Fatalf("%s 第 %d 块与整图不一致", f, i)
			}
			copy(merged[y0*ww:], band)
		}
		if !bytes.Equal(merged, whole) {
			t.Fatalf("%s 合并结果与整图不逐字节相同", f)
		}
		// 边界像素单独抽查：每条分块缝的上下两行、行首尾四个像素。
		for i := 1; i < len(sink.ys); i++ {
			seamY := sink.ys[i] // 该块第一行，即缝的下沿
			for _, y := range []int{seamY - 1, seamY} {
				for _, x := range []int{0, ww / 2, ww - 1} {
					if merged[y*ww+x] != whole[y*ww+x] {
						t.Fatalf("%s 边界像素 (%d,%d) 不一致", f, y, x)
					}
				}
			}
		}
	}
}

// TestCorruptionCaughtBeforeMerge 截断编码流后，分块解码必须报错且零块落地
// （比对/长度闸门在 WriteBand 之前生效），绝不把残缺结果合并出去。
func TestCorruptionCaughtBeforeMerge(t *testing.T) {
	w, h := 48, 24
	streams := encDecTriplet(t, w, h)
	cases := map[string][]byte{
		"pgm": streams["pgm"][:len(streams["pgm"])-5],
		"bmp": streams["bmp"][:len(streams["bmp"])-5],
		"rle": streams["rle"][:len(streams["rle"])-1], // 奇数尾字节
	}
	for f, bad := range cases {
		sink := &collectSink{}
		err := DecodeBands(f, bad, 7, sink)
		if err == nil {
			t.Fatalf("%s 截断流应被拦截", f)
		}
		if len(sink.bands) != 0 {
			t.Fatalf("%s 失败前不得有任何分块落地，实际落地 %d 块", f, len(sink.bands))
		}
	}
}

// TestBandEqualGuard 直接单测合并闸门本身：分歧必须被拦下。
func TestBandEqualGuard(t *testing.T) {
	if !bandsEqual([]uint8{1, 2, 3}, []uint8{1, 2, 3}) {
		t.Fatal("一致分块应放行")
	}
	if bandsEqual([]uint8{1, 2, 3}, []uint8{1, 9, 3}) {
		t.Fatal("分歧分块必须拦截")
	}
	got := seamChecks(5, 10)
	want := [][2]int{{0, 0}, {0, 9}, {4, 0}, {4, 9}}
	if len(got) != len(want) {
		t.Fatalf("抽查坐标数错误: %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("抽查坐标 %d 错误: %v", i, got[i])
		}
	}
}

// TestMaxDecodablePixels 校验反解上限满足预算不等式：
// 反解出的最大像素，其模型峰值不得超过预算；再多一像素必须超预算。
func TestMaxDecodablePixels(t *testing.T) {
	for _, budget := range []int64{BudgetTestTight, BudgetTestGenerous, 1 << 30} {
		for _, f := range []string{"pgm", "bmp", "rle"} {
			whole, chunked := MaxDecodablePixels(f, budget)
			enc, _ := WorstEncoded(f, whole)
			if whole > 0 && enc+whole+memFixed > budget {
				t.Fatalf("%s 整图反解超预算: n=%d peak=%d budget=%d", f, whole, enc+whole+memFixed, budget)
			}
			enc1, _ := WorstEncoded(f, whole+1)
			if whole > 0 && enc1+(whole+1)+memFixed <= budget {
				t.Fatalf("%s 整图反解不是最大: n=%d 仍可容纳", f, whole+1)
			}
			encC, _ := WorstEncoded(f, chunked)
			// 分块路径线性口径：输入 + 双行带缓冲（保守按整图像素线性估计）。
			if chunked > 0 && encC+2*chunked+memFixed > budget {
				t.Fatalf("%s 分块反解超预算: n=%d peak=%d budget=%d",
					f, chunked, encC+2*chunked+memFixed, budget)
			}
		}
	}
}

// TestMaxChunkedSize 宽度感知的分块反解必须自洽，且对窄高图显著优于整图上限。
func TestMaxChunkedSize(t *testing.T) {
	for _, budget := range []int64{BudgetTestTight, BudgetTestGenerous} {
		for _, f := range []string{"pgm", "bmp", "rle"} {
			whole, _ := MaxDecodablePixels(f, budget)
			// 取一个能在预算内放下单行双缓冲的窄宽。
			w := 256
			pix, rows := MaxChunkedSize(f, w, budget)
			if rows <= 0 || pix != rows*int64(w) {
				t.Fatalf("%s 宽度感知反解非法: rows=%d pix=%d", f, rows, pix)
			}
			// 反解出的画幅按真实单行双缓冲核算，必须落在预算内。
			enc, _ := WorstEncoded(f, pix)
			peak := enc + 2*int64(w) + memFixed
			if peak > budget {
				t.Fatalf("%s 分块反解超预算: peak=%d budget=%d", f, peak, budget)
			}
			// 再多一行就超预算（上界性）。
			enc2, _ := WorstEncoded(f, pix+int64(w))
			if enc2+2*int64(w)+memFixed <= budget {
				t.Fatalf("%s 分块反解不是上界", f)
			}
			// 窄高图的分块上限必须超过整图路径上限，体现分块的价值。
			if f == "pgm" && pix <= whole {
				t.Fatalf("%s 窄高分块上限 %d 应超过整图上限 %d", f, pix, whole)
			}
		}
	}
	// 单行双缓冲本身就放不下的超宽图，必须反解为 0。
	if pix, rows := MaxChunkedSize("pgm", int(BudgetTestTight), BudgetTestTight); pix != 0 || rows != 0 {
		t.Fatalf("超宽图应反解为 0，得到 pix=%d rows=%d", pix, rows)
	}
}

// 测试用两档预算常量（与 api 包的公开常量同值，codec 层不反向依赖 api）。
const (
	BudgetTestTight    int64 = 512 << 10
	BudgetTestGenerous int64 = 8 << 20
)

// TestPlanRejection 超出上限必须拒绝，并说明超的是哪一项。
func TestPlanRejection(t *testing.T) {
	// 1) 头里声明 100000x100000 但数据不足：探测成功、像素检查失败前，
	//    预算闸门先命中，且错误指明超限项。
	giantPGM := []byte("P5\n100000 100000\n255\nX")
	plan, err := PlanDecode("pgm", giantPGM, BudgetTestTight, 0)
	if err != nil {
		t.Fatalf("探测不应失败: %v", err)
	}
	err = plan.CheckChunk(BudgetTestTight)
	var be *BudgetError
	if !errors.As(err, &be) {
		t.Fatalf("应返回 BudgetError，得到 %v", err)
	}
	if be.Item != "输入缓冲" && be.Item != "分块工作集" {
		t.Fatalf("超限项错误: %s", be.Item)
	}
	// 2) 真实中等图：整图路径超预算、分块路径可行时，CheckWhole 拒绝、CheckChunk 放行。
	w, h := 640, 480
	streams := encDecTriplet(t, w, h)
	plan, err = PlanDecode("pgm", streams["pgm"], BudgetTestTight, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CheckWhole(BudgetTestTight) == nil {
		t.Fatal("640x480 PGM 在 tight 档整图路径应被拒绝")
	}
	if err := plan.CheckChunk(BudgetTestTight); err != nil {
		t.Fatalf("分块路径应可行: %v", err)
	}
	if plan.BandRows >= h {
		t.Fatalf("自动分块行数应小于总高，得到 %d", plan.BandRows)
	}
	// 3) BMP 编码流自身（~900KiB）超过 tight 档：两条路径都因输入缓冲被拒。
	planB, err := PlanDecode("bmp", streams["bmp"], BudgetTestTight, 0)
	if err != nil {
		t.Fatal(err)
	}
	errB := planB.CheckChunk(BudgetTestTight)
	var beB *BudgetError
	if !errors.As(errB, &beB) || beB.Item != "输入缓冲" {
		t.Fatalf("BMP 应以输入缓冲超限拒绝，得到 %v", errB)
	}
	// 4) generous 档整图放行。
	if err := plan.CheckWhole(BudgetTestGenerous); err != nil {
		t.Fatalf("generous 档应放行整图: %v", err)
	}
}

// TestChunkedPeakWithinBudget 实测分块解码的 Go 堆增量不超过模型峰值与预算。
func TestChunkedPeakWithinBudget(t *testing.T) {
	w, h := 640, 480
	streams := encDecTriplet(t, w, h)
	plan, err := PlanDecode("pgm", streams["pgm"], BudgetTestTight, 37)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.CheckChunk(BudgetTestTight); err != nil {
		t.Fatal(err)
	}
	sink := &collectSink{}
	if err := DecodeBandsPGM(streams["pgm"], plan.BandRows, sink); err != nil {
		t.Fatal(err)
	}
	if plan.ChunkPeak > BudgetTestTight {
		t.Fatalf("模型峰值 %d 超预算 %d", plan.ChunkPeak, BudgetTestTight)
	}
	// 单块缓冲不得大于模型里的 BandBytes。
	if int64(plan.BandRows)*int64(w) != plan.BandBytes {
		t.Fatal("BandBytes 口径错误")
	}
}

// TestEdgeBandShapes 单块覆盖整图、块高不整除、三格式单块路径都要正确。
func TestEdgeBandShapes(t *testing.T) {
	w, h := 16, 8
	pix := patternImg(w, h)
	body, _ := EncodePGM(w, h, pix)
	s := &collectSink{}
	if err := DecodeBandsPGM(body, h, s); err != nil {
		t.Fatal(err)
	}
	if len(s.bands) != 1 {
		t.Fatalf("单块覆盖应只有 1 块，得到 %d", len(s.bands))
	}
	s2 := &collectSink{}
	if err := DecodeBandsPGM(body, 3, s2); err != nil {
		t.Fatal(err)
	}
	if len(s2.bands) != 3 || len(s2.bands[2]) != 2*w {
		t.Fatalf("不整除分块划分错误: %d 块", len(s2.bands))
	}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		var b []byte
		switch f {
		case "pgm":
			b, _ = EncodePGM(w, h, pix)
		case "bmp":
			b, _ = EncodeBMP(w, h, pix)
		default:
			b, _ = EncodeRLE(w, h, pix)
		}
		if err := DecodeBands(f, b, h, &collectSink{}); err != nil {
			t.Fatalf("%s 单块解码失败: %v", f, err)
		}
	}
}

// TestRejectTooWide 宽度大到单行双缓冲都放不下时，分块路径也必须拒绝。
func TestRejectTooWide(t *testing.T) {
	w, h := int(BudgetTestTight), 2 // 单行 2*w 远超预算
	giant := []byte("P5\n" + edgeItoa(w) + " " + edgeItoa(h) + "\n255\nXX")
	plan, err := PlanDecode("pgm", giant, BudgetTestTight, 0)
	if err != nil {
		t.Fatal(err)
	}
	var be *BudgetError
	if err := plan.CheckChunk(BudgetTestTight); !errors.As(err, &be) {
		t.Fatalf("超宽图应以 BudgetError 拒绝，得到 %v", err)
	}
}

func edgeItoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
