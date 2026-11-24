// Package palette 实现灰度图的中位切分量化。
//
// 这是只在 api.Options.Palette=true 时启用的可选像素变换：文件头仍由原有
// codec 包写出，因此关闭调色板时编码字节与历史产物完全一致。
package palette

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

const (
	// MaxBuckets 是 8 位灰度值可区分的最大桶数。
	MaxBuckets = 256
	// ValueMax 是最大灰度值。
	ValueMax = 255
)

// ErrBuckets 表示桶数不在 [1, MaxBuckets] 内。
var ErrBuckets = errors.New("调色板桶数非法")

// Order 决定每一轮从哪里选待切桶。
// 正式编码始终使用 OrderVariance；其它顺序只用于误差曲线对照实验。
type Order string

const (
	// OrderVariance 每轮选择桶内方差最大的可切桶，并列时像素多者优先。
	OrderVariance Order = "variance"
	// OrderLeftmost 每轮选择灰度区间最靠左的可切桶，形成深度优先切分。
	// 它是“分裂顺序会改变误差曲线”的反例，不是正式编码路径。
	OrderLeftmost Order = "leftmost"
)

// Options 是中位切分参数。
type Options struct {
	Buckets int
	Order   Order
}

// Split 记录一次桶分裂，便于复算分裂顺序与误差下降。
type Split struct {
	Step     int     // 第几次分裂，从 1 开始
	Axis     int     // 灰度空间只有一个值维，恒为 0
	Variance float64 // 父桶分裂前的总体方差
	Pivot    uint8   // 下中位灰度值
	LeftN    int     // 左桶像素数
	RightN   int     // 右桶像素数
}

// Result 保存量化像素、调色板和误差统计。
type Result struct {
	Pix     []uint8
	Palette []uint8
	Splits  []Split
	Actual  int
	MSE     float64 // 均方误差
	MAE     float64 // 平均绝对误差
	RMSE    float64 // 均方根误差
	MaxAbs  int     // 最大绝对误差
	SSE     float64 // 误差平方和
}

type bucket struct {
	lo, hi int
	id     int
	mean   float64
	vari   float64
	plain  bool
}

// Quantize 使用方差优先顺序做中位切分。
func Quantize(pix []uint8, buckets int) (*Result, error) {
	return QuantizeOrdered(pix, Options{Buckets: buckets, Order: OrderVariance})
}

