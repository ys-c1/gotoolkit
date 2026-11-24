package api

import (
	"math"
	"testing"
)

func TestFixedPointFeedbackValues(t *testing.T) {
	d := FixedPointFeedback()
	if d.Step != 10 {
		t.Fatalf("q*=%d, want 10", d.Step)
	}
	if math.Abs(d.C1+1.8226665269) > 1e-9 {
		t.Fatalf("c1=%.10f", d.C1)
	}
	if math.Abs(d.C2-0.8226665269) > 1e-9 {
		t.Fatalf("c2=%.10f", d.C2)
	}
	if math.Abs(d.Bound-18.2266652688) > 1e-6 {
		t.Fatalf("bound=%.6f", d.Bound)
	}
	if d.Bound > d.BudgetD0 {
		t.Fatalf("不动点界 %.4f 超过预算 %.1f", d.Bound, d.BudgetD0)
	}
}

func TestQuantizeJointAPIRoundTrip(t *testing.T) {
	// 256×64 gamma 渐变，走唯一对外入口。
	const W, H = 256, 64
	img := &Image{W: W, H: H, Pix: make([]uint8, W*H)}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			u := float64(x) / (W - 1)
			v := 255 * math.Pow(u, 2.2)
			img.Pix[y*W+x] = uint8(math.Round(v))
		}
	}
	out, err := QuantizeJoint(img)
	if err != nil {
		t.Fatal(err)
	}
	if out.Image.W != W || out.Image.H != H || len(out.Image.Pix) != W*H {
		t.Fatal("输出尺寸/长度不一致")
	}
	if !out.BoundHolds || out.OutMaxAbs > out.Bound+1e-9 {
		t.Fatalf("输出误差越界: max=%.3f B=%.3f holds=%v", out.OutMaxAbs, out.Bound, out.BoundHolds)
	}
	// 低频占比应当处于噪声整形后的低水平（gamma 上实测约 0.0145）。
	if out.LFShare > 0.05 {
		t.Fatalf("低频占比偏高: %.4f", out.LFShare)
	}
	// 参数校验。
	if _, err := QuantizeFeedback(img, 0, 0, 0); err == nil {
		t.Fatal("step=0 必须报 ErrBadOption")
	}
	if _, err := QuantizeJoint(nil); err == nil {
		t.Fatal("nil 图像必须报错")
	}
}
