package quantfb

// Tap 是扩散核中的一个权重，DX/DY 为相对当前像素（0,0）的位移。
type Tap struct {
	DX, DY int
	W      float64
}

// DiffusionKernel 是一个前向误差扩散核：权重除以 Div 后使用。
type DiffusionKernel struct {
	Name string
	Div  float64
	Taps []Tap
	Note string
}

// OriginalFloydSteinberg 是 **A 组使用的现成扩散核原核**。
//
// 这是留作底稿的原始 kernel（Floyd–Steinberg 1976），数值刻意保持原样，
// 联合设计的新二阶反馈核在 design.go / quant.go 中另行实现，
// **不要删除或修改这里的四个权重**；测试 TestOriginalKernelPreserved 会锁定。
//
//   - 7/16
//     3/16 5/16 1/16
func OriginalFloydSteinberg() DiffusionKernel {
	return DiffusionKernel{
		Name: "A-floyd-steinberg-original",
		Div:  16,
		Taps: []Tap{
			{DX: 1, DY: 0, W: 7},  // 右
			{DX: -1, DY: 1, W: 3}, // 左下
			{DX: 0, DY: 1, W: 5},  // 正下
			{DX: 1, DY: 1, W: 1},  // 右下
		},
		Note: "A 组现成扩散核原核（底稿，保留勿删）",
	}
}

// BTunedCoeffs 是 **B 组「只调系数、不联调步长」** 的朴素二阶反馈系数。
//
// 它对应 NTF=(1−z⁻¹)²=1−2z⁻¹+z⁻²，即 c1=−2、c2=+1，
// 阶数同样是二，但仍套用 A 组 8 级量化的粗步长 q≈37。
var BTunedCoeffs = [2]float64{-2, 1}

// AGroupStep 是 A/B 两组共用的 8 级 palette 的整数量化步长上限。
// palette 为 [0,36,73,109,146,182,219,255]，最大半格误差 D0=18.5。
const AGroupStep = 37

// Palette8 是 A 组 8 级重建电平（由 round(k·255/7) 生成，逐项列出便于核对）。
var Palette8 = []float64{0, 36, 73, 109, 146, 182, 219, 255}

// BudgetD 是「量化误差不许上升」的最坏情况预算：
// 新方案输出误差的理论上界不得超过 A 组量化器的最大误差 qA/2=18.5。
const BudgetD = 18.5

// L1Cap 是系数 L1 范数的稳健上限；朴素二阶双积分核 (−2,+1) 的范数恰为 3。
const L1Cap = 3.0
