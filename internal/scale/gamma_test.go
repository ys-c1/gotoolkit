package scale

import (
	"math"
	"testing"
)

func TestAnalyticBiasAndInverse(t *testing.T) {
	const gamma = 2.2
	for _, rho := range []float64{0.5, 0.25, 0.125} {
		bias := EdgeBias(32, rho, gamma)
		if bias <= 0 {
			t.Fatalf("rho=%v 应得到正的偏亮偏差，实际 %v", rho, bias)
		}

		k := EdgeCompensation(rho, gamma)
		got := k * rho * 32
		want := math.Pow(rho, gamma) * 32
		if math.Abs(got-want) > 1e-12 {
			t.Fatalf("rho=%v 反解不一致: %v != %v", rho, got, want)
		}
	}
}

func TestEncodedPathRemainsSelectable(t *testing.T) {
	// 2x2 盒中第一列是暗电平 32，另一列为 0；覆盖率 theta=1/2。
	src := Image{W: 2, H: 2, Pix: []byte{32, 0, 32, 0}}

	enc, err := Box(src, 2, 2.2, ModeEncoded)
	if err != nil {
		t.Fatal(err)
	}
	if enc.W != 1 || enc.H != 1 || enc.Pix[0] != 16 {
		t.Fatalf("原编码值路径应为算术平均 16，得到 %#v", enc)
	}

	dual, err := Box(src, 2, 2.2, ModeGammaDual)
	if err != nil {
		t.Fatal(err)
	}
	if dual.Pix[0] != 7 {
		t.Fatalf("gamma-dual 参照应为 7，得到 %d", dual.Pix[0])
	}

	comp, err := Box(src, 2, 2.2, ModeCompensated)
	if err != nil {
		t.Fatal(err)
	}
	if comp.Pix[0] != dual.Pix[0] {
		t.Fatalf("补偿码值 %d 未对齐参照 %d", comp.Pix[0], dual.Pix[0])
	}
}

func TestUniformRegionIsUnchangedByCompensation(t *testing.T) {
	src := Image{W: 4, H: 4, Pix: make([]byte, 16)}
	for i := range src.Pix {
		src.Pix[i] = 40
	}
	out, err := Box(src, 2, 2.2, ModeCompensated)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range out.Pix {
		if v != 40 {
			t.Fatalf("均匀区域不应被补偿改变，得到 %d", v)
		}
	}
}

func TestTwoFrameSizesRecomputeWithinOneCode(t *testing.T) {
	rows, err := RunLab(2.2, 1, DefaultFrames())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("应有三档缩放比，得到 %d", len(rows))
	}
	for _, row := range rows {
		if len(row.Frames) != 2 {
			t.Fatalf("rho=1/%d 应有两档画幅", row.Denom)
		}
		for _, f := range row.Frames {
			t.Logf("rho=1/%d frame=%s output=%dx%d K=%.6f bias32=%+.3f residual32=%+d maxResidual=%d",
				row.Denom, f.Name, f.OutW, f.OutH, row.K, row.BiasAt32, row.Residual32, f.MaxAbsResidual)
			if f.MaxAbsResidual > 1 {
				t.Fatalf("rho=1/%d frame=%s residual=%d 超过 1 code",
					row.Denom, f.Name, f.MaxAbsResidual)
			}
		}
		if !row.Pass {
			t.Fatalf("rho=1/%d 未通过容差", row.Denom)
		}
	}
}

func TestCompensationMonotonicInGamma(t *testing.T) {
	gammas := []float64{1.4, 1.8, 2.2, 2.6, 3.0}
	for _, rho := range []float64{0.5, 0.25, 0.125} {
		if !MonotonicCompensation(rho, gammas) {
			t.Fatalf("rho=%v 下 K 应随 gamma 单调递减", rho)
		}

		// 导数 dK/dgamma = rho^(gamma-1) ln rho；rho<1 时严格为负。
		k := EdgeCompensation(rho, 2.2)
		derivative := k * math.Log(rho)
		if derivative >= 0 {
			t.Fatalf("rho=%v 导数应为负，得到 %v", rho, derivative)
		}
	}
}
