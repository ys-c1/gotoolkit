// Package rd 实现率失真编码三件事：
//  1. 量化步长搜索：先按目标每像素比特反解初始步长，再在 [1,255] 全量搜索最接近目标体积的步长；
//  2. 预测残差编码：在预测域（左邻 DPCM）上计算残差，对残差做均匀量化后重建；
//  3. 目标体积约束：以真实编码字节数对账，目标不可达时返回最接近的步长与差距。
package rd

import (
	"math"

	"gotoolkit/internal/codec"
)

// 合法量化步长区间。步长 0 没有意义（除零），由上层报 ErrZeroStep。
const (
	MinStep = 1
	MaxStep = 255
)

// EncodeFunc 是某种格式的真实编码器，用来对量化重建后的图计真实字节数。
type EncodeFunc func(w, h int, pix []uint8) ([]byte, error)

// Encoders 返回三种格式对应的真实编码器，保证体积对账用的是最终落地的字节。
func Encoders(format string) (EncodeFunc, bool) {
	switch format {
	case "pgm":
		return codec.EncodePGM, true
	case "bmp":
		return codec.EncodeBMP, true
	case "rle":
		return codec.EncodeRLE, true
	default:
		return nil, false
	}
}

// predictAt 返回 (x,y) 的预测值：行首预测 0，其余用左邻重建值。
// 预测在重建域上做，避免编解码漂移。
func predictAt(x, y, w int, recon []uint8) int {
	if x == 0 {
		return 0
	}
	return int(recon[y*w+x-1])
}

// quantize 对单个残差做均匀量化（四舍五入）。
func quantize(res, step int) int {
	if res >= 0 {
		return (res + step/2) / step
	}

	return -((-res + step/2) / step)
}

// Residuals 在预测域上计算全部残差（原始像素 − 左邻预测，行首预测为 0）。
func Residuals(w, h int, pix []uint8) []int {
	res := make([]int, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x
			res[idx] = int(pix[idx]) - predictAt(x, y, w, pix)
		}
	}
	return res
}

// Reconstruct 以指定步长在预测域量化残差并重建像素，返回重建图与量化索引。
// 重建值裁剪到 [0,255]，量化索引供残差编码侧使用。
func Reconstruct(w, h int, pix []uint8, step int) (recon []uint8, levels []int) {
	n := w * h
	recon = make([]uint8, n)
	levels = make([]int, n)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x
			pred := predictAt(x, y, w, recon)
			res := int(pix[idx]) - pred
			q := quantize(res, step)
			levels[idx] = q
			v := pred + q*step
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			recon[idx] = uint8(v)
		}
	}
	return recon, levels
}

// EstimateStep 按目标每像素比特反解初始量化步长（高率量化模型 Q ≈ sqrt(12·σ²)·2^(−bpp/2)）。
// 残差方差为 0（平坦图）时给最小步长。结果裁剪到 [1,255]，只作为搜索起点。
func EstimateStep(w, h int, pix []uint8, targetBPP float64) int {
	return clampStep(InverseStep(w, h, pix, targetBPP))
}

// InverseStep 是不裁剪的步长反解：返回 0 表示目标高到最细步长都够不着（上层报 ErrZeroStep），
// 返回 >255 表示目标低到只能用最粗步长（搜索会自然落到 255）。
// 平坦图（残差方差为 0）天然最小体积，反解给最小步长。
func InverseStep(w, h int, pix []uint8, targetBPP float64) int {
	res := Residuals(w, h, pix)
	var sum, sumSq float64
	for _, r := range res {
		sum += float64(r)
		sumSq += float64(r) * float64(r)
	}
	n := float64(len(res))
	variance := sumSq/n - (sum/n)*(sum/n)
	if variance < 0 {
		variance = 0
	}
	if variance == 0 {
		return MinStep
	}
	q := math.Sqrt(12.0*variance) * math.Pow(2.0, -targetBPP/2.0)
	return int(math.Round(q))
}

func clampStep(step int) int {
	if step < MinStep {
		return MinStep
	}
	if step > MaxStep {
		return MaxStep
	}
	return step
}

// Candidate 是某个步长的真实编码结果。
type Candidate struct {
	Step  int
	Bytes int
	Bits  int
	BPP   float64
	Recon []uint8
	Body  []byte
}

// measure 用真实编码器对某步长的重建图计字节数。
func measure(w, h int, pix []uint8, step int, enc EncodeFunc) (Candidate, error) {
	recon, _ := Reconstruct(w, h, pix, step)
	body, err := enc(w, h, recon)
	if err != nil {
		return Candidate{}, err
	}
	n := w * h
	return Candidate{
		Step:  step,
		Bytes: len(body),
		Bits:  len(body) * 8,
		BPP:   float64(len(body)*8) / float64(n),
		Recon: recon,
		Body:  body,
	}, nil
}

// SearchResult 是步长搜索的对账结果。
type SearchResult struct {
	Best        Candidate // 最接近目标的步长
	TargetBits  int       // 目标比特数 = round(bpp × 像素数)
	TargetBPP   float64
	Reachable   bool    // 是否存在步长使实际比特恰好等于目标比特
	GapBits     int     // 实际与目标的比特差绝对值
	GapBPP      float64 // 实际与目标的 bpp 差绝对值
	GapRatio    float64 // 差距占目标的比例
	InitialStep int     // 反解出的初始步长
	Recon       []uint8 // 最接近步长下的重建图
	Body        []byte  // 最接近步长下的真实编码字节
}

// Search 在 [1,255] 上逐档量化、用真实编码器计字节，选与目标比特差绝对值最小的步长。
// 差距相同取更小步长（失真更小）。任何一档恰好命中即标记 Reachable。
func Search(w, h int, pix []uint8, targetBPP float64, enc EncodeFunc) (*SearchResult, error) {
	n := w * h
	targetBits := int(math.Round(targetBPP * float64(n)))
	initial := EstimateStep(w, h, pix, targetBPP)

	res := &SearchResult{
		TargetBits:  targetBits,
		TargetBPP:   targetBPP,
		InitialStep: initial,
	}
	bestGap := math.MaxInt
	for step := MinStep; step <= MaxStep; step++ {
		cand, err := measure(w, h, pix, step, enc)
		if err != nil {
			return nil, err
		}
		gap := cand.Bits - targetBits
		if gap < 0 {
			gap = -gap
		}
		if gap < bestGap {
			bestGap = gap
			res.Best = cand
		}
		if gap == 0 {
			// 升序扫描，首个命中就是步长最小（失真最小）的一档；差距不可能更小，提前结束。
			res.Reachable = true
			break
		}
	}
	res.Recon = res.Best.Recon
	res.Body = res.Best.Body
	res.GapBits = res.Best.Bits - targetBits
	if res.GapBits < 0 {
		res.GapBits = -res.GapBits
	}
	res.GapBPP = math.Abs(res.Best.BPP - targetBPP)
	if targetBits > 0 {
		res.GapRatio = float64(res.GapBits) / float64(targetBits)
	} else {
		res.GapRatio = math.Abs(res.Best.BPP - targetBPP)
	}
	return res, nil
}
