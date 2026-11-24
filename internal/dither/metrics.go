package dither

import "math"

// Metrics 汇总一个阈值矩阵的全部评估量，供测试与 CLI 报告使用。
type Metrics struct {
	LowFreqLeak float64 // 值表面 DFT：r<=2 的 12 个非 DC 系数 |F|² 之和
	LowFreqWide float64 // 稳健性对照：r<=sqrt5 的 20 个非 DC 系数之和
	TotalPower  float64 // 全部 64 个系数（含 DC 项为 0）的功率和，Parseval 校验用
	BinaryLF    float64 // 阈值级 1..63 二值图案 r<=2 非 DC 功率的均值
	PAR         float64 // 二值图案谱的峰均比（1..63 级均值），规则纹锐度
	Anisotropy  float64 // 轴对齐方向能量占比（1..63 级均值），方向性纹路度量
	MSE         float64 // 测试图集上的均方量化误差
	MAE         float64 // 测试图集上的平均绝对量化误差
}

// 评估用测试图的边长与灰场灰度级。
const (
	testSize  = 256
	grayLevel = 128
)

// wideLeakage 用 fftshift 坐标系下 r²<=5 的系数集合（对照指标，20 个非 DC）。
func wideLeakage(p [Size * Size]float64) float64 {
	c := Size / 2
	sum := 0.0
	for v := 0; v < Size; v++ {
		dv := (v+c)%Size - c
		for u := 0; u < Size; u++ {
			du := (u+c)%Size - c
			d2 := du*du + dv*dv
			if d2 <= 5 && d2 > 0 {
				sum += p[v*Size+u]
			}
		}
	}
	return sum
}

// binarySpectrum 计算阈值级 level 的二值图案功率谱（已去均值）。
func binarySpectrum(m [Size * Size]int, level int) [Size * Size]float64 {
	var b [Size * Size]int
	for i, v := range m {
		if v < level {
			b[i] = 1
		}
	}
	return powerSpectrum(b)
}

// Evaluate 计算矩阵的全部指标。quantSuite 由 testImages 生成，
// 与分析原型 analysis/dither_prototype.py 的 test_suite 同源。
func Evaluate(m [Size * Size]int, suite [][]uint8, w, h int) Metrics {
	p := powerSpectrum(m)
	mt := Metrics{
		LowFreqLeak: lowFreqLeakage(p),
		LowFreqWide: wideLeakage(p),
		TotalPower:  sumPower(p),
	}

	var parSum, anisSum, lfSum float64
	levels := Size*Size - 1 // 1..63
	for level := 1; level <= levels; level++ {
		bp := binarySpectrum(m, level)
		// 直流在未平移坐标 [0,0]；PAR 在其余 63 个系数上取峰均比。
		var maxP, total float64
		for i, val := range bp {
			if i == 0 {
				continue
			}
			total += val
			if val > maxP {
				maxP = val
			}
		}
		parSum += maxP / (total / float64(Size*Size-1))
		// 各向异性：fftshift 坐标系下过中心 (4,4) 的行与列，
		// 即未平移坐标的第 0 行与第 0 列。
		var axial float64
		for k := 0; k < Size; k++ {
			axial += bp[k] + bp[k*Size]
		}
		anisSum += axial / total
		lfSum += lowFreqLeakage(bp)
	}
	mt.PAR = parSum / float64(levels)
	mt.Anisotropy = anisSum / float64(levels)
	mt.BinaryLF = lfSum / float64(levels)

	var se, ae float64
	n := 0
	for _, img := range suite {
		out, err := Dither(w, h, img, m)
		if err != nil {
			continue
		}
		for i, v := range img {
			e := float64(out[i]) - float64(v)
			se += e * e
			ae += math.Abs(e)
			n++
		}
	}
	mt.MSE = se / float64(n)
	mt.MAE = ae / float64(n)
	return mt
}

func sumPower(p [Size * Size]float64) float64 {
	s := 0.0
	for _, v := range p {
		s += v
	}
	return s
}
