package scale

import (
	"fmt"
	"math"
)

// FrameSpec 是一档用于复算的画幅。
type FrameSpec struct {
	Name string
	W, H int
}

// FrameResult 是单个画幅、单个缩放比上的整数残差实测。
type FrameResult struct {
	Name           string
	W, H           int
	OutW, OutH     int
	MaxEncodedBias int
	MaxAbsResidual int
}

// Row 是一个缩放比的解析式、码值偏差和两档画幅复算结果。
type Row struct {
	Denom      int
	Ratio      float64
	K          float64
	BiasAt32   float64
	Encoded32  byte
	Dual32     byte
	Comp32     byte
	Residual32 int
	Frames     []FrameResult
	Pass       bool
}

// DefaultFrames 返回题目要求复算补偿量的两档画幅。
func DefaultFrames() []FrameSpec {
	return []FrameSpec{
		{Name: "640x480", W: 640, H: 480},
		{Name: "1920x1080", W: 1920, H: 1080},
	}
}

// RunLab 在三档缩放比和两档画幅上构造可复现的暗边梳状图，
// 比较补偿路径与 gamma-dual 参照路径。
func RunLab(gamma, tolerance float64, frames []FrameSpec) ([]Row, error) {
	gamma = NormalizeGamma(gamma)
	if err := ValidateGamma(gamma); err != nil {
		return nil, err
	}
	if tolerance == 0 {
		tolerance = 1
	}
	if math.IsNaN(tolerance) || math.IsInf(tolerance, 0) || tolerance < 0 {
		return nil, fmt.Errorf("tolerance 越界: %v", tolerance)
	}
	if len(frames) == 0 {
		frames = DefaultFrames()
	}

	denoms := []int{2, 4, 8}
	rows := make([]Row, 0, len(denoms))
	for _, d := range denoms {
		rho := 1 / float64(d)
		enc32, dual32, comp32 := CanonicalEdgeValues(32, rho, gamma)
		row := Row{
			Denom:      d,
			Ratio:      rho,
			K:          EdgeCompensation(rho, gamma),
			BiasAt32:   EdgeBias(32, rho, gamma),
			Encoded32:  enc32,
			Dual32:     dual32,
			Comp32:     comp32,
			Residual32: int(comp32) - int(dual32),
		}

		allPass := true
		for _, fs := range frames {
			if fs.W%d != 0 || fs.H%d != 0 {
				return nil, fmt.Errorf("画幅 %s 不能被 %d 整除", fs.Name, d)
			}
			src := edgeComb(fs.W, fs.H, d)
			enc, err := Box(src, d, gamma, ModeEncoded)
			if err != nil {
				return nil, err
			}
			dual, err := Box(src, d, gamma, ModeGammaDual)
			if err != nil {
				return nil, err
			}
			comp, err := Box(src, d, gamma, ModeCompensated)
			if err != nil {
				return nil, err
			}

			maxBias, maxRes := 0, 0
			for i := range dual.Pix {
				if b := absInt(int(enc.Pix[i]) - int(dual.Pix[i])); b > maxBias {
					maxBias = b
				}
				if dr := int(comp.Pix[i]) - int(dual.Pix[i]); absInt(dr) > maxRes {
					maxRes = absInt(dr)
				}
			}
			fr := FrameResult{
				Name:           fs.Name,
				W:              fs.W,
				H:              fs.H,
				OutW:           fs.W / d,
				OutH:           fs.H / d,
				MaxEncodedBias: maxBias,
				MaxAbsResidual: maxRes,
			}
			row.Frames = append(row.Frames, fr)
			if float64(maxRes) > tolerance {
				allPass = false
			}
		}
		row.Pass = allPass
		rows = append(rows, row)
	}
	return rows, nil
}

// edgeComb 让每个 d×d 盒内恰有一列为同一暗电平 C，其余为 0。
// C 按块遍历 0..255，从而覆盖全部 8-bit 暗电平的量化边界。
func edgeComb(w, h, d int) Image {
	img := Image{W: w, H: h, Pix: make([]byte, w*h)}
	blocksW, blocksH := w/d, h/d
	for by := 0; by < blocksH; by++ {
		for bx := 0; bx < blocksW; bx++ {
			c := byte((bx*37 + by*17) % 256)
			x := bx * d
			for y := by * d; y < (by+1)*d; y++ {
				img.Pix[y*w+x] = c
			}
		}
	}
	return img
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// MonotonicCompensation 仅用于测试/文档：检查“偏亮”符号约定下 K 随 gamma 单调递减。
func MonotonicCompensation(rho float64, gammas []float64) bool {
	if len(gammas) < 2 {
		return true
	}
	prev := EdgeCompensation(rho, gammas[0])
	for _, g := range gammas[1:] {
		next := EdgeCompensation(rho, g)
		if next >= prev {
			return false
		}
		prev = next
	}
	return true
}
