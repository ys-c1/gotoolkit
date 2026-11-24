package lowbit

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// 抖动核的两种形态。
type Kind int

const (
	Diffusion Kind = iota // 误差扩散
	Ordered               // 有序抖动（阈值矩阵）
)

// 核相关错误。
var (
	ErrZeroSum   = errors.New("核权重和为零")
	ErrBadKernel = errors.New("核不合法")
)

// Kernel 是一个方形核，row-major 存放，中心权重必须为 0。
type Kernel struct {
	Name string
	N    int
	Kind Kind
	W    []int
}

// ErrZeroWeights 是规范要求的“核权重为零”错误哨兵。
var ErrZeroWeights = ErrZeroSum

func newKernel(name string, kind Kind, n int, w []int) *Kernel {
	return &Kernel{Name: name, Kind: kind, N: n, W: w}
}

// Builtins 返回内置核。键名同时是命令行的 --kernel 取值。
func Builtins() map[string]*Kernel {
	return map[string]*Kernel{
		"floyd-steinberg": newKernel("floyd-steinberg", Diffusion, 3, []int{
			0, 0, 0,
			0, 0, 7,
			3, 5, 1,
		}),
		"bayer": newKernel("bayer", Ordered, 4, []int{
			0, 8, 2, 10,
			12, 4, 14, 6,
			3, 11, 1, 9,
			15, 7, 13, 5,
		}),
	}
}

// Builtin 查找内置核。
func Builtin(name string) (*Kernel, bool) {
	k, ok := Builtins()[name]
	return k, ok
}

// ParseKernel 解析形如 "0,0,7;3,0,5;1,0,0" 的自定义扩散核。
// 空片段（行尾多余分号）忽略；必须是奇数阶方阵；中心权重必须为 0。
func ParseKernel(s string) (*Kernel, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("%w: 权重为空", ErrBadKernel)
	}
	rows := strings.Split(s, ";")
	var w []int
	n := -1
	for _, row := range rows {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		parts := strings.Split(row, ",")
		if n == -1 {
			n = len(parts)
		}
		if len(parts) != n {
			return nil, fmt.Errorf("%w: 非方阵", ErrBadKernel)
		}
		for _, p := range parts {
			v, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil || v < 0 {
				return nil, fmt.Errorf("%w: 权重 %q 必须是非负整数", ErrBadKernel, p)
			}
			w = append(w, v)
		}
	}
	if n <= 0 || len(w) != n*n {
		return nil, fmt.Errorf("%w: 空核", ErrBadKernel)
	}
	if n%2 == 0 {
		return nil, fmt.Errorf("%w: 扩散核必须是奇数阶（中心权重为 0）", ErrBadKernel)
	}
	c := n / 2
	if w[c*n+c] != 0 {
		return nil, fmt.Errorf("%w: 中心权重必须为 0", ErrBadKernel)
	}
	k := newKernel("custom", Diffusion, n, w)
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return k, nil
}

// Sum 是未归一化的权重和。
func (k *Kernel) Sum() int {
	s := 0
	for _, v := range k.W {
		s += v
	}
	return s
}

// Q8 返回归一到 1 的核权重（定点 256 表示 1）。
// 用最大余数法分配，保证总和恰为 Q8One，而不是逐项截断后的近似值。
func (k *Kernel) Q8() ([]int, error) {
	s := k.Sum()
	if s == 0 {
		return nil, fmt.Errorf("%w: %s", ErrZeroSum, k.Name)
	}
	out := make([]int, len(k.W))
	rem := make([]int, len(k.W))
	assigned := 0
	for i, v := range k.W {
		out[i] = v * Q8One / s
		rem[i] = v * Q8One % s
		assigned += out[i]
	}
	// 把剩下的 Q8One-assigned 个定点单位按余数从大到小补给各项（余数相同取下标小者）。
	for assigned < Q8One {
		best, bestRem := -1, -1
		for i := range rem {
			if rem[i] > bestRem {
				best, bestRem = i, rem[i]
			}
		}
		if best < 0 {
			break
		}
		out[best]++
		rem[best] = -1
		assigned++
	}
	return out, nil
}

// Q8One 是定点表示中的 1.0。
const Q8One = 256

// Validate 检查扩散核的因果支撑：不得引用已处理像素，且权重和不得为零。
func (k *Kernel) Validate() error {
	if k.Kind == Ordered {
		if k.Sum() == 0 {
			return fmt.Errorf("%w: %s", ErrZeroSum, k.Name)
		}
		return nil
	}
	c := k.N / 2
	for dy := 0; dy < k.N; dy++ {
		for dx := 0; dx < k.N; dx++ {
			v := k.W[dy*k.N+dx]
			if v == 0 {
				continue
			}
			offY, offX := dy-c, dx-c
			if offY < 0 || (offY == 0 && offX <= 0) {
				return fmt.Errorf("%w: %s 在 (%d,%d) 引用了已处理像素", ErrBadKernel, k.Name, offY, offX)
			}
		}
	}
	if k.Sum() == 0 {
		return fmt.Errorf("%w: %s", ErrZeroSum, k.Name)
	}
	return nil
}

// Fractions 返回归一化权重的最简分数写法，例如 "7/16"。
func (k *Kernel) Fractions() []string {
	s := k.Sum()
	out := make([]string, len(k.W))
	for i, v := range k.W {
		if v == 0 {
			out[i] = "0"
			continue
		}
		out[i] = fmt.Sprintf("%d/%d", v, s)
	}
	return out
}

// Normalized 返回核权重按 1 归一后的浮点值，供报告与校验。
func (k *Kernel) Normalized() []float64 {
	s := k.Sum()
	out := make([]float64, len(k.W))
	for i, v := range k.W {
		if s != 0 {
			out[i] = float64(v) / float64(s)
		}
	}
	return out
}
