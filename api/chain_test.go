package api

import (
	"bytes"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func chainFixture() []*Image {
	const w, h = 4, 3
	speeds := []int{4, 3, 5, 2, 6, 1, 7, 0, 4, 3, 5, 2}
	frames := make([]*Image, 24)
	for t := range frames {
		pix := make([]uint8, w*h)
		for i := range pix {
			v := i*7 + t*speeds[i]
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			pix[i] = uint8(v)
		}
		frames[t] = &Image{W: w, H: h, Pix: pix}
	}
	return frames
}

func TestKeyframeIntervalInversionAndRounding(t *testing.T) {
	cases := []struct {
		budget, q, want int
	}{
		{0, 8, 1},
		{3, 8, 1},  // floor(3/4)=0：向下取整，不能四舍五入成 K=2
		{4, 8, 2},  // 1+floor(1)
		{7, 8, 2},  // 仍是 2；K=3 会给上界 8 > 7
		{8, 8, 3},  // 1+floor(2)
		{10, 8, 3}, // floor(2.5)=2
		{12, 8, 4},
		{1, 1, 1}, // q=1 无损，保守返回全关键帧
	}
	for _, c := range cases {
		if got := KeyframeIntervalForBudget(c.budget, c.q); got != c.want {
			t.Fatalf("KeyframeIntervalForBudget(%d,%d)=%d, want %d", c.budget, c.q, got, c.want)
		}
	}
}

func TestAllKeyframesMatchCurrentEncoding(t *testing.T) {
	frames := chainFixture()
	for _, format := range []string{"pgm", "bmp", "rle"} {
		blobs, err := EncodeSequence(frames, Options{Format: format, KeyframeInterval: 1, QuantStep: 8})
		if err != nil {
			t.Fatal(err)
		}
		if len(blobs) != len(frames) {
			t.Fatalf("%s: 帧数 %d，产物数 %d", format, len(frames), len(blobs))
		}
		for i, f := range frames {
			want, err := Encode(f, Options{Format: format})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(blobs[i], want) {
				t.Fatalf("%s 第 %d 帧与当前单帧编码不逐字节一致", format, i)
			}
		}

		// 第 0 帧就是 api_test.go 的锁定 sample，必须与三份 golden 一致。
		golden, err := os.ReadFile(filepath.Join("..", "testdata", "golden", format+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(blobs[0], golden) {
			t.Fatalf("%s 第 0 帧偏离 golden", format)
		}
	}
}

func TestChainBoundsAndKnownLastPixels(t *testing.T) {
	frames := chainFixture()
	cases := []struct {
		k, budget, wantMax int
		wantLast           []int
	}{
		{2, 4, 4, []int{4, -3, 3, -2, 2, -1, 1, 0, 4, -3, 3, -2}},
		{3, 8, 8, []int{8, -6, 6, -4, 4, -2, 2, 0, 8, -6, 6, -4}},
		{4, 12, 12, []int{12, -9, 9, -6, 6, -3, 3, 0, 12, -9, 9, -6}},
	}
	for _, tc := range cases {
		blobs, decoded, rep, err := AnalyzeSequence(frames, Options{
			Format: "pgm", KeyframeInterval: tc.k, QuantStep: 8,
		})
		if err != nil {
			t.Fatalf("K=%d: %v", tc.k, err)
		}
		if len(blobs) != len(frames) || len(decoded) != len(frames) {
			t.Fatalf("K=%d: 产物/解码帧数错误", tc.k)
		}
		if got := KeyframeIntervalForBudget(tc.budget, 8); got != tc.k {
			t.Fatalf("预算 %d 反解 K=%d, want %d", tc.budget, got, tc.k)
		}
		if !rep.WithinBound || rep.LastActualMaxAbs != tc.wantMax {
			t.Fatalf("K=%d: max=%d bound=%d within=%v", tc.k, rep.LastActualMaxAbs, rep.LastTheoryBound, rep.WithinBound)
		}
		if rep.LastTheoryBound != tc.budget {
			t.Fatalf("K=%d: theory bound=%d, want %d", tc.k, rep.LastTheoryBound, tc.budget)
		}
		last := frames[len(frames)-1]
		gotPixels := make([]int, len(last.Pix))
		for i := range last.Pix {
			gotPixels[i] = int(decoded[len(frames)-1].Pix[i]) - int(last.Pix[i])
			if math.Abs(float64(gotPixels[i])) > float64(tc.budget) {
				t.Fatalf("K=%d pixel %d signed error %d exceeds bound %d", tc.k, i, gotPixels[i], tc.budget)
			}
		}
		for i, want := range tc.wantLast {
			if gotPixels[i] != want {
				t.Fatalf("K=%d pixel %d error=%d, want %d (all=%v)", tc.k, i, gotPixels[i], want, gotPixels)
			}
		}

		for _, m := range rep.FrameMetrics {
			if m.ActualMaxAbs > m.TheoryBound {
				t.Fatalf("K=%d frame %d actual %d > theory bound %d", tc.k, m.Frame, m.ActualMaxAbs, m.TheoryBound)
			}
			if m.ActualMaxAbs > m.TriangleBound {
				t.Fatalf("K=%d frame %d actual %d > measured triangle bound %d", tc.k, m.Frame, m.ActualMaxAbs, m.TriangleBound)
			}
			if m.Keyframe && (m.ActualMaxAbs != 0 || m.TheoryBound != 0) {
				t.Fatalf("K=%d keyframe %d not exact", tc.k, m.Frame)
			}
		}
	}
}

func TestChainBoundHoldsOnRandomSequences(t *testing.T) {
	rng := rand.New(rand.NewSource(20261125))
	const w, h, nFrames = 7, 5, 40
	for _, q := range []int{2, 3, 8, 33} {
		for _, k := range []int{1, 2, 5, 13} {
			frames := make([]*Image, nFrames)
			pix := make([]uint8, w*h)
			for i := range pix {
				pix[i] = uint8(rng.Intn(256))
			}
			for f := range frames {
				if f > 0 {
					for i := range pix {
						v := int(pix[i]) + rng.Intn(41) - 20
						if v < 0 {
							v = 0
						} else if v > 255 {
							v = 255
						}
						pix[i] = uint8(v)
					}
				}
				cp := append([]uint8(nil), pix...)
				frames[f] = &Image{W: w, H: h, Pix: cp}
			}

			_, _, rep, err := AnalyzeSequence(frames, Options{
				Format: "pgm", KeyframeInterval: k, QuantStep: q,
			})
			if err != nil {
				t.Fatalf("q=%d k=%d: %v", q, k, err)
			}
			for _, m := range rep.FrameMetrics {
				if m.ActualMaxAbs > m.TheoryBound {
					t.Fatalf("q=%d k=%d frame=%d actual %d > theory %d", q, k, m.Frame, m.ActualMaxAbs, m.TheoryBound)
				}
				if m.ActualMaxAbs > m.TriangleBound {
					t.Fatalf("q=%d k=%d frame=%d actual %d > triangle %d", q, k, m.Frame, m.ActualMaxAbs, m.TriangleBound)
				}
			}
		}
	}
}

func TestOversizedIntervalIsDetectedAgainstBudget(t *testing.T) {
	frames := chainFixture()
	_, _, rep, err := AnalyzeSequence(frames, Options{Format: "pgm", KeyframeInterval: 8, QuantStep: 8})
	if err != nil {
		t.Fatal(err)
	}
	// 数学上界随深度变为 7*4=28，实测最坏像素恰好到 28；
	// 但目标预算仍是 12，因此报告必须能由调用方标出“间隔取大”。
	if rep.LastTheoryBound != 28 || rep.LastActualMaxAbs != 28 || !rep.WithinBound {
		t.Fatalf("负对照异常: theory=%d actual=%d withinTheory=%v", rep.LastTheoryBound, rep.LastActualMaxAbs, rep.WithinBound)
	}
	const targetBudget = 12
	if rep.LastActualMaxAbs <= targetBudget {
		t.Fatal("负对照应当超过目标预算")
	}
}
