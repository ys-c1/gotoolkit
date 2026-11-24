// Package target 按目标体积反解质量参数。
//
// 这里不依赖具体图像格式：调用方提供 measure(q)（通常是“用质量 q 真实编码一次，
// 返回落地字节数”）。前提是 measure 关于 q 单调不减——BLK 分块位深格式在结构上
// 严格满足这一点（见 codec.BLKSize 与 DepthForQuality）。
//
// 求解分两步：
//  1. 二分查找最小的、使体积 >= 区间下界的质量参数（lower_bound），并保留每次
//     试探的 (lo,mid,hi,实测字节) 过程；
//  2. 回代实测。若该点体积 <= 区间上界则落入目标区间；否则区间不可达，
//     再在整个质量域上选到区间距离最小的“最接近参数”，并量化差多少。
package target

import "fmt"

const (
	MinQuality = 1
	MaxQuality = 100
)

// DepthForQuality 把 1..100 的质量参数映射到 BLK 位深 1..8：
//
//	d(Q) = ceil(8Q/100)
//
// Q 越大位深越高、画质越好，且 d 关于 Q 单调不减，因此体积 S(Q) 也单调不减。
// 越界 Q 会被夹到合法域。
func DepthForQuality(q int) int {
	if q < MinQuality {
		q = MinQuality
	}
	if q > MaxQuality {
		q = MaxQuality
	}
	d := (8*q + 99) / 100 // ceil(8q/100)
	if d < 1 {
		d = 1
	}
	if d > 8 {
		d = 8
	}
	return d
}

// Iteration 是二分过程中的一次试探，Bytes 是该参数下真实编码测得的体积。
type Iteration struct {
	N     int `json:"n"`
	Lo    int `json:"lo"`
	Mid   int `json:"mid"`
	Hi    int `json:"hi"`
	Bytes int `json:"bytes"`
}

// Solution 是一次反解的完整结果。
type Solution struct {
	MinBytes int `json:"min_bytes"`
	MaxBytes int `json:"max_bytes"`

	// BinaryQuality 是二分得到的 lower_bound：最小的使体积 >= MinBytes 的 Q；
	// 若最高质量仍小于下界则为 -1。BinaryBytes 是该 Q 的实测体积。
	BinaryQuality int `json:"binary_quality"`
	BinaryBytes   int `json:"binary_bytes"`

	// Iterations 按顺序记录二分试探过程（含每次实测体积）。
	Iterations []Iteration `json:"iterations"`

	// InRange 表示最终采用参数的实测体积是否落在闭区间 [MinBytes,MaxBytes]。
	InRange bool `json:"in_range"`

	// Quality 是最终采用参数：命中时等于二分解；未命中时等于最接近参数。
	Quality int `json:"quality"`
	Bytes   int `json:"bytes"`

	// 区间外兜底：最接近区间的参数与体积，以及到区间的绝对距离（字节）。
	ClosestQuality int    `json:"closest_quality"`
	ClosestBytes   int    `json:"closest_bytes"`
	GapBytes       int    `json:"gap_bytes"`
	Side           string `json:"side"` // inside | below_min | above_max

	MeasureCalls int `json:"measure_calls"`
}

// Measure 用给定质量参数真实编码一次，返回最终落地字节数。
type Measure func(quality int) (bytes int, err error)

// Solve 在闭区间 [minBytes,maxBytes] 上反解质量参数。
//
// 允许区间不可达：此时不返回错误，而是给出全质量域上的最接近解与差距。
// 仅当区间本身非法或 measure 返回错误时才返回 error。
func Solve(minBytes, maxBytes int, measure Measure) (*Solution, error) {
	if minBytes <= 0 || maxBytes < minBytes {
		return nil, fmt.Errorf("目标区间非法: [%d,%d]", minBytes, maxBytes)
	}
	if measure == nil {
		return nil, fmt.Errorf("缺少体积测量函数")
	}

	s := &Solution{
		MinBytes: minBytes,
		MaxBytes: maxBytes,
		Side:     "inside",
	}
	cache := make(map[int]int, MaxQuality-MinQuality+1)
	m := func(q int) (int, error) {
		if n, ok := cache[q]; ok {
			return n, nil
		}
		n, err := measure(q)
		if err != nil {
			return 0, err
		}
		cache[q] = n
		s.MeasureCalls++
		return n, nil
	}

	// 1) 二分 lower_bound：最小的 Q 使 S(Q) >= minBytes。
	lo, hi := MinQuality, MaxQuality
	bq := -1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		n, err := m(mid)
		if err != nil {
			return nil, err
		}
		s.Iterations = append(s.Iterations, Iteration{
			N: len(s.Iterations) + 1, Lo: lo, Mid: mid, Hi: hi, Bytes: n,
		})
		if n < minBytes {
			lo = mid + 1
		} else {
			bq = mid
			hi = mid - 1
		}
	}
	s.BinaryQuality = bq
	if bq >= 0 {
		s.BinaryBytes = cache[bq]
	}

	// 2) 回代实测二分解（不存在 lower_bound 时取最高质量端来刻画“够不着下界”）。
	candQ := bq
	if candQ == -1 {
		candQ = MaxQuality
	}
	candBytes, err := m(candQ)
	if err != nil {
		return nil, err
	}

	if candBytes >= minBytes && candBytes <= maxBytes {
		s.InRange = true
		s.Quality = candQ
		s.Bytes = candBytes
		s.ClosestQuality = candQ
		s.ClosestBytes = candBytes
		s.GapBytes = 0
		s.Side = "inside"
		return s, nil
	}

	// 3) 区间外：在整个离散质量域上选到区间距离最小的最接近值。
	bestQ, bestBytes, bestDist := 0, 0, -1
	for q := MinQuality; q <= MaxQuality; q++ {
		n, err := m(q)
		if err != nil {
			return nil, err
		}
		dist := DistanceToRange(n, minBytes, maxBytes)
		if bestDist < 0 || dist < bestDist { // 严格小于：平局取更小 Q，结果确定
			bestDist, bestQ, bestBytes = dist, q, n
		}
	}
	s.InRange = false
	s.Quality = bestQ
	s.Bytes = bestBytes
	s.ClosestQuality = bestQ
	s.ClosestBytes = bestBytes
	s.GapBytes = bestDist
	s.Side = Classify(bestBytes, minBytes, maxBytes)
	return s, nil
}

// DistanceToRange 返回字节数 n 到闭区间 [lo,hi] 的距离：区间内为 0，
// 低于 lo 为 lo-n，高于 hi 为 n-hi。
func DistanceToRange(n, lo, hi int) int {
	if n < lo {
		return lo - n
	}
	if n > hi {
		return n - hi
	}
	return 0
}

// Classify 判定 n 相对区间的位置。
func Classify(n, lo, hi int) string {
	switch {
	case n < lo:
		return "below_min"
	case n > hi:
		return "above_max"
	default:
		return "inside"
	}
}
