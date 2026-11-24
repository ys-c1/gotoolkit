package api

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func sample() *Image {
	img := &Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	return img
}

// ramp 是 256 级灰度斜坡，均值恰好 127.5，作为伽马反解的演示样本。
func ramp() *Image {
	img := &Image{W: 256, H: 1, Pix: make([]uint8, 256)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	return img
}

func meanOf(pix []uint8) float64 {
	s := 0
	for _, v := range pix {
		s += int(v)
	}
	return float64(s) / float64(len(pix))
}

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, Options{Format: f, Gamma: 1})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f, Gamma: 1})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if got.W != img.W || got.H != img.H {
			t.Fatalf("%s 尺寸不一致", f)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("%s 像素不一致", f)
		}
	}
}

func TestGoldenBytes(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		// 缺省 Options 与显式 Gamma=1 都必须与基准逐字节一致。
		for _, opt := range []Options{
			{Format: f, Gamma: 1},
			DefaultOptions(), /* Format 覆盖为 f，Gamma 仍为缺省 1 */
		} {
			opt.Format = f
			got, err := Encode(img, opt)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s 与原编码不逐字节一致 (opt=%+v)", f, opt)
			}
		}
	}
}

func TestRejects(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "tga", Gamma: 1}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500, Gamma: 1}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm", Gamma: 1}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm", Gamma: 1}); err == nil {
		t.Fatal("空数据应报错")
	}
}

func TestGammaRejects(t *testing.T) {
	img := sample()
	bad := []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, g := range bad {
		if _, err := Encode(img, Options{Format: "pgm", Gamma: g}); err == nil {
			t.Fatalf("Gamma=%v 应被拒绝", g)
		}
		if _, err := Decode([]byte("P5\n1 1\n255\n\x00"), Options{Format: "pgm", Gamma: g}); err == nil {
			t.Fatalf("解码 Gamma=%v 应被拒绝", g)
		}
	}
}

func TestGammaDefaultIdentity(t *testing.T) {
	img := ramp()
	// 零值结构体以外的所有缺省路径：DefaultOptions 与显式 1 输出完全相同。
	a, err := Encode(img, Options{Format: "pgm", Gamma: 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(img, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("Gamma=1 与缺省输出不一致")
	}
	out := applyGamma(img, 1)
	if !bytes.Equal(out.Pix, img.Pix) {
		t.Fatal("Gamma=1 不是恒等变换")
	}
}

func TestSolveGammaTwoTargets(t *testing.T) {
	img := ramp()
	for _, target := range []float64{100, 180} {
		g, bound, err := SolveGamma(img.Pix, target)
		if err != nil {
			t.Fatalf("target=%v 反解失败: %v", target, err)
		}
		if g <= 0 || math.IsNaN(g) || math.IsInf(g, 0) {
			t.Fatalf("反解 gamma 非法: %v", g)
		}
		got := applyGamma(img, g)
		gotMean := meanOf(got.Pix)
		errAbs := math.Abs(gotMean - target)
		t.Logf("target=%.1f gamma=%.10f 回代均值=%.6f 误差=%.6f 上界=%.6f",
			target, g, gotMean, errAbs, bound)
		if errAbs > bound {
			t.Fatalf("target=%v 误差 %.6f 超过上界 %.6f", target, errAbs, bound)
		}
		if bound > 0.5+1e-9 {
			t.Fatalf("上界 %.6f 超过理论值 0.5+1e-9", bound)
		}
	}
}

func TestSolveGammaRejects(t *testing.T) {
	img := ramp()
	// 斜坡可达区间为 (0, 255)，端点与区间外均不可达。
	for _, target := range []float64{0, 255, 300, -5, math.NaN()} {
		if _, _, err := SolveGamma(img.Pix, target); err == nil {
			t.Fatalf("target=%v 应报错", target)
		}
	}
	// 常量图：全 0 与全 255 在任何伽马下都不动，没有可调空间；
	// 中间值常量（如全 100）则可达 (0,255) 内任意目标。
	for _, pix := range []uint8{0, 255} {
		if _, _, err := SolveGamma([]uint8{pix, pix, pix}, 100); err == nil {
			t.Fatalf("常量 %v 图应报不可达", pix)
		}
	}
	if _, _, err := SolveGamma(nil, 100); err == nil {
		t.Fatal("空像素应报错")
	}
}

func TestGammaRoundTrip(t *testing.T) {
	img := ramp()
	for _, g := range []float64{0.5, 1.8} {
		body, err := Encode(img, Options{Format: "pgm", Gamma: g})
		if err != nil {
			t.Fatal(err)
		}
		back, err := Decode(body, Options{Format: "pgm", Gamma: g})
		if err != nil {
			t.Fatal(err)
		}
		// 量化往返误差：每级舍入 ≤0.5，但解码端逆变换在暗部梯度大，
		// 单级舍入经梯度放大后 gamma=0.5 最坏约 11 级（双重量化的固有上界）；
		// 均值层面误差仍应 ≤1 级。
		maxDiff, sumDiff := 0, 0.0
		for i, v := range img.Pix {
			d := int(back.Pix[i]) - int(v)
			if d < 0 {
				d = -d
			}
			if d > maxDiff {
				maxDiff = d
			}
			sumDiff += float64(d)
		}
		meanDiff := sumDiff / float64(len(img.Pix))
		t.Logf("gamma=%v 往返最大误差=%d 级, 均值误差=%.4f 级", g, maxDiff, meanDiff)
		if g > 1 && maxDiff > 1 {
			t.Fatalf("gamma=%v 提亮方向往返最大误差 %d 级", g, maxDiff)
		}
		if maxDiff > 12 || meanDiff > 1 {
			t.Fatalf("gamma=%v 往返误差超界: max=%d mean=%.4f", g, maxDiff, meanDiff)
		}
	}
}
