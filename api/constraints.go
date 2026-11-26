package api

import "gotoolkit/internal/codec"

// 本文件把“块边界连续性 vs 块直流保真”的约束系统作为公共 API 暴露。
//
// 两个约束（详细推导见 docs/block-constraints.md）：
//   (DC)   直流保真：每像素 |重建 − 块直流| ≤ ρ
//   (SEAM) 边界连续：跨块边界相邻像素差 ≤ τ
//
// 二者有解的充要条件是两条不等式：
//   ① |μ_b − μ_b'| ≤ 2ρ + τ     对所有边相邻块
//   ② |μ_b − μ_b''| ≤ 2ρ + 2τ   对所有角对角块
// 冲突块的判定用块间边缘强度 |μ_b − μ_b'| 与阈值 θ = 2ρ + τ。

// ConstraintParams 是两条约束的预算。
type ConstraintParams struct {
	Rho int // (DC) 每像素允许偏离块直流的灰度级，≥0
	Tau int // (SEAM) 跨块边界最大灰度差，≥0
}

// BlockEdge 是一条块间边及其边缘强度。
type BlockEdge struct {
	BX0, BY0, BX1, BY1 int
	Strength           int // |μ_a − μ_b|
}

// Feasibility 是可行性判定与冲突定位的结果。
type Feasibility struct {
	Rho, Tau        int
	EdgeThreshold   int // θ = 2ρ+τ，块间边缘强度阈值
	CornerThreshold int // 2ρ+2τ
	EdgeMax         int
	CornerMax       int
	EdgeOK          bool
	CornerOK        bool
	Feasible        bool
	ConflictEdges   []BlockEdge
	ConflictBlocks  [][2]int
}

// TierCosts 是不可行时两条约束各自的代价（灰度级）。
type TierCosts struct {
	Shortfall     int    // 不等式①超出量
	GiveUpSeamMax int    // 放弃连续性：残留最大缝宽
	GiveUpDCText  string // 放弃直流：必须达到的最大直流偏差（可为 .5）
}

// TierResult 是某一档块长的完整实测。
type TierResult struct {
	BlockSize       int
	Feasible        bool
	EdgeThreshold   int
	CornerThreshold int
	EdgeMax         int
	EdgeMean        float64
	CornerMax       int
	CornerMean      float64

	ConflictEdgeCount int
	ConflictBlocks    [][2]int

	FlatSeamMax   int
	FlatSeamMean  float64
	FlatDCDevMax  int
	FlatDCDevMean float64
	FlatMSE       float64

	// 可行档：约束内重建。
	Constrained  bool
	ConSeamMax   int
	ConSeamMean  float64
	ConDCDevMax  int
	ConDCDevMean float64
	ConMSE       float64

	// 不可行档：两条重建的实测 + 解析代价。
	Costs TierCosts

	KeepDCSeamMax   int
	KeepDCSeamMean  float64
	KeepDCDCDevMax  int
	KeepDCDCDevMean float64
	KeepDCMSE       float64

	KeepSeamSeamMax   int
	KeepSeamSeamMean  float64
	KeepSeamDCDevMax  int
	KeepSeamDCDevMean float64
	KeepSeamMSE       float64
}

// StudyReport 是三档块长实验的完整结果。
type StudyReport struct {
	Fixture  string
	W, H     int
	Rho, Tau int
	Tiers    []TierResult
}

// RunConstraintStudy 在内置测试图上对 B=4/8/16 三档跑约束实验。
func RunConstraintStudy(fixture string, w, h int) (*StudyReport, error) {
	name, ok := fixtureName(fixture)
	if !ok {
		return nil, ErrUnknownFixture
	}
	s := codec.RunStudy(name, w, h, codec.StudyParams)
	return toStudyReport(fixture, s), nil
}

func fixtureName(s string) (codec.FixtureName, bool) {
	switch s {
	case string(codec.FixtureGradient):
		return codec.FixtureGradient, true
	case string(codec.FixtureQuadratic):
		return codec.FixtureQuadratic, true
	case string(codec.FixtureTexture):
		return codec.FixtureTexture, true
	}
	return "", false
}

func toStudyReport(fixture string, s codec.StudyResult) *StudyReport {
	r := &StudyReport{Fixture: fixture, W: s.W, H: s.H, Rho: s.Params.Rho, Tau: s.Params.Tau}
	for _, t := range s.Tiers {
		tr := TierResult{
			BlockSize: t.BlockSize, Feasible: t.Feasible,
			EdgeThreshold: t.EdgeThreshold, CornerThreshold: t.CornerThreshold,
			EdgeMax: t.EdgeMax, EdgeMean: milliToFloat(t.EdgeMeanMilli),
			CornerMax: t.CornerMax, CornerMean: milliToFloat(t.CornerMeanMilli),
			ConflictEdgeCount: len(t.ConflictEdges), ConflictBlocks: t.ConflictBlocks,
			FlatSeamMax: t.FlatSeam.Max, FlatSeamMean: milliToFloat(t.FlatSeam.MeanMilli),
			FlatDCDevMax: t.FlatDCDev.Max, FlatDCDevMean: milliToFloat(t.FlatDCDev.MeanMilli),
			FlatMSE: t.FlatMSE,
		}
		if t.Feasible {
			tr.Constrained = t.Constrained
			tr.ConSeamMax = t.ConstrainedSeam.Max
			tr.ConSeamMean = milliToFloat(t.ConstrainedSeam.MeanMilli)
			tr.ConDCDevMax = t.ConstrainedDCDev.Max
			tr.ConDCDevMean = milliToFloat(t.ConstrainedDCDev.MeanMilli)
			tr.ConMSE = t.ConstrainedMSE
		} else {
			q := codec.CheckFeasibility(codec.BlockDCField(s.W, s.H, t.BlockSize, codec.FixtureImage(codec.FixtureName(fixture), s.W, s.H)), codec.StudyParams).TierCosts()
			tr.Costs = TierCosts{Shortfall: q.Shortfall, GiveUpSeamMax: q.SeamGiveUpMax, GiveUpDCText: q.DCGiveUpMaxText()}
			tr.KeepDCSeamMax = t.KeepDC.SeamMax
			tr.KeepDCSeamMean = milliToFloat(t.KeepDC.SeamMeanMilli)
			tr.KeepDCDCDevMax = t.KeepDC.DCDevMax
			tr.KeepDCDCDevMean = milliToFloat(t.KeepDC.DCDevMeanMilli)
			tr.KeepDCMSE = t.KeepDC.MSE
			tr.KeepSeamSeamMax = t.KeepSeam.SeamMax
			tr.KeepSeamSeamMean = milliToFloat(t.KeepSeam.SeamMeanMilli)
			tr.KeepSeamDCDevMax = t.KeepSeam.DCDevMax
			tr.KeepSeamDCDevMean = milliToFloat(t.KeepSeam.DCDevMeanMilli)
			tr.KeepSeamMSE = t.KeepSeam.MSE
		}
		r.Tiers = append(r.Tiers, tr)
	}
	return r
}

func milliToFloat(v int) float64 { return float64(v) / 1000 }

// ErrUnknownFixture 表示实验图名未知。
var ErrUnknownFixture = errUnknownFixture{}

type errUnknownFixture struct{}

func (errUnknownFixture) Error() string { return "未知实验图" }

// 实验图名（公共取值）。
const (
	FixtureGradient  = string(codec.FixtureGradient)
	FixtureQuadratic = string(codec.FixtureQuadratic)
	FixtureTexture   = string(codec.FixtureTexture)
)
