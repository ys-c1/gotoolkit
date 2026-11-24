package api

import (
	"fmt"
	"math"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/target"
)

// 分块尺寸的公共默认/上限，具体语义见 internal/codec/blk.go。
const (
	DefaultBlockSize = codec.DefaultBlock
	MaxBlockSize     = codec.MaxBlock
)

// SolveStep 是二分求解中的一次试探：窗口 [Lo,Hi] 内取 Mid，
// Depth 是 Mid 对应的位深，Bytes 是真实编码实测体积。
type SolveStep struct {
	N     int `json:"n"`
	Lo    int `json:"lo"`
	Mid   int `json:"mid"`
	Hi    int `json:"hi"`
	Depth int `json:"depth"`
	Bytes int `json:"bytes"`
}

// QualityReport 是“按目标体积反解质量参数”的结果与对账记录。
//
// 最终采用参数 Quality 与实测体积 Bytes 始终成对出现；InRange 表示实测值
// 是否落在 [MinBytes,MaxBytes]。区间不可达时 InRange=false，Closest* 给出
// 全质量域上的最接近参数/体积，GapBytes/GapPercent 说明差多少。
type QualityReport struct {
	Tier   string `json:"tier,omitempty"`
	Format string `json:"format"`
	W, H   int    `json:"-"`

	BlockSize int `json:"block_size"`
	Pixels    int `json:"pixels"`

	MinBytes int `json:"min_bytes"`
	MaxBytes int `json:"max_bytes"`

	// BinaryQuality/BinaryDepth 是二分 lower_bound 求出的参数（可能 -1）。
	BinaryQuality int `json:"binary_quality"`
	BinaryDepth   int `json:"binary_depth"`
	BinaryBytes   int `json:"binary_bytes"`

	// Quality/Depth/Bytes 是最终采用参数及其实测体积（同一行对账的三个量）。
	Quality int `json:"quality"`
	Depth   int `json:"depth"`
	Bytes   int `json:"bytes"`

	InRange bool `json:"in_range"`

	Steps []SolveStep `json:"steps"`

	// 区间外兜底：最接近区间的参数、实测体积与差距。
	ClosestQuality int     `json:"closest_quality"`
	ClosestDepth   int     `json:"closest_depth"`
	ClosestBytes   int     `json:"closest_bytes"`
	GapBytes       int     `json:"gap_bytes"`
	GapPercent     float64 `json:"gap_percent"`
	Side           string  `json:"side"` // inside | below_min | above_max

	PSNR float64 `json:"psnr_db"`

	Body []byte `json:"-"` // 采用参数下真实编码出的 BLK1 字节
}

// targetInterval 从 Options 解析目标闭区间。
func targetInterval(opt Options) (int, int, error) {
	lo, hi := opt.TargetMinBytes, opt.TargetMaxBytes
	if lo == 0 && hi == 0 && opt.TargetBytes > 0 {
		lo, hi = opt.TargetBytes, opt.TargetBytes // 点目标 => 退化闭区间
	}
	if lo <= 0 || hi < lo {
		return 0, 0, fmt.Errorf("%w: TargetMinBytes=%d TargetMaxBytes=%d TargetBytes=%d，需要正的闭区间",
			ErrBadTarget, opt.TargetMinBytes, opt.TargetMaxBytes, opt.TargetBytes)
	}
	return lo, hi, nil
}

