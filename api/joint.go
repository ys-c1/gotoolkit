package api

import "gotoolkit/internal/codec"

// 本文件把「预测器 × 块长」联合代价分析作为公共 API 暴露。
// 具体熵/分块算法在 internal/codec，外部模块只能看到这里的类型。

// JointBlockHeaderBytes 是每个残差块固定块头的字节数（h = 8*该值 bit）。
const JointBlockHeaderBytes = codec.BlockHeaderSize

// 预测器候选名（公共取值）。
const (
	PredictNoneName = codec.PredictNone // "none"
	PredictLeftName = codec.PredictLeft // "left"
)

// BlockStat 是单个残差块的熵与代价明细。
type BlockStat struct {
	Index       int
	Start       int
	N           int
	Entropy     float64 // 本块经验一阶熵，bit/符号
	EntropyBits float64 // N * Entropy
	HeaderBits  float64 // 块头开销
	CostBits    float64 // EntropyBits + HeaderBits
}

// JointReport 是某个 (Predict, BlockLen) 组合的联合代价统计。
//
// 联合代价（bit）定义为
//
//	C(P,B) = Σ_k n_k·H_k(P,B) + h·m,  m = ⌈N/B⌉, h = 8·JointBlockHeaderBytes
//
// 其中 H_k 是第 k 块残差的经验一阶熵。
type JointReport struct {
	Predict     string
	W, H, N     int
	BlockLen    int // 调用方给定值，0 表示整幅一块
	EffBlockLen int // 实际块长（0 展开为 N）
	Blocks      int
	HGlobal     float64
	EntropyBits float64
	HeaderBits  float64
	CostBits    float64
	CostBytes   float64
	ModelBytes  int
	BlockStats  []BlockStat
}

func toJointReport(s codec.JointStat) *JointReport {
	rs := make([]BlockStat, len(s.BlockStats))
	for i, b := range s.BlockStats {
		rs[i] = BlockStat{
			Index: b.Index, Start: b.Start, N: b.N, Entropy: b.Entropy,
			EntropyBits: b.EntropyBits, HeaderBits: b.HeaderBits, CostBits: b.CostBits,
		}
	}
	return &JointReport{
		Predict: s.Predict, W: s.W, H: s.H, N: s.N,
		BlockLen: s.BlockLen, EffBlockLen: s.EffBlockLen, Blocks: s.Blocks,
		HGlobal: s.HGlobal, EntropyBits: s.EntropyBits, HeaderBits: s.HeaderBits,
		CostBits: s.CostBits, CostBytes: s.CostBytes, ModelBytes: s.ModelBytes,
		BlockStats: rs,
	}
}

// AnalyzeJoint 计算给定预测器与块长组合的残差熵及联合代价。
// Predict 取 "none"|"left"（空串等价 none）；BlockLen==0 表示整幅一块。
func AnalyzeJoint(img *Image, predict string, blockLen int) (*JointReport, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if _, err := codec.NormalizePredictor(predict); err != nil {
		return nil, ErrBadOption
	}
	if blockLen < 0 {
		return nil, ErrBadOption
	}
	st, err := codec.AnalyzeJoint(img.W, img.H, img.Pix, predict, blockLen)
	if err != nil {
		return nil, err
	}
	return toJointReport(st), nil
}

// SolveJoint 在给定候选报告中返回联合代价最小的组合下标。
// 代价相同时取块数更少、再按预测器名确定先后。
func SolveJoint(reports []*JointReport) int {
	best := 0
	for i := 1; i < len(reports); i++ {
		a, b := reports[best], reports[i]
		switch {
		case b.CostBits < a.CostBits:
			best = i
		case b.CostBits == a.CostBits && b.Blocks < a.Blocks:
			best = i
		case b.CostBits == a.CostBits && b.Blocks == a.Blocks && b.Predict < a.Predict:
			best = i
		}
	}
	return best
}
