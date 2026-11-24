package codec

import (
	"bytes"
	"math"
	"testing"
)

func jointInputs() []struct {
	name string
	w, h int
	pix  []uint8
} {
	// 确定性伪随机，避免引入 math/rand 种子争议。
	noise := make([]uint8, 37*13)
	s := uint32(1234567)
	for i := range noise {
		s = s*1664525 + 1013904223
		noise[i] = uint8(s >> 16)
	}
	grad := make([]uint8, 64*8)
	for y := 0; y < 8; y++ {
		for x := 0; x < 64; x++ {
			grad[y*64+x] = uint8(x * 4)
		}
	}
	return []struct {
		name string
		w, h int
		pix  []uint8
	}{
		{"constant", 16, 4, bytes.Repeat([]byte{7}, 64)},
		{"gradient", 64, 8, grad},
		{"noise", 37, 13, noise},
		{"tiny", 4, 3, []uint8{0, 7, 14, 21, 28, 35, 42, 49, 56, 63, 70, 77}},
	}
}

func TestJointRoundTripAllCombos(t *testing.T) {
	combos := []struct {
		pred  string
		block int
	}{
		{PredictNone, 0},
		{PredictLeft, 0},
		{PredictNone, 16},
		{PredictLeft, 16},
		{PredictLeft, 7},   // 不整除，末块短
		{PredictNone, 1},   // 每符号一块的极端档
		{PredictLeft, 999}, // 大于 N，退化为整幅
	}
	for _, in := range jointInputs() {
		for _, c := range combos {
			body, err := EncodeJoint(in.w, in.h, in.pix, c.pred, c.block)
			if err != nil {
				t.Fatalf("%s %v 编码失败: %v", in.name, c, err)
			}
			w, h, got, err := DecodeJoint(body)
			if err != nil {
				t.Fatalf("%s %v 解码失败: %v", in.name, c, err)
			}
			if w != in.w || h != in.h || !bytes.Equal(got, in.pix) {
				t.Fatalf("%s %v 往返不一致", in.name, c)
			}
		}
	}
}

func TestJointCurrentComboEqualsGTR1(t *testing.T) {
	for _, in := range jointInputs() {
		got, err := EncodeJoint(in.w, in.h, in.pix, PredictNone, 0)
		if err != nil {
			t.Fatal(err)
		}
		want, err := EncodeRLE(in.w, in.h, in.pix)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 当前组合必须逐字节等于 GTR1", in.name)
		}
		// 空串预测器与 none 等价。
		got2, err := EncodeJoint(in.w, in.h, in.pix, "", 0)
		if err != nil || !bytes.Equal(got2, want) {
			t.Fatalf("空 Predict 应等价 none 且回落 GTR1")
		}
	}
}

func TestJointRejectsBadArgs(t *testing.T) {
	pix := []uint8{1, 2, 3, 4}
	if _, err := EncodeJoint(2, 2, pix, "paeth", 0); err == nil {
		t.Fatal("未知预测器应报错")
	}
	if _, err := EncodeJoint(2, 2, pix, PredictNone, -3); err == nil {
		t.Fatal("负块长应报错")
	}
	if _, err := AnalyzeJoint(2, 2, pix, "avg", 0); err == nil {
		t.Fatal("Analyze 未知预测器应报错")
	}
	if _, err := EncodeJoint(2, 2, []uint8{1}, PredictNone, 0); err == nil {
		t.Fatal("像素数不匹配应报错")
	}
}

func TestEntropyKnownValues(t *testing.T) {
	// 8 个相同符号：H=0。
	var h0 [256]int
	h0[9] = 8
	if got := FirstOrderEntropyJoint(h0, 8); math.Abs(got) > 1e-12 {
		t.Fatalf("常值熵应为 0, got %v", got)
	}
	// 4/4 两个等概符号：H=1。
	var h1 [256]int
	h1[0], h1[1] = 4, 4
	if got := FirstOrderEntropyJoint(h1, 8); math.Abs(got-1) > 1e-12 {
		t.Fatalf("等概二符号熵应为 1, got %v", got)
	}
}

func TestAnalyzeJointBlockCountAndCost(t *testing.T) {
	pix := make([]uint8, 1000)
	for i := range pix {
		pix[i] = uint8(i % 251)
	}
	st, err := AnalyzeJoint(100, 10, pix, PredictLeft, 128)
	if err != nil {
		t.Fatal(err)
	}
	if st.Blocks != 8 { // ceil(1000/128)=8
		t.Fatalf("块数错误: %d", st.Blocks)
	}
	if len(st.BlockStats) != 8 || st.BlockStats[7].N != 104 {
		t.Fatalf("末块长度错误: %+v", st.BlockStats[7])
	}
	wantHeader := float64(8 * BlockHeaderSize * 8)
	if st.HeaderBits != wantHeader {
		t.Fatalf("块头比特错误: %v != %v", st.HeaderBits, wantHeader)
	}
	if math.Abs(st.CostBits-(st.EntropyBits+st.HeaderBits)) > 1e-9 {
		t.Fatal("代价合计错误")
	}
	if st.ModelBytes != int(math.Ceil(st.CostBits/8)) {
		t.Fatal("ModelBytes 应为 ceil(CostBits/8)")
	}
}

func TestSolveJointPicksMinimum(t *testing.T) {
	mk := func(pred string, cost float64, blocks int) JointStat {
		return JointStat{Predict: pred, CostBits: cost, Blocks: blocks}
	}
	stats := []JointStat{
		mk(PredictNone, 100, 4),
		mk(PredictLeft, 90, 1),
		mk(PredictLeft, 95, 8),
	}
	best, idx := SolveJoint(stats)
	if idx != 1 || best.CostBits != 90 {
		t.Fatalf("应选代价 90 的组合, idx=%d best=%+v", idx, best)
	}
}

func TestDecodeJointRejectsGarbage(t *testing.T) {
	if _, _, _, err := DecodeJoint([]byte("XXXX")); err == nil {
		t.Fatal("未知 magic 应报错")
	}
	if _, _, _, err := DecodeJoint([]byte("GTR3")); err == nil {
		t.Fatal("截断流应报错")
	}
}
