package codec

import (
	"bytes"
	"math"
	"testing"
)

func mustEncodePredict(t *testing.T, w, h int, pix []uint8, order int) ([]byte, *PredictInfo) {
	t.Helper()
	body, info, err := EncodeRLEPredict(w, h, pix, order)
	if err != nil {
		t.Fatalf("EncodeRLEPredict(%d,%d,%d): %v", w, h, order, err)
	}
	return body, info
}

// decodeAny 按 magic 分派 GTR2 / GTR1，模拟 api.Decode 的路径。
func decodeAny(t *testing.T, body []byte, cap int) (int, int, []uint8, error) {
	t.Helper()
	if len(body) >= 4 && string(body[0:4]) == "GTR2" {
		return DecodeRLEPredict(body, cap)
	}
	return DecodeRLE(body)
}

var predictFixtures = []struct {
	name string
	w, h int
	pix  []uint8
}{
	{"1x1", 1, 1, []uint8{42}},
	{"1x16", 1, 16, []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
	{"16x1", 16, 1, []uint8{0, 3, 6, 9, 12, 15, 18, 21, 24, 27, 30, 33, 36, 39, 42, 45}},
	{"flat", 8, 8, bytes.Repeat([]uint8{77}, 64)},
	{"noise", 8, 8, []uint8{
		3, 250, 1, 9, 44, 128, 7, 200,
		31, 2, 90, 77, 5, 199, 60, 12,
		8, 8, 8, 9, 9, 1, 250, 251,
		0, 255, 128, 64, 32, 16, 8, 4,
		1, 2, 3, 4, 5, 6, 7, 8,
		8, 7, 6, 5, 4, 3, 2, 1,
		99, 98, 97, 96, 95, 94, 93, 92,
		10, 20, 30, 40, 50, 60, 70, 80,
	}},
}

func TestPredictRoundTrip(t *testing.T) {
	for _, tc := range predictFixtures {
		for _, tier := range []int{1, 2, 4} {
			body, _ := mustEncodePredict(t, tc.w, tc.h, tc.pix, tier)
			for _, cap := range []int{0, tier} {
				w, h, got, err := decodeAny(t, body, cap)
				if err != nil {
					t.Fatalf("%s tier=%d cap=%d 解码失败: %v", tc.name, tier, cap, err)
				}
				if w != tc.w || h != tc.h || !bytes.Equal(got, tc.pix) {
					t.Fatalf("%s tier=%d cap=%d 往返不一致", tc.name, tier, cap)
				}
			}
		}
	}
}

func TestPredictAtInverse(t *testing.T) {
	const w, h = 32, 8
	pix := make([]uint8, w*h)
	for i := range pix {
		pix[i] = uint8(i*7 + i/3)
	}
	for s := 1; s <= MaxPredictOrder; s++ {
		for kx := 0; kx <= s; kx++ {
			c := Candidate{KX: kx, KY: s - kx}
			res := Residuals(pix, w, c)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					i := y*w + x
					if uint8(int(predictAt(pix, w, x, y, kx, s-kx))+int(res[i])) != pix[i] {
						t.Fatalf("候选 %v 下标 (%d,%d) 不互逆", c, x, y)
					}
				}
			}
		}
	}
}

func TestFirstOrderEntropy(t *testing.T) {
	cases := []struct {
		hist [256]int
		n    int
		want float64
	}{
		{[256]int{10}, 10, 0},
		{[256]int{0: 5, 1: 5}, 10, 1},
		{[256]int{0: 25, 1: 25, 2: 25, 3: 25}, 100, 2},
		{[256]int{}, 0, 0},
	}
	for _, c := range cases {
		got := FirstOrderEntropy(c.hist, c.n)
		if math.Abs(got-c.want) > 1e-12 {
			t.Fatalf("熵计算 %v/%d = %v，期望 %v", c.hist, c.n, got, c.want)
		}
	}
}

func TestSelectPredictMinEntropy(t *testing.T) {
	// 每行是常数 3+3y：竖直一阶预测（取正上方像素）把整幅图压成
	// 常数 3（首行预测 0，而首行值恰为 3），残差熵严格为 0；
	// 不预测有 16 个符号（4 位），因此 x0/y1 是唯一最优。
	const w, h = 64, 16
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8(3 + y*3)
		}
	}
	sel, stats := SelectPredict(pix, w, 4)
	want := len(candidates(4))
	if len(stats) != want {
		t.Fatalf("候选数 %d，期望 %d", len(stats), want)
	}
	best := math.Inf(1)
	for _, st := range stats {
		if st.Samples != w*h {
			t.Fatalf("统计区间应为全图 %d，得到 %d", w*h, st.Samples)
		}
		if st.Entropy < best {
			best = st.Entropy
		}
	}
	if math.Abs(best-entropyOf(sel, stats)) > 1e-12 {
		t.Fatalf("选中的 %v 不是熵最小项", sel)
	}
	if best > 1e-9 {
		t.Fatalf("竖直轴一阶应把逐行相同图像压成零熵，得到 %v", best)
	}
	if sel.String() != "x0/y1" {
		t.Fatalf("期望 x0/y1，得到 %v", sel)
	}
}

