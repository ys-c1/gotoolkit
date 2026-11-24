package subband

import "math"

// ArtifactStats 是两类失真的分开统计。
//
// 占比有三套口径，避免“只给总数”：
//  1. 能量占比：正交变换下按带族累加误差能量，LowShare+HighShare=1；
//  2. 异常像素主导归属：|误差|≥Tau 的像素按最大贡献带归一类，两类内合计 100%；
//  3. 形态检出率：块状接缝命中率、边缘邻域毛刺检出率，分母各自写明。
type ArtifactStats struct {
	Tau     float64
	Flagged int     // |总误差|≥Tau 的像素数
	N       int
	FlagPct float64 // Flagged/N

	DomLow      int // 异常像素中最大误差贡献来自低频族
	DomHigh     int
	DomLowPct   float64 // DomLow/(DomLow+DomHigh)
	DomHighPct  float64
	DomCoverPct float64 // (DomLow+DomHigh)/Flagged，恒为 1（保留字段）

	EnergyTotal     float64
	EnergyLow       float64
	EnergyHigh      float64
	EnergyLowShare  float64
	EnergyHighShare float64

	// 低频族块状/条纹形态量（在低频族误差图上测量）。
	BlockGrid         int
	SeamPositions     int
	SeamHits          int
	SeamHitPct        float64 // SeamHits/SeamPositions
	SeamStepRMS       float64 // 跨 8 像素块缝的误差阶跃 RMS
	InteriorStepRMS   float64 // 非块缝相邻点误差阶跃 RMS
	SeamExcessMSE     float64 // 块缝阶跃平方均值 − 内部阶跃平方均值
	SeamHitThreshold  float64

	// 高频族边缘毛刺形态量（在高频族误差图上测量）。
	EdgeThreshold   float64
	EdgePixels      int // Sobel 强边缘像素
	EdgeBandPixels  int // 膨胀 1 像素后的边缘邻域像素
	BurrHits        int
	BurrPrevalence  float64 // BurrHits/EdgeBandPixels
	BurrScoreEdge   float64 // 边缘邻域拉普拉斯能量均值
	BurrScoreOutside float64
	BurrLapThreshold float64
}

// IsLowFamily 定义两类的带族归属：
// 最粗 LL 与第 2、3 级（粗/中频，空间尺度 ≥4px）归“低频块状条纹”类；
// 第 1 级 LH/HL/HH（2px 细节）归“高频边缘毛刺”类。
func IsLowFamily(b Band, levels int) bool {
	_ = levels
	return b.Kind == "LL" || b.Level >= 2
}

// SobelMag 计算灰度图的 Sobel 梯度幅值（平坦切片）。
func SobelMag(pix []uint8, w, h int) []float64 {
	mag := make([]float64, w*h)
	at := func(x, y int) float64 { return float64(pix[y*w+x]) }
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			gx := -at(x-1, y-1) + at(x+1, y-1) -
				2*at(x-1, y) + 2*at(x+1, y) -
				at(x-1, y+1) + at(x+1, y+1)
			gy := -at(x-1, y-1) - 2*at(x, y-1) - at(x+1, y-1) +
				at(x-1, y+1) + 2*at(x, y+1) + at(x+1, y+1)
			mag[y*w+x] = math.Hypot(gx, gy)
		}
	}
	return mag
}