// SolveQuality 按 opt 中的目标体积区间反解 BLK 质量参数。
//
// 流程：校验 → 用真实 BLK 编码作为 measure 二分 lower_bound → 回代实测 →
// 不命中则全质量域找最接近值与差距 → 再编码/解码一次得到 Body 与 PSNR。
// 区间不可达不视为错误（报告 InRange=false + Closest* + Gap*）；
// 只有图像/参数/区间非法或编码失败才返回 error。
func SolveQuality(img *Image, opt Options) (*QualityReport, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	opt.Format = "blk" // 体积反解只挂在单调的 BLK 分块格式上
	if err := opt.validate(); err != nil {
		return nil, err
	}
	lo, hi, err := targetInterval(opt)
	if err != nil {
		return nil, err
	}

	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	bs := opt.effectiveBlockSize()
	w, h := src.W, src.H

	measure := func(q int) (int, error) {
		d := target.DepthForQuality(q)
		body, err := codec.EncodeBLK(w, h, src.Pix, bs, d)
		if err != nil {
			return 0, err
		}
		return len(body), nil
	}

	sol, err := target.Solve(lo, hi, measure)
	if err != nil {
		return nil, err
	}

	depth := target.DepthForQuality(sol.Quality)
	body, err := codec.EncodeBLK(w, h, src.Pix, bs, depth)
	if err != nil {
		return nil, err
	}

	rep := &QualityReport{
		Format:         "blk",
		W:              w,
		H:              h,
		BlockSize:      bs,
		Pixels:         w * h,
		MinBytes:       lo,
		MaxBytes:       hi,
		BinaryQuality:  sol.BinaryQuality,
		BinaryBytes:    sol.BinaryBytes,
		Quality:        sol.Quality,
		Depth:          depth,
		Bytes:          len(body), // 最终以落地字节为准
		InRange:        sol.InRange,
		ClosestQuality: sol.ClosestQuality,
		ClosestDepth:   target.DepthForQuality(sol.ClosestQuality),
		ClosestBytes:   sol.ClosestBytes,
		GapBytes:       sol.GapBytes,
		Side:           sol.Side,
		Body:           body,
	}
	if sol.BinaryQuality >= 0 {
		rep.BinaryDepth = target.DepthForQuality(sol.BinaryQuality)
	}
	for _, st := range sol.Iterations {
		rep.Steps = append(rep.Steps, SolveStep{
			N: st.N, Lo: st.Lo, Mid: st.Mid, Hi: st.Hi,
			Depth: target.DepthForQuality(st.Mid), Bytes: st.Bytes,
		})
	}

	// 差距百分比相对被突破的那一侧边界。
	switch rep.Side {
	case "below_min":
		rep.GapPercent = 100 * float64(rep.GapBytes) / float64(lo)
	case "above_max":
		rep.GapPercent = 100 * float64(rep.GapBytes) / float64(hi)
	}

	// 解码一次算 PSNR，直观交代“体积 vs 糊不糊”。
	if _, _, dec, derr := codec.DecodeBLK(body); derr == nil {
		rep.PSNR = psnr(src.Pix, dec)
	}
	return rep, nil
}

// SolveInterval 是显式给出区间与分块边长的便捷入口，其余参数用默认 Options。
func SolveInterval(img *Image, minBytes, maxBytes, blockSize int) (*QualityReport, error) {
	return SolveQuality(img, Options{
		Format:         "blk",
		BlockSize:      blockSize,
		TargetMinBytes: minBytes,
		TargetMaxBytes: maxBytes,
	})
}

// BLKPredictedSize 是体积单调关系式 S(Q;B) 的公共入口：
// 返回 W×H 图在分块 B、质量 Q（0=100）下的精确预测字节数，
// 且恒等于实际 Encode(...Format=blk...) 的 len。
func BLKPredictedSize(w, h, blockSize, quality int) int {
	q := quality
	if q <= 0 {
		q = target.MaxQuality
	}
	return codec.BLKSize(w, h, codec.NormalizeBlockSize(blockSize), target.DepthForQuality(q))
}

// BLKBudgetDepth 是“分块尺寸 ↔ 预算”反解式的公共入口（见 codec.BudgetDepth）。
func BLKBudgetDepth(w, h, blockSize, budgetBytes int) int {
	return codec.BudgetDepth(w, h, blockSize, budgetBytes)
}

// BLKDepthSize 按位深（1..8）直接返回 BLK1 精确字节数。
func BLKDepthSize(w, h, blockSize, depth int) int {
	return codec.BLKSize(w, h, codec.NormalizeBlockSize(blockSize), depth)
}

// BLKBlockSizeForBudget 是给定每块预算与目标位深反推最大分块边长的公共入口。
func BLKBlockSizeForBudget(blockBytes, depth int) int {
	return codec.BlockSizeForBudget(blockBytes, depth)
}

// DepthForQuality 暴露质量→位深的单调映射 d(Q)=ceil(8Q/100)。
func DepthForQuality(quality int) int { return target.DepthForQuality(quality) }

// psnr 计算 8 位灰度峰值信噪比；完全相同返回 +Inf。
func psnr(a, b []byte) float64 {
	n := len(a)
	if n == 0 || len(b) != n {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		d := float64(int(a[i]) - int(b[i]))
		sum += d * d
	}
	mse := sum / float64(n)
	if mse == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/mse)
}
