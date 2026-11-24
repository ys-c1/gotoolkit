package api

import (
	"fmt"

	"gotoolkit/internal/quantfb"
)

// FeedbackDesign 是「量化器 × 二阶反馈」联合不动点的公开参数。
type FeedbackDesign struct {
	Step        int     // 整数量化步长 q*=10
	C1, C2      float64 // 两个二阶反馈系数
	Bound       float64 // 理论输出误差界 (1+|c1|+|c2|)q/2
	BudgetD0    float64 // A 组最大量化误差预算 18.5
	DeltaCont   float64 // 连续不动点步长 ≈10.1499642020
	TSecondZero float64 // 第二零点 t*≈0.8226665269
}

// FixedPointFeedback 返回交替迭代得到的联合不动点参数：
// q*=10，c1*≈−1.8226665269，c2*≈+0.8226665269。
func FixedPointFeedback() FeedbackDesign {
	c1, c2 := quantfb.JointCoeffs()
	delta, t, _, _, _, _ := quantfb.ContinuousFixedPoint()
	q := quantfb.StepForCoeffs(c1, c2)
	return FeedbackDesign{
		Step: q, C1: c1, C2: c2,
		Bound:       quantfb.BoundAt(float64(q), c1, c2),
		BudgetD0:    quantfb.BudgetD,
		DeltaCont:   delta,
		TSecondZero: t,
	}
}

// FeedbackOutput 是二阶反馈量化的结果与核对指标。
type FeedbackOutput struct {
	Image      *Image  // 量化后的 8-bit 灰度图
	Step       int     // 实际使用的步长
	C1, C2     float64 // 实际系数
	OutMaxAbs  float64 // 实测最大输出误差 |Out−x|
	Bound      float64 // 理论误差界
	BoundHolds bool    // 全图像素是否满足 OutMaxAbs ≤ Bound
	LFShare    float64 // ω≤π/4 行 DFT 低频能量占比
	QuantRMSE  float64 // 回路局部量化误差 RMSE
}

// QuantizeFeedback 用给定步长与两个反馈系数跑逐行二阶误差反馈量化。
// step 必须为正；系数阶数固定为二。联合不动点参数见 FixedPointFeedback。
func QuantizeFeedback(img *Image, step int, c1, c2 float64) (*FeedbackOutput, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if step <= 0 || step > 255 {
		return nil, fmt.Errorf("%w: 反馈量化步长 step=%d 越界 [1,255]", ErrBadOption, step)
	}
	g := &quantfb.Gray{W: img.W, H: img.H, Pix: img.Pix}
	res := quantfb.FeedbackQuantize(g, float64(step), c1, c2, true)
	bound := quantfb.BoundAt(float64(step), c1, c2)
	st := quantfb.Analyze(res, bound)
	return &FeedbackOutput{
		Image:      &Image{W: img.W, H: img.H, Pix: res.Out},
		Step:       step,
		C1:         c1,
		C2:         c2,
		OutMaxAbs:  st.OutMax,
		Bound:      bound,
		BoundHolds: st.BoundHolds,
		LFShare:    st.LFSharePi4,
		QuantRMSE:  st.QuantRMSE,
	}, nil
}

// QuantizeJoint 直接用联合不动点 (q*=10,c1*,c2*) 量化，是最常用入口。
func QuantizeJoint(img *Image) (*FeedbackOutput, error) {
	d := FixedPointFeedback()
	return QuantizeFeedback(img, d.Step, d.C1, d.C2)
}
