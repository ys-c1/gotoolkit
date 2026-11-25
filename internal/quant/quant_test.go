package quant

import (
	"math"
	"testing"
)

func TestStepAndValidation(t *testing.T) {
	cases := map[int]int{2: 2, 3: 4, 4: 8, 5: 16, 8: 128}
	for b, want := range cases {
		got, err := Step(b)
		if err != nil || got != want {
			t.Fatalf("Step(%d) = %d, %v; 期望 %d", b, got, err, want)
		}
		th, err := TheoreticalEntropy(b)
		if err != nil || th != float64(b-1) {
			t.Fatalf("理论熵(%d) = %v, %v; 期望 %d", b, th, err, b-1)
		}
	}
	for _, b := range []int{1, 9, 0, -1} {
		if _, err := Step(b); err == nil {
			t.Fatalf("档位 %d 应报错", b)
		}
	}
}

func TestReduceTruncation(t *testing.T) {
	// Δ=4 截断：量化后取 4 的倍数（向下）
	pix := []uint8{0, 1, 2, 3, 4, 5, 6, 7, 8}
	got, err := Reduce(pix, 3) // d=2, Δ=4
	if err != nil {
		t.Fatal(err)
	}
	want := []uint8{0, 0, 0, 0, 4, 4, 4, 4, 8}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Reduce 位置 %d = %d，期望 %d", i, got[i], want[i])
		}
	}
}

func TestErrorIsDiscardedLowBits(t *testing.T) {
	// 截断量化的误差就是被丢弃的低 d 位：e = x mod Δ
	for _, b := range []int{2, 3, 4} {
		_, delta, _ := dropBits(b)
		pix := []uint8{0, 1, 5, 17, 200, 255}
		e, err := Error(pix, b)
		if err != nil {
			t.Fatal(err)
		}
		for i, x := range pix {
			if e[i] != int(x)%delta {
				t.Fatalf("b=%d x=%d 误差 %d，期望 %d", b, x, e[i], int(x)%delta)
			}
		}
	}
}

func TestErrorRangeAndClamp(t *testing.T) {
	// 全幅 0..255，Δ=8，误差必须落在 {0,…,7}
	pix := make([]uint8, 256)
	for i := range pix {
		pix[i] = uint8(i)
	}
	e, err := Error(pix, 4) // d=3, Δ=8
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, v := range e {
		if v < 0 || v > 7 {
			t.Fatalf("误差 %d 超出区间 [0,7]", v)
		}
		seen[v] = true
	}
	if len(seen) != 8 {
		t.Fatalf("误差符号数 = %d，期望 8", len(seen))
	}

	// 边界裁剪：255 量化重构后仍应落在 [0,255]
	edge, err := Reduce([]uint8{255, 0}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if edge[0] > 255 || edge[1] != 0 {
		t.Fatalf("边界裁剪失败: %v", edge)
	}
}

func TestUniformEntropyEqualsTheory(t *testing.T) {
	// 0..255 全幅在 Δ 整除 256 时，每个误差值恰好出现 256/Δ 次，严格均匀
	for _, b := range []int{2, 3, 4, 5} {
		pix := make([]uint8, 256)
		for i := range pix {
			pix[i] = uint8(i)
		}
		e, err := Error(pix, b)
		if err != nil {
			t.Fatal(err)
		}
		got := Entropy(e)
		th, _ := TheoreticalEntropy(b)
		if math.Abs(got-th) > 1e-12 {
			t.Fatalf("b=%d 实测熵 %.6f 与理论 %.1f 不符", b, got, th)
		}
		if !IsUniform(e, 1e-9) {
			t.Fatalf("b=%d 误差应严格均匀", b)
		}
	}
}

func TestEntropySlopeOneOnUniform(t *testing.T) {
	// 三档 b=2,3,4：相邻档熵差应恰为 1
	pix := make([]uint8, 256*4)
	for i := range pix {
		pix[i] = uint8(i % 256)
	}
	var prev float64
	for k, b := range []int{2, 3, 4} {
		e, err := Error(pix, b)
		if err != nil {
			t.Fatal(err)
		}
		h := Entropy(e)
		if k > 0 && math.Abs(h-prev-1) > 1e-12 {
			t.Fatalf("b=%d 与上一档熵差 = %.6f，期望 1", b, h-prev)
		}
		prev = h
	}
}
