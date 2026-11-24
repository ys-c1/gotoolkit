package rd

import "testing"

func pix12() []uint8 {
	p := make([]uint8, 12)
	for i := range p {
		p[i] = uint8(i * 7)
	}
	return p
}

func TestResidualsArePredictive(t *testing.T) {
	// 4x3，行首残差应等于像素本身，其余等于与左邻之差。
	pix := pix12()
	res := Residuals(4, 3, pix)
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			idx := y*4 + x
			var want int
			if x == 0 {
				want = int(pix[idx])
			} else {
				want = int(pix[idx]) - int(pix[idx-1])
			}
			if res[idx] != want {
				t.Fatalf("(%d,%d) 残差=%d 期望 %d", x, y, res[idx], want)
			}
		}
	}
}

func TestQuantizeRoundTrip(t *testing.T) {
	for _, step := range []int{1, 2, 7, 64, 255} {
		for r := -300; r <= 300; r++ {
			q := quantize(r, step)
			got := q * step
			// 重建误差不超过半步（裁剪在 Reconstruct 里处理，这里只看量化）。
			d := got - r
			if d < 0 {
				d = -d
			}
			if d > step/2+1 {
				t.Fatalf("step=%d r=%d 量化误差 %d 超半步", step, r, d)
			}
		}
	}
}

func TestReconstructBounded(t *testing.T) {
	pix := []uint8{255, 0, 200, 10, 90, 128}
	recon, levels := Reconstruct(3, 2, pix, 5)
	if len(recon) != len(pix) || len(levels) != len(pix) {
		t.Fatal("重建长度不匹配")
	}
	for _, v := range recon {
		if v > 255 {
			t.Fatal("重建值越界")
		}
	}
	// 步长 1 时应近似无损（DPCM 闭环 + 裁剪）。
	recon1, _ := Reconstruct(3, 2, pix, 1)
	for i, v := range pix {
		if recon1[i] != v {
			t.Fatalf("步长1 应无损: %d != %d", recon1[i], v)
		}
	}
}

func TestInverseStepMonotonic(t *testing.T) {
	pix := pix12()
	prev := InverseStep(4, 3, pix, 0.1)
	for _, bpp := range []float64{0.5, 1.0, 2.0, 4.0, 8.0, 16.0, 32.0} {
		got := InverseStep(4, 3, pix, bpp)
		if got > prev {
			t.Fatalf("bpp=%.1f 反解步长 %d 应不增（前值 %d）", bpp, got, prev)
		}
		prev = got
	}
	if InverseStep(4, 3, pix, 32.0) != 0 {
		t.Fatal("极端高目标应反解为 0 步长")
	}
}

func TestSearchAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		enc, ok := Encoders(f)
		if !ok {
			t.Fatalf("缺编码器 %s", f)
		}
		for _, bpp := range []float64{0.5, 1.0, 2.0, 4.0} {
			res, err := Search(4, 3, pix12(), bpp, enc)
			if err != nil {
				t.Fatalf("%s bpp=%.1f 搜索失败: %v", f, bpp, err)
			}
			if res.Best.Step < MinStep || res.Best.Step > MaxStep {
				t.Fatalf("%s bpp=%.1f 步长越界: %d", f, bpp, res.Best.Step)
			}
			if res.Best.Bits != len(res.Body)*8 {
				t.Fatalf("%s bpp=%.1f 比特与字节对不上", f, bpp)
			}
			if res.GapBits < 0 {
				t.Fatal("差距不能为负")
			}
			if res.Reachable && res.GapBits != 0 {
				t.Fatal("标记可达但差距非零")
			}
			// 最接近结果必须是全局最优：用独立全量扫描复核。
			minGap := -1
			for step := MinStep; step <= MaxStep; step++ {
				c, err := measure(4, 3, pix12(), step, enc)
				if err != nil {
					t.Fatal(err)
				}
				gap := c.Bits - res.TargetBits
				if gap < 0 {
					gap = -gap
				}
				if minGap < 0 || gap < minGap {
					minGap = gap
				}
			}
			if res.GapBits != minGap {
				t.Fatalf("%s bpp=%.1f 报告差距 %d 不是最优 %d", f, bpp, res.GapBits, minGap)
			}
		}
	}
}

func TestSearchFlatImage(t *testing.T) {
	flat := make([]uint8, 16) // 全 0，残差全 0
	enc, _ := Encoders("rle")
	res, err := Search(4, 4, flat, 4.0, enc)
	if err != nil {
		t.Fatal(err)
	}
	if res.InitialStep != MinStep {
		t.Fatalf("平坦图初始步长应为 %d，实际 %d", MinStep, res.InitialStep)
	}
}
