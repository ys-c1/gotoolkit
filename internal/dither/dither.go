// Package dither 实现带误差反馈（error feedback）的标量量化。
//
// 闭环（行方向一维，每行开始时反馈状态复位）：
//
//	u[n] = x[n] + d[n] - beta·e[n-1]
//	y[n] = Q(u[n])                    量化级 delta，饱和到 [0,255]
//	e[n] = y[n] - u[n]
//
// 噪声传递函数 NTF(z) = 1 - beta·z⁻¹ 为 FIR（全零点）：
// |beta| <= 1 时零点在单位圆内/上，回路 BIBO 稳定；beta > 1 不做保证。
package dither

import (
	"errors"
)

// pcg 是最小实现的 PCG-XSH-RR 64/32 随机源，避免依赖高版本 math/rand/v2，
// 固定种子下序列确定，保证量化结果可复现。
type pcg struct {
	state uint64
	inc   uint64
}

func newPCG(seed uint64) *pcg {
	p := &pcg{inc: seed<<1 | 1}
	p.state = 0
	p.next()
	p.state += seed
	p.next()
	return p
}

func (p *pcg) next() uint32 {
	old := p.state
	p.state = old*6364136223846793005 + p.inc
	x := uint32(((old >> 18) ^ old) >> 27)
	rot := uint32(old >> 59)
	return (x >> rot) | (x << ((-rot) & 31))
}

// float64 返回 [0,1) 的均匀随机数。
func (p *pcg) float64() float64 {
	// 两个 32 位拼成 53 位尾数
	hi := uint64(p.next()) << 21
	lo := uint64(p.next()) >> 11
	return float64(hi|lo) / (1 << 53)
}

var (
	ErrBadLevel  = errors.New("量化级数必须在 [2,256]")
	ErrBadBeta   = errors.New("反馈系数必须在 [-1,1]")
	ErrBadOption = errors.New("参数越界")
	ErrBadSize   = errors.New("画幅必须为正")
)

// Options 是误差反馈量化的全部可配参数。
type Options struct {
	Levels      int     // 量化级数 2..256，默认 16
	Beta        float64 // 误差反馈系数，[-1,1]；0 表示关闭反馈（原路径）
	Seed        uint64  // RPDF 抖动随机种子；固定种子保证可复现
	DitherScale float64 // 抖动幅度相对满幅 ±delta/2 的比例，(0,1]；默认 1
}

// DefaultOptions 返回默认参数：16 级、无反馈、满幅 RPDF 抖动。
func DefaultOptions() Options {
	return Options{Levels: 16, Beta: 0, Seed: 1, DitherScale: 1}
}

func (o Options) validate() error {
	if o.Levels < 2 || o.Levels > 256 {
		return ErrBadLevel
	}
	if o.Beta < -1 || o.Beta > 1 {
		return ErrBadBeta
	}
	// 零值按默认满幅处理；显式传非正数或大于 1 才拒绝。
	if o.DitherScale != 0 && (o.DitherScale < 0 || o.DitherScale > 1) {
		return ErrBadOption
	}
	return nil
}

func (o Options) ditherScale() float64 {
	if o.DitherScale == 0 {
		return 1
	}
	return o.DitherScale
}

// Quantize 对灰度图做带 RPDF 抖动与误差反馈的标量量化。
// 输入 pix 长度必须为 w*h；返回与输入等长的量化结果，不改写输入。
func Quantize(w, h int, pix []byte, opt Options) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, ErrBadSize
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	// 256 级时 delta=1，退化为恒等映射。
	delta := 256.0 / float64(opt.Levels)
	rng := newPCG(opt.Seed)
	amp := delta / 2.0 * opt.ditherScale() // RPDF 抖动幅度

	out := make([]byte, len(pix))
	for row := 0; row < h; row++ {
		ePrev := 0.0
		base := row * w
		for col := 0; col < w; col++ {
			i := base + col
			d := rng.float64()*2*amp - amp
			u := float64(pix[i]) + d - opt.Beta*ePrev
			q := round(u / delta)
			if q < 0 {
				q = 0
			}
			if q > float64(opt.Levels)-1 {
				q = float64(opt.Levels) - 1
			}
			y := q * delta
			if y > 255 {
				y = 255
			}
			e := y - u
			out[i] = byte(round(y))
			ePrev = e
		}
	}
	return out, nil
}

// round 实现数学四舍五入（向远离 0 方向），显式写出避免浮点边界歧义。
func round(v float64) float64 {
	if v < 0 {
		return float64(int64(v - 0.5))
	}
	return float64(int64(v + 0.5))
}
