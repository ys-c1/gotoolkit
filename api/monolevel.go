// 本文件把 internal/monolevel 的单调重建能力接到唯一对外入口 api 包。
// 外部调用方只允许使用这里的类型与函数；internal 包不可被模块外引用。
package api

import (
	"fmt"

	"gotoolkit/internal/monolevel"
)

// QuantBoundaries 是两步构造的第一步：由步长序列铺出 L+1 个区间边界
// t_0=0，t_k=Σ_{i≤k}Δ_i。要求步长为正且总和为 256（8 位灰度域）。
func QuantBoundaries(steps []int) ([]int, error) {
	b, err := monolevel.Partition(steps)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	return b, nil
}

// MonotoneReconstruct 是两步构造的第二步（加权保序回归 PAVA）：
// 对任给的名义电平 a（不需要单调）与正权重 w，求
//
//	min Σ_k w_k(r_k-a_k)²  s.t. r_0≤r_1≤…≤r_{L-1}
//
// 的唯一实值最优解。对任意 (a,w) 输出都非降。
func MonotoneReconstruct(a, w []float64) ([]float64, error) {
	r, _, err := monolevel.PAVA(a, w)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	return r, nil
}

// RoundQuantLevels 把实值电平四舍五入并夹到 [0,255]；取整与夹紧都非降，
// 故输入非降时输出也非降。
func RoundQuantLevels(r []float64) []int { return monolevel.RoundLevels(r) }

// MidpointQuantLevels 返回步长序列对应的几何中点重建表（对照路径）。
func MidpointQuantLevels(steps []int) ([]int, error) {
	m, err := monolevel.MidpointLevels(steps)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	return m, nil
}

// CountLevelInversions 对电平表做全量逐对比较：比较全部 n(n-1)/2 个 i<j，
// 返回比较对数与逆序对（levels[i]>levels[j]）数；不抽样。
func CountLevelInversions(levels []int) (compared, inversions int) {
	n, bad := monolevel.AllPairs(levels)
	return n, len(bad)
}

// CountRampInversions 把电平表按步长展开成 256 点重建映射 Q(0..255)，
// 再对全部 256·255/2=32640 个对子做逐对比较。
func CountRampInversions(steps, levels []int) (compared, inversions int, err error) {
	n, bad, err := monolevel.RampAllPairs(steps, levels)
	if err != nil {
		return 0, 0, fmt.Errorf("api: %w", err)
	}
	return n, len(bad), nil
}

// QuantAnalysis 是一张量化表从构造、最优性到误差的完整核验结果。
type QuantAnalysis struct {
	Steps    []int
	Bounds   []int
	Nominal  []int // 由直方图条件均值加对比度扰动凑出的（通常非单调）名义表
	Float    []float64
	Levels   []int // 两步构造 + 取整后的单调整数电平
	Midpoint []int // 中点参照

	LevelPairs       int // 电平表逐对比较总数 L(L-1)/2
	LevelInversions  int // 修复后电平逆序对（必须为 0）
	RampPairs        int // 256 点映射逐对比较总数 32640
	RampInversions   int // 修复后映射逆序对（必须为 0）
	NominalLevelBad  int // 名义表电平逆序对
	NominalRampBad   int // 名义表映射逆序对
	MidpointLevelBad int
	MidpointRampBad  int

	JOptimal  float64 // Σw(r_int-a)²
	JMidpoint float64 // Σw(mid-a)²

	MSEOptimal           float64 // 与输入直方图同一份数据上的像素 MSE
	MSEMidpoint          float64
	MSENominal           float64
	PredictedMSEOptimal  float64 // Σw[σ²+(r-μ)²]，与同一份直方图上的 MSE 相等
	PredictedMSEMidpoint float64

	PrimalViolation float64 // max_k max(0,r_k-r_{k+1})
	Stationarity    float64
	Complementarity float64
	MinLambda       float64
	Active          int // 起作用约束 r_k=r_{k+1}
	Inactive        int // 非起作用约束 r_k<r_{k+1}
}