// QuantizeOrdered 按指定桶选择顺序做中位切分。
func QuantizeOrdered(pix []uint8, opt Options) (*Result, error) {
	if len(pix) == 0 {
		return nil, fmt.Errorf("%w: 像素为空", ErrBuckets)
	}
	if opt.Buckets < 1 || opt.Buckets > MaxBuckets {
		return nil, fmt.Errorf("%w: K=%d 必须在 [1,%d]", ErrBuckets, opt.Buckets, MaxBuckets)
	}
	if opt.Order == "" {
		opt.Order = OrderVariance
	}
	if opt.Order != OrderVariance && opt.Order != OrderLeftmost {
		return nil, fmt.Errorf("%w: 未知分裂顺序 %q", ErrBuckets, opt.Order)
	}

	order := make([]int, len(pix))
	for i := range order {
		order[i] = i
	}
	// 所有桶都是这个有序索引数组上的连续区间；只排序一次，保证结果可复现。
	sort.SliceStable(order, func(i, j int) bool {
		if pix[order[i]] != pix[order[j]] {
			return pix[order[i]] < pix[order[j]]
		}
		return order[i] < order[j]
	})

	buckets := []*bucket{{lo: 0, hi: len(pix), id: 0}}
	computeBucket(buckets[0], pix, order)
	nextID := 1
	var splits []Split

	for len(buckets) < opt.Buckets {
		idx := selectBucket(buckets, opt.Order)
		if idx < 0 {
			break
		}
		parent := buckets[idx]
		k := splitIndex(parent, pix, order)
		if k <= parent.lo || k >= parent.hi {
			parent.plain = true
			continue
		}

		left := &bucket{lo: parent.lo, hi: k, id: nextID}
		nextID++
		right := &bucket{lo: k, hi: parent.hi, id: nextID}
		nextID++
		computeBucket(left, pix, order)
		computeBucket(right, pix, order)

		n := parent.hi - parent.lo
		pivot := pix[order[parent.lo+(n-1)/2]]
		splits = append(splits, Split{
			Step:     len(splits) + 1,
			Axis:     0,
			Variance: parent.vari,
			Pivot:    pivot,
			LeftN:    left.hi - left.lo,
			RightN:   right.hi - right.lo,
		})

		buckets[idx] = left
		buckets = append(buckets, right)
	}

	sort.Slice(buckets, func(i, j int) bool { return buckets[i].lo < buckets[j].lo })

	out := make([]uint8, len(pix))
	seen := make(map[uint8]bool, len(buckets))
	var sse, sae float64
	maxAbs := 0
	palette := make([]uint8, 0, len(buckets))

	for _, b := range buckets {
		p := roundMean(b.mean)
		if !seen[p] {
			seen[p] = true
			palette = append(palette, p)
		}
		for _, id := range order[b.lo:b.hi] {
			out[id] = p
			d := int(pix[id]) - int(p)
			ad := d
			if ad < 0 {
				ad = -ad
			}
			sae += float64(ad)
			sse += float64(d * d)
			if ad > maxAbs {
				maxAbs = ad
			}
		}
	}
	sort.Slice(palette, func(i, j int) bool { return palette[i] < palette[j] })

	n := float64(len(pix))
	return &Result{
		Pix:     out,
		Palette: palette,
		Splits:  splits,
		Actual:  len(buckets),
		MSE:     sse / n,
		MAE:     sae / n,
		RMSE:    math.Sqrt(sse / n),
		MaxAbs:  maxAbs,
		SSE:     sse,
	}, nil
}

func selectBucket(bs []*bucket, order Order) int {
	best := -1
	for i, b := range bs {
		if b.plain {
			continue
		}
		if best == -1 {
			best = i
			continue
		}
		switch order {
		case OrderLeftmost:
			if b.lo < bs[best].lo {
				best = i
			}
		default:
			cur, old := b, bs[best]
			cn, on := cur.hi-cur.lo, old.hi-old.lo
			if cur.vari > old.vari ||
				(cur.vari == old.vari && cn > on) ||
				(cur.vari == old.vari && cn == on && cur.id < old.id) {
				best = i
			}
		}
	}
	return best
}

func computeBucket(b *bucket, pix []uint8, order []int) {
	n := b.hi - b.lo
	var sum, sumSq int
	mn, mx := ValueMax, 0
	for _, id := range order[b.lo:b.hi] {
		v := int(pix[id])
		sum += v
		sumSq += v * v
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	fn := float64(n)
	mean := float64(sum) / fn
	vari := float64(sumSq)/fn - mean*mean
	if vari < 0 {
		vari = 0
	}
	b.mean = mean
	b.vari = vari
	b.plain = mn == mx
}

// splitIndex 返回右桶在 order 中的起点。首选第一个严格大于下中位值的位置；
// 若中位值已是区间最大值，则退到第一个大于等于它的位置，保证两侧非空。
func splitIndex(b *bucket, pix []uint8, order []int) int {
	n := b.hi - b.lo
	pivot := pix[order[b.lo+(n-1)/2]]
	off := sort.Search(n, func(i int) bool {
		return pix[order[b.lo+i]] > pivot
	})
	if off == n {
		off = sort.Search(n, func(i int) bool {
			return pix[order[b.lo+i]] >= pivot
		})
	}
	return b.lo + off
}

func roundMean(v float64) uint8 {
	q := int(math.Round(v))
	if q < 0 {
		return 0
	}
	if q > ValueMax {
		return ValueMax
	}
	return uint8(q)
}
