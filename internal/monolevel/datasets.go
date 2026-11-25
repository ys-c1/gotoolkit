package monolevel

import "math"

// Tier 是一档步长与对应数据集的名字。
type Tier struct {
	Name  string
	Steps []int
}

// Tiers 是三档步长序列。第一档等宽；第二档按几何增长（暗部细、亮部粗）；
// 第三档模拟感知步长（暗部与亮部细、中间粗）。三档都满足正整数且和为 256。
var Tiers = []Tier{
	{Name: "uniform-8", Steps: []int{32, 32, 32, 32, 32, 32, 32, 32}},
	{Name: "geometric-16", Steps: []int{
		6, 7, 8, 8, 9, 11, 12, 13, 15, 17, 19, 21, 23, 26, 29, 32,
	}},
	{Name: "perceptual-32", Steps: []int{
		3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 9, 9,
		9, 9, 9, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	}},
}

// DatasetN 是每档数据集的样本总数（训练/测试各一半）。
const DatasetN = 262144

// tierEdge 是逐格“靠边”质量占比：每格把大部分质量压在靠近某一侧边的窄条里，
// 偶数格靠右、奇数格靠左。它让条件均值逐格左右交替，从而凑出的名义表必然交叉。
const tierEdge = 0.78

// TierHistogram 生成第 t 档步长对应的确定性直方图，长度 256、总和 DatasetN。
//
// 构造：每格配额按 1+0.30·sin(2πk/L+0.7) 归一分配；格内把 tierEdge 的质量按
// 三角权重压在靠边 m=max(1,Δ/4) 个整数点上，其余均匀铺开。全部整数运算，
// 结果与运行平台无关。
func TierHistogram(t int) ([]uint64, error) {
	if t < 0 || t >= len(Tiers) {
		return nil, ErrBadSteps
	}
	steps := Tiers[t].Steps
	bounds, err := Partition(steps)
	if err != nil {
		return nil, err
	}
	l := len(steps)
	env := make([]float64, l)
	var envSum float64
	for k := range steps {
		env[k] = 1 + 0.30*math.Sin(2*math.Pi*float64(k)/float64(l)+0.7)
		envSum += env[k]
	}
	hist := make([]uint64, DomainN)
	assigned := 0
	for k := 0; k < l; k++ {
		quota := int(math.Floor(float64(DatasetN) * env[k] / envSum))
		if k == l-1 {
			quota = DatasetN - assigned
		}
		assigned += quota

		lo, hi := bounds[k], bounds[k+1]
		width := hi - lo
		m := width / 4
		if m < 1 {
			m = 1
		}
		add := make([]float64, width)
		var patSum float64
		pat := make([]float64, m)
		for j := 0; j < m; j++ {
			if k%2 == 0 {
				pat[j] = 1.0 - 0.7*float64(j)/float64(maxInt(m-1, 1))
			} else {
				pat[j] = 0.3 + 0.7*float64(j)/float64(maxInt(m-1, 1))
			}
			patSum += pat[j]
		}
		uniform := (1 - tierEdge) / float64(width)
		for j := range add {
			add[j] = uniform
		}
		if k%2 == 0 {
			for j := 0; j < m; j++ {
				add[width-m+j] += tierEdge * pat[j] / patSum
			}
		} else {
			for j := 0; j < m; j++ {
				add[j] += tierEdge * pat[j] / patSum
			}
		}
		// 按配额取整，余数补给最大格，保证非负且总和精确。
		cell := make([]uint64, width)
		var got int
		maxIdx := 0
		for j := range add {
			v := int(math.Round(add[j] * float64(quota)))
			cell[j] = uint64(v)
			got += v
			if v > int(cell[maxIdx]) {
				maxIdx = j
			}
		}
		cell[maxIdx] = uint64(int(cell[maxIdx]) + quota - got)
		for j := range cell {
			hist[lo+j] = cell[j]
		}
	}
	return hist, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
