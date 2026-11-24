package quantfb

import (
	"fmt"
	"math"
)

// Cutoff 是「低频」的角频率截止 ωc=π/4（对应 DFT 前 1/8 频带）。
const Cutoff = math.Pi / 4

// TStar 给出 DC 置零约束下的最优第二零点 t*。
//
// 令 c1=−(1+t)、c2=t，则 NTF=(1−z⁻¹)(1−t z⁻¹)，
// 在白噪声误差假设下最小化低频带能量
//
//	J(t)=∫_0^ωc 2(1−cosω)(1+t²−2t cosω)dω，
//
// 一阶条件给出闭式解
//
//	t* = [sinωc − ωc/2 − sin2ωc/4] / [ωc − sinωc]
//	   = [4sinωc − 2ωc − sin2ωc] / [4(ωc − sinωc)]。
func TStar(wc float64) float64 {
	s := math.Sin(wc)
	return (4*s - 2*wc - math.Sin(2*wc)) / (4 * (wc - s))
}

// JointCoeffs 返回不动点处的两个反馈系数（高精度）：
// c1=−(1+t*)≈−1.8226665269，c2=t*≈+0.8226665269。
func JointCoeffs() (c1, c2 float64) {
	t := TStar(Cutoff)
	return -(1 + t), t
}

// UnconstrainedCoeffs 是不强制 DC 置零、直接对带能二次型解 2×2 法方程
// 得到的另一组系数：(−1.7270560686,+0.9182769851)。
//
// 令 r1=sinωc/ωc、r2=sin2ωc/(2ωc)，则
//
//	h2=(r1²−r2)/(1−r1²)，h1=−(1+h2)r1。
//
// 它的 DC 增益 1+h1+h2≈0.191（没有精确零 DC），在纯渐变上实测行低频略逊
// DC 置零解；但在 ωc=π/4 下其 L1 范数与 JointCoeffs 精确相同，
// 因而给出同一个步长 q*=10 与同一条误差界。列出它是为了覆盖另一种常见口径。
func UnconstrainedCoeffs(wc float64) (h1, h2 float64) {
	r1 := math.Sin(wc) / wc
	r2 := math.Sin(2*wc) / (2 * wc)
	h2 = (r1*r1 - r2) / (1 - r1*r1)
	h1 = -(1 + h2) * r1
	return h1, h2
}

// CoeffL1 是输出误差界里的系数范数因子 |c1|+|c2|。
func CoeffL1(c1, c2 float64) float64 { return math.Abs(c1) + math.Abs(c2) }

// BandPower 是 NTF=1+c1 z⁻¹+c2 z⁻² 在 [−ωc,ωc] 上的归一化带内能量
// （白噪声误差谱的系数目标函数，闭式积分，整体常数不影响最优解）。
func BandPower(c1, c2, wc float64) float64 {
	s := math.Sin(wc)
	num := wc*(1+c1*c1+c2*c2) + 2*(c1+c1*c2)*s + c2*math.Sin(2*wc)
	return num / math.Pi
}

// ACap 是给定步长 q 时，误差预算 (1+‖c‖₁)q/2 ≤ BudgetD
// 与稳健上限 ‖c‖₁ ≤ L1Cap 共同允许的系数 L1 半径：
//
//	Acap(q)=min(L1Cap, 2·BudgetD/q − 1)。
func ACap(q float64) float64 {
	return math.Min(L1Cap, 2*BudgetD/q-1)
}

// CoeffsForStep 执行交替迭代的「固定量化步长，解反馈系数」半步。
//
// 在 ‖c‖₁≤Acap(q)、c1≤0、c2≥0、1+c1+c2≥0 的可行域内最小化 BandPower。
// 不动点附近无约束最优核的范数 L1*=2.64533…<Acap，直接取 JointCoeffs；
// 当预算收紧、约束生效时，最优点落在 DC 置零射线 c1=−(1+t),c2=t 上，
// 由 1+2t=Acap 得 t=(Acap−1)/2；Acap<1 时退到角点 (−Acap,0)。
func CoeffsForStep(q float64) (c1, c2, acap float64, capped bool) {
	acap = ACap(q)
	s1, s2 := JointCoeffs()
	l1 := CoeffL1(s1, s2)
	if l1 <= acap+1e-12 {
		return s1, s2, acap, false
	}
	if acap < 1 {
		return -math.Max(acap, 0), 0, acap, true
	}
	t := (acap - 1) / 2
	return -(1 + t), t, acap, true
}