// AnalyzeQuantTable 对给定步长序列与 256 桶直方图执行完整构造与核验：
// 用区间条件均值按 contrast·Δ_k·(−1)^k 生成“凑出来的”名义表，再做两步构造，
// 并与中点重建对照。contrast 取 0 时名义表就是条件均值。
func AnalyzeQuantTable(steps []int, hist []uint64, contrast float64) (*QuantAnalysis, error) {
	bounds, err := monolevel.Partition(steps)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	cells, err := monolevel.Stats(steps, hist)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	nominal := monolevel.NominalLevels(cells, contrast)
	a := make([]float64, len(nominal))
	for i, v := range nominal {
		a[i] = float64(v)
	}
	rf, _, err := monolevel.PAVA(a, cells.Weight)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	levels := monolevel.RoundLevels(rf)
	mid, err := monolevel.MidpointLevels(steps)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}

	out := &QuantAnalysis{
		Steps: steps, Bounds: bounds, Nominal: nominal, Float: rf,
		Levels: levels, Midpoint: mid,
	}

	out.LevelPairs, out.NominalLevelBad = count(monolevel.AllPairs(nominal))
	_, badLevels := monolevel.AllPairs(levels)
	out.LevelInversions = len(badLevels)
	_, badMidLevels := monolevel.AllPairs(mid)
	out.MidpointLevelBad = len(badMidLevels)

	out.RampPairs, _, err = monolevel.RampAllPairs(steps, nominal)
	if err != nil {
		return nil, err
	}
	_, badNomRamp, err := monolevel.RampAllPairs(steps, nominal)
	if err != nil {
		return nil, err
	}
	out.NominalRampBad = len(badNomRamp)
	_, badRamp, err := monolevel.RampAllPairs(steps, levels)
	if err != nil {
		return nil, err
	}
	out.RampInversions = len(badRamp)
	_, badMidRamp, err := monolevel.RampAllPairs(steps, mid)
	if err != nil {
		return nil, err
	}
	out.MidpointRampBad = len(badMidRamp)

	if out.JOptimal, err = monolevel.LSError(a, cells.Weight, floatSlice(levels)); err != nil {
		return nil, err
	}
	if out.JMidpoint, err = monolevel.LSError(a, cells.Weight, floatSlice(mid)); err != nil {
		return nil, err
	}
	if out.MSENominal, err = monolevel.PixelMSE(hist, bounds, nominal); err != nil {
		return nil, err
	}
	if out.MSEOptimal, err = monolevel.PixelMSE(hist, bounds, levels); err != nil {
		return nil, err
	}
	if out.MSEMidpoint, err = monolevel.PixelMSE(hist, bounds, mid); err != nil {
		return nil, err
	}
	if out.PredictedMSEOptimal, err = monolevel.PredictMSE(cells, levels); err != nil {
		return nil, err
	}
	if out.PredictedMSEMidpoint, err = monolevel.PredictMSE(cells, mid); err != nil {
		return nil, err
	}
	kkt := monolevel.KKT(a, cells.Weight, rf)
	out.PrimalViolation = kkt.PrimalViolation
	out.Stationarity = kkt.Stationarity
	out.Complementarity = kkt.Complementarity
	out.MinLambda = kkt.MinLambda
	out.Active, out.Inactive = kkt.Active, kkt.Inactive
	return out, nil
}

// DemoQuantTiers 返回内置三档步长的名字与序列（等宽 8 / 几何 16 / 感知 32）。
func DemoQuantTiers() []QuantTier {
	tiers := make([]QuantTier, len(monolevel.Tiers))
	for i, t := range monolevel.Tiers {
		tiers[i] = QuantTier{Name: t.Name, Steps: append([]int(nil), t.Steps...)}
	}
	return tiers
}

// QuantTier 是一档演示步长。
type QuantTier struct {
	Name  string
	Steps []int
}

// DemoTierHistogram 返回第 i 档内置的确定性 256 桶数据集（总和 262144）。
func DemoTierHistogram(i int) ([]uint64, error) {
	h, err := monolevel.TierHistogram(i)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	return h, nil
}

func count(n int, bad []monolevel.Pair) (int, int) { return n, len(bad) }

func floatSlice(v []int) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x)
	}
	return out
}
