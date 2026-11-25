package api

import (
	"fmt"
	"sort"
	"testing"
)

// maxForDepth 返回 n 位满量程 2^n-1。
func maxForDepth(n int) int { return 1<<n - 1 }

// safeMax 返回正变换不饱和的最大输入：x·(100+g)/100 ≤ max。
// g ≤ 0 时全域安全；g > 0 时只有 x ≤ ⌊max·100/(100+g)⌋ 属于恒等域。
func safeMax(g, max int) int {
	if g <= 0 {
		return max
	}
	return max * 100 / (100 + g)
}

// errBoundLSB 是量化往返误差界 |Δ| ≤ ⌊(b+1)/2⌋（LSB），b = 100/(100+g)。
// (b+1)/2 = (200+g) / (2(100+g))，与位深无关；相对满量程的界随位深每位约减半。
func errBoundLSB(g int) int {
	return (200 + g) / (2 * (100 + g))
}

type tripStat struct {
	maxErr  int
	meanErr float64
	hist    map[int]int // 有符号误差 -> 像素数
}

func roundTripStat(pix []int, g, max int) tripStat {
	st := tripStat{hist: map[int]int{}}
	sum := 0
	for _, x := range pix {
		d := gainInv(gainFwd(x, g, max), g, max) - x
		st.hist[d]++
		ad := d
		if ad < 0 {
			ad = -ad
		}
		if ad > st.maxErr {
			st.maxErr = ad
		}
		sum += ad
	}
	st.meanErr = float64(sum) / float64(len(pix))
	return st
}

func (st tripStat) histString() string {
	keys := make([]int, 0, len(st.hist))
	for k := range st.hist {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("%+d:%d", k, st.hist[k])
	}
	return out
}

// TestGainRoundTripBound 三档位深 × 纯色/渐变，验证往返误差落在 ⌊(b+1)/2⌋ LSB 界内。
func TestGainRoundTripBound(t *testing.T) {
	depths := []int{8, 10, 12}
	gains := []int{20, -50, -90}
	const size = 64 * 64
	for _, g := range gains {
		bound := errBoundLSB(g)
		for _, n := range depths {
			max := maxForDepth(n)
			sm := safeMax(g, max)
			solid := make([]int, size)
			for i := range solid {
				solid[i] = sm / 2
			}
			grad := make([]int, size)
			for i := range grad {
				grad[i] = i % (sm + 1)
			}
			cases := []struct {
				name string
				pix  []int
			}{{"纯色", solid}, {"渐变", grad}}
			for _, c := range cases {
				st := roundTripStat(c.pix, g, max)
				if st.maxErr > bound {
					t.Errorf("g=%+d n=%d %s: maxErr=%d 超出界 %d", g, n, c.name, st.maxErr, bound)
				}
				t.Logf("g=%+d n=%2d %s max=%d mean=%.4f bound=%d hist{%s}",
					g, n, c.name, st.maxErr, st.meanErr, bound, st.histString())
			}
		}
	}
}

// TestGainRoundTripAPI 走真实 Encode/Decode 路径（8 位 PGM），误差同样必须在界内。
func TestGainRoundTripAPI(t *testing.T) {
	for _, g := range []int{20, -50, -90} {
		img := &Image{W: 64, H: 64, Pix: make([]uint8, 64*64)}
		sm := safeMax(g, 255)
		for i := range img.Pix {
			img.Pix[i] = uint8(i % (sm + 1))
		}
		body, err := Encode(img, Options{Format: "pgm", Gain: g})
		if err != nil {
			t.Fatalf("g=%+d 编码失败: %v", g, err)
		}
		got, err := Decode(body, Options{Format: "pgm", Gain: g})
		if err != nil {
			t.Fatalf("g=%+d 解码失败: %v", g, err)
		}
		maxErr := 0
		for i, v := range got.Pix {
			d := int(v) - int(img.Pix[i])
			if d < 0 {
				d = -d
			}
			if d > maxErr {
				maxErr = d
			}
		}
		if bound := errBoundLSB(g); maxErr > bound {
			t.Errorf("g=%+d API 往返 maxErr=%d 超出界 %d", g, maxErr, bound)
		}
	}
}

// TestDecodeGainMinus100 g=-100 时正变换斜率为 0，不可逆，Decode 必须报错。
func TestDecodeGainMinus100(t *testing.T) {
	body, err := Encode(sample(), Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(body, Options{Format: "pgm", Gain: -100}); err == nil {
		t.Fatal("Gain=-100 不可逆，应报错")
	}
}