func entropyOf(c Candidate, stats []OrderStat) float64 {
	for _, st := range stats {
		if st.Candidate == c {
			return st.Entropy
		}
	}
	return math.NaN()
}

func TestSelectIncludesNoPredict(t *testing.T) {
	pix := make([]uint8, 64)
	for i := range pix {
		pix[i] = uint8(i*37 + 11)
	}
	_, stats := SelectPredict(pix, 8, 2)
	if stats[0].Candidate.KX != 0 || stats[0].Candidate.KY != 0 {
		t.Fatalf("候选集首项应为不预测，得到 %v", stats[0].Candidate)
	}
}

func TestRunsRoundTrip(t *testing.T) {
	seqs := [][]uint8{
		nil,
		{0},
		{7, 7},
		bytes.Repeat([]uint8{9}, 200),
		bytes.Repeat([]uint8{1}, 70000),
		{0, 1, 0, 1, 2, 2, 2, 3, 4, 5, 5},
	}
	for i := 0; i < 200; i++ {
		s := make([]uint8, i*3)
		x := uint32(i)
		for j := range s {
			x = x*1664525 + 1013904223
			s[j] = uint8(x >> 24)
			if j%7 == 0 && j > 0 {
				s[j] = s[j-1]
			}
		}
		seqs = append(seqs, s)
	}
	for _, seq := range seqs {
		enc := appendRuns(nil, seq)
		got, err := readRuns(enc, 0, len(seq))
		if err != nil {
			t.Fatalf("读回失败 len=%d: %v", len(seq), err)
		}
		if !bytes.Equal(got, seq) {
			t.Fatalf("游程往返不一致 len=%d", len(seq))
		}
	}
}

func TestDecodeGTR2Rejects(t *testing.T) {
	// 行内线性递增：一阶残差恒为 2，预测后必发 GTR2。
	const w, h = 32, 8
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8(x * 2)
		}
	}
	base, _ := mustEncodePredict(t, w, h, pix, 2)
	if string(base[0:4]) != "GTR2" {
		t.Fatalf("线性渐变应发出 GTR2，得到 %q", base[0:4])
	}
	bad := [][]byte{
		nil,
		base[:8],
		base[:len(base)-1],
	}
	for _, mut := range []func([]byte){
		func(b []byte) { b[8] = 0 },                          // 总阶数为 0
		func(b []byte) { b[8] = MaxPredictOrder + 1 },        // 水平阶越界
		func(b []byte) { b[8], b[9] = 0, MaxPredictOrder+1 }, // 竖直阶越界
		func(b []byte) { b[9] = 200 },                        // 竖直阶远超上限
	} {
		b := append([]byte{}, base...)
		mut(b)
		bad = append(bad, b)
	}
	for i, b := range bad {
		if _, _, _, err := DecodeRLEPredict(b, 0); err == nil {
			t.Fatalf("第 %d 个坏流应报错", i)
		}
	}
	capped := append([]byte{}, base...)
	capped[8] = 2
	if _, _, _, err := DecodeRLEPredict(capped, 1); err == nil {
		t.Fatal("流内阶数超过上限应报错")
	}
}

func TestPredictNeverWorseThanBaseline(t *testing.T) {
	for _, tc := range predictFixtures {
		base, err := EncodeRLE(tc.w, tc.h, tc.pix)
		if err != nil {
			t.Fatal(err)
		}
		for _, tier := range []int{1, 2, 4} {
			body, info := mustEncodePredict(t, tc.w, tc.h, tc.pix, tier)
			if len(body) > len(base) {
				t.Fatalf("%s tier=%d 预测后更大: %d > %d", tc.name, tier, len(body), len(base))
			}
			if info.FallbackToGTR && !bytes.Equal(body, base) {
				t.Fatalf("%s tier=%d fallback 流应与基线逐字节一致", tc.name, tier)
			}
		}
	}
}

func TestPredictOrderBounds(t *testing.T) {
	pix := []uint8{1, 2, 3, 4}
	for _, order := range []int{-1, 0, MaxPredictOrder + 1} {
		if _, _, err := EncodeRLEPredict(2, 2, pix, order); err == nil {
			t.Fatalf("阶数 %d 应报错", order)
		}
	}
}

func TestStreamPredictOrder(t *testing.T) {
	pix := make([]uint8, 16*4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 16; x++ {
			pix[y*16+x] = uint8(x * 5)
		}
	}
	body, info := mustEncodePredict(t, 16, 4, pix, 4)
	if string(body[0:4]) != "GTR2" {
		t.Fatalf("应发出 GTR2")
	}
	kx, ky := StreamPredict(body)
	if kx != info.Emitted.KX || ky != info.Emitted.KY {
		t.Fatalf("流内 %d/%d，期望 %v", kx, ky, info.Emitted)
	}
}