// ContinuousStep 执行「固定反馈系数，解量化步长」的连续版本：
// 取满足误差预算的最大实数步长 Δ=2·BudgetD/(1+‖c‖₁)。
func ContinuousStep(c1, c2 float64) float64 {
	return 2 * BudgetD / (1 + CoeffL1(c1, c2))
}

// StepForCoeffs 是 StepSolve 的整数（灰阶步长）版本：
// q*=⌊2·BudgetD/(1+‖c‖₁)⌋，且至少为 1。
func StepForCoeffs(c1, c2 float64) int {
	q := int(math.Floor(ContinuousStep(c1, c2) + 1e-12))
	if q < 1 {
		q = 1
	}
	return q
}

// Iteration 是一轮交替迭代的完整记录，便于报告与测试核对。
type Iteration struct {
	Round int     `json:"round"`
	Q     int     `json:"q"`
	ACap  float64 `json:"a_cap"`
	C1    float64 `json:"c1"`
	C2    float64 `json:"c2"`
	L1    float64 `json:"l1"`
	Bound float64 `json:"bound"`  // (1+L1)·q/2
	QNext int     `json:"q_next"` // 固定本轮系数解出的下一步长
}

// Alternate 从初始整数步长 q0 开始，严格按
// 「固定 q 解 (c1,c2) → 固定 (c1,c2) 解 q」交替，
// 直到 q 与系数都不再变化（或达到 50 轮保护）。
func Alternate(q0 int) []Iteration {
	if q0 < 1 {
		q0 = 1
	}
	var trace []Iteration
	q := q0
	prevC1, prevC2 := math.NaN(), math.NaN()
	for round := 0; round < 50; round++ {
		c1, c2, acap, _ := CoeffsForStep(float64(q))
		l1 := CoeffL1(c1, c2)
		next := StepForCoeffs(c1, c2)
		trace = append(trace, Iteration{
			Round: round, Q: q, ACap: acap, C1: c1, C2: c2, L1: l1,
			Bound: (1 + l1) * float64(q) / 2, QNext: next,
		})
		if next == q && math.Abs(c1-prevC1) < 1e-12 && math.Abs(c2-prevC2) < 1e-12 {
			break
		}
		prevC1, prevC2 = c1, c2
		q = next
	}
	return trace
}

// FixedPoint 是推荐入口：从 B 组系数 (−2,+1) 先解出初始可行步长 q0=9，
// 再执行规定顺序的交替迭代。返回完整轨迹与最终一轮。
func FixedPoint() (trace []Iteration, final Iteration) {
	q0 := StepForCoeffs(BTunedCoeffs[0], BTunedCoeffs[1]) // 9
	trace = Alternate(q0)
	return trace, trace[len(trace)-1]
}

// ContinuousFixedPoint 给出无整数截断时的连续不动点数值，供文档核对：
// Δ*=10.1499642020…，且 Acap(Δ*) 恰好等于 L1*=2.6453330538。
func ContinuousFixedPoint() (delta, t, c1, c2, l1, acap float64) {
	c1, c2 = JointCoeffs()
	l1 = CoeffL1(c1, c2)
	delta = 2 * BudgetD / (1 + l1)
	acap = ACap(delta)
	t = TStar(Cutoff)
	return
}

// String 便于打印关键系数。
func (it Iteration) String() string {
	return fmt.Sprintf("r%d q=%2d acap=%.4f c=(%+.9f,%+.9f) L1=%.6f B=%.4f q'=%d",
		it.Round, it.Q, it.ACap, it.C1, it.C2, it.L1, it.Bound, it.QNext)
}