// AnalyzeArtifacts 由各带像素域误差图汇总两类失真统计。
// bandErr[i] 为第 i 带量化误差逆变换到像素域的结果。
func AnalyzeArtifacts(src []uint8, w, h, levels int, bands []Band, bandErr [][]float64, tau float64) ArtifactStats {
	n := w * h
	a := ArtifactStats{Tau: tau, N: n, BlockGrid: 1 << uint(levels)}

	lowImg := make([]float64, n)
	highImg := make([]float64, n)
	var eLow, eHigh, eTot float64
	for i, e := range bandErr {
		low := IsLowFamily(bands[i], levels)
		var se float64
		for p, v := range e {
			se += v * v
			eTot += v * v
			if low {
				eLow += v * v
				lowImg[p] += v
			} else {
				eHigh += v * v
				highImg[p] += v
			}
		}
		_ = se
	}
	a.EnergyTotal = eTot
	a.EnergyLow = eLow
	a.EnergyHigh = eHigh
	if eLow+eHigh > 0 {
		a.EnergyLowShare = eLow / (eLow + eHigh)
		a.EnergyHighShare = eHigh / (eLow + eHigh)
	}

	// 像素级主导带归属。
	for p := 0; p < n; p++ {
		sum := 0.0
		best := -1
		bestV := -1.0
		for i, e := range bandErr {
			v := e[p] * e[p]
			sum += e[p] // 总误差带符号相加
			if v > bestV {
				bestV = v
				best = i
			}
		}
		if math.Abs(sum) >= tau {
			a.Flagged++
			if best >= 0 && IsLowFamily(bands[best], levels) {
				a.DomLow++
			} else {
				a.DomHigh++
			}
		}
	}
	a.FlagPct = 100 * float64(a.Flagged) / float64(n)
	if a.DomLow+a.DomHigh > 0 {
		a.DomLowPct = 100 * float64(a.DomLow) / float64(a.DomLow+a.DomHigh)
		a.DomHighPct = 100 * float64(a.DomHigh) / float64(a.DomLow+a.DomHigh)
		a.DomCoverPct = 100
	}

	// ---- 块状/条纹：在低频族误差图上比较块缝与内部的相邻阶跃 ----
	g := a.BlockGrid
	a.SeamHitThreshold = 10.0
	var seamSum, interiorSum float64
	var seamCnt, intCnt, hits int
	isVSeam := func(x int) bool { // 相邻对 (x,x+1) 是否跨竖块缝
		return (x+1)%g == 0
	}
	isHSeam := func(y int) bool {
		return (y+1)%g == 0
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w-1; x++ {
			d := lowImg[y*w+x] - lowImg[y*w+x+1]
			if isVSeam(x) {
				seamSum += d * d
				seamCnt++
				if math.Abs(d) >= a.SeamHitThreshold {
					hits++
				}
			} else {
				interiorSum += d * d
				intCnt++
			}
		}
	}
	for y := 0; y < h-1; y++ {
		for x := 0; x < w; x++ {
			d := lowImg[y*w+x] - lowImg[(y+1)*w+x]
			if isHSeam(y) {
				seamSum += d * d
				seamCnt++
				if math.Abs(d) >= a.SeamHitThreshold {
					hits++
				}
			} else {
				interiorSum += d * d
				intCnt++
			}
		}
	}
	a.SeamPositions = seamCnt
	a.SeamHits = hits
	if seamCnt > 0 {
		m := seamSum / float64(seamCnt)
		a.SeamStepRMS = math.Sqrt(m)
	}
	if intCnt > 0 {
		m := interiorSum / float64(intCnt)
		a.InteriorStepRMS = math.Sqrt(m)
		a.SeamExcessMSE = seamSum/float64(seamCnt) - m
	}
	if seamCnt > 0 {
		a.SeamHitPct = 100 * float64(hits) / float64(seamCnt)
	}

	// ---- 边缘毛刺：Sobel 强边缘膨胀邻域内，高频误差的拉普拉斯能量/尖峰 ----
	a.EdgeThreshold = 100
	a.BurrLapThreshold = 15
	mag := SobelMag(src, w, h)
	edge := make([]bool, n)
	for p, v := range mag {
		if v >= a.EdgeThreshold {
			edge[p] = true
			a.EdgePixels++
		}
	}
	inBand := make([]bool, n)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			if !edge[p] {
				continue
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					xx, yy := x+dx, y+dy
					if xx >= 0 && xx < w && yy >= 0 && yy < h {
						inBand[yy*w+xx] = true
					}
				}
			}
		}
	}
	var edgeSum, outSum float64
	var edgeCnt, outCnt, burr int
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			p := y*w + x
			l := 4*highImg[p] - highImg[p-1] - highImg[p+1] -
				highImg[p-w] - highImg[p+w]
			l2 := l * l
			if inBand[p] {
				edgeSum += l2
				edgeCnt++
				if math.Abs(l) >= a.BurrLapThreshold {
					burr++
				}
			} else {
				outSum += l2
				outCnt++
			}
		}
	}
	a.EdgeBandPixels = edgeCnt
	a.BurrHits = burr
	if edgeCnt > 0 {
		a.BurrPrevalence = 100 * float64(burr) / float64(edgeCnt)
		a.BurrScoreEdge = edgeSum / float64(edgeCnt)
	}
	if outCnt > 0 {
		a.BurrScoreOutside = outSum / float64(outCnt)
	}
	return a
}
