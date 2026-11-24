package recon

import (
	"errors"
	"fmt"
	"math"
)

// 五个抽头的固定维数与约束条数（系数和 1 条 + 中心对称 2 条逐对相等）。
const (
	TapCount  = 5
	ConCount  = 3
	KKTDim    = TapCount + ConCount
)

// ErrSingular 表示正规方程/增广 KKT 矩阵奇异，无法唯一求解。
var ErrSingular = errors.New("线性方程组奇异，主元为零")

// SolveLinear 用带部分选主元的高斯-约当消元解方阵 A x = b。
// 入参不会被修改。
func SolveLinear(A [][]float64, b []float64) ([]float64, error) {
	n := len(A)
	if n == 0 || len(A[0]) != n || len(b) != n {
		return nil, fmt.Errorf("方程维数不合法: A=%dx%d b=%d", n, colOf(A), len(b))
	}
	m := make([][]float64, n)
	for i := 0; i < n; i++ {
		m[i] = make([]float64, n+1)
		copy(m[i], A[i])
		m[i][n] = b[i]
	}
	for col := 0; col < n; col++ {
		piv := col
		for r := col + 1; r < n; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[piv][col]) {
				piv = r
			}
		}
		if math.Abs(m[piv][col]) < 1e-12 {
			return nil, fmt.Errorf("%w: 第 %d 列主元 %.3e", ErrSingular, col, m[piv][col])
		}
		m[col], m[piv] = m[piv], m[col]
		pv := m[col][col]
		for r := 0; r < n; r++ {
			if r == col {
				continue
			}
			f := m[r][col] / pv
			if f == 0 {
				continue
			}
			for c := col; c <= n; c++ {
				m[r][c] -= f * m[col][c]
			}
		}
	}
	x := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i] = m[i][n] / m[i][i]
	}
	return x, nil
}

func colOf(A [][]float64) int {
	if len(A) == 0 {
		return 0
	}
	return len(A[0])
}

// CLSResult 保存一次等式约束最小二乘的全部正规方程与解，供报告逐项核对。
//
// 目标 min ||A h - y||^2，约束 C h = d。
// 令乘子 μ = λ/2，增广正规方程（KKT）为
//
//	[AᵀA  Cᵀ] [h]   [Aᵀy]
//	[ C    0] [μ] = [ d  ]
type CLSResult struct {
	AtA  [TapCount][TapCount]float64
	Aty  [TapCount]float64
	C    [ConCount][TapCount]float64
	D    [ConCount]float64
	KKT  [KKTDim][KKTDim]float64
	RHS  [KKTDim]float64
	H    [TapCount]float64
	Mu   [ConCount]float64
}

// Constraints 返回本题固定的约束矩阵与右端：
//
//	系数和 h0+h1+h2+h3+h4 = 1
//	中心对称 h0 = h4（上 = 下）
//	中心对称 h1 = h3（左 = 右）
//
// 抽头顺序固定为 [上, 左, 中, 右, 下]。
func Constraints() (C [ConCount][TapCount]float64, d [ConCount]float64) {
	C = [ConCount][TapCount]float64{
		{1, 1, 1, 1, 1}, // 和为 1
		{1, 0, 0, 0, -1}, // h0 - h4 = 0
		{0, 1, 0, -1, 0}, // h1 - h3 = 0
	}
	d = [ConCount]float64{1, 0, 0}
	return C, d
}

// SolveConstrainedLS 从回归样本 A、y 组正规方程并解 KKT。
func SolveConstrainedLS(A [][]float64, y []float64) (*CLSResult, error) {
	if len(A) != len(y) || len(A) == 0 {
		return nil, fmt.Errorf("回归样本为空或行数不匹配: A=%d y=%d", len(A), len(y))
	}
	if len(A[0]) != TapCount {
		return nil, fmt.Errorf("特征列数应为 %d，实为 %d", TapCount, len(A[0]))
	}
	var r CLSResult
	r.C, r.D = Constraints()

	n := len(A)
	for i := 0; i < n; i++ {
		for a := 0; a < TapCount; a++ {
			r.Aty[a] += A[i][a] * y[i]
			for b := 0; b < TapCount; b++ {
				r.AtA[a][b] += A[i][a] * A[i][b]
			}
		}
	}

	// 拼 8x8 KKT 矩阵与右端。
	for a := 0; a < TapCount; a++ {
		for b := 0; b < TapCount; b++ {
			r.KKT[a][b] = r.AtA[a][b]
		}
		for k := 0; k < ConCount; k++ {
			r.KKT[a][TapCount+k] = r.C[k][a] // Cᵀ
			r.KKT[TapCount+k][a] = r.C[k][a] // C
		}
		r.RHS[a] = r.Aty[a]
	}
	for k := 0; k < ConCount; k++ {
		r.RHS[TapCount+k] = r.D[k]
	}

	kkt := make([][]float64, KKTDim)
	rhs := make([]float64, KKTDim)
	for i := 0; i < KKTDim; i++ {
		kkt[i] = append([]float64(nil), r.KKT[i][:]...)
		rhs[i] = r.RHS[i]
	}
	sol, err := SolveLinear(kkt, rhs)
	if err != nil {
		return nil, err
	}
	for a := 0; a < TapCount; a++ {
		r.H[a] = sol[a]
	}
	for k := 0; k < ConCount; k++ {
		r.Mu[k] = sol[TapCount+k]
	}
	return &r, nil
}

// ConstraintResiduals 返回三条等式各自的残差 C h - d。
func (r *CLSResult) ConstraintResiduals() [ConCount]float64 {
	var out [ConCount]float64
	for k := 0; k < ConCount; k++ {
		s := -r.D[k]
		for a := 0; a < TapCount; a++ {
			s += r.C[k][a] * r.H[a]
		}
		out[k] = s
	}
	return out
}

// KKTResidual 返回增广正规方程整体的最大逐行残差，用于验证解确实满足正规方程。
func (r *CLSResult) KKTResidual() float64 {
	var max float64
	for i := 0; i < KKTDim; i++ {
		var s float64
		for j := 0; j < KKTDim; j++ {
			s += r.KKT[i][j] * r.solAt(j)
		}
		res := math.Abs(s - r.RHS[i])
		if res > max {
			max = res
		}
	}
	return max
}

func (r *CLSResult) solAt(j int) float64 {
	if j < TapCount {
		return r.H[j]
	}
	return r.Mu[j-TapCount]
}
