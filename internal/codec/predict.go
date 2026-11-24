package codec

import (
	"encoding/binary"
	"errors"
	"math"
)

// MaxPredictOrder 是预测阶数的公共上限。
//
// 候选集 = {不预测} ∪ {水平 kx 阶 + 竖直 ky 阶 | kx+ky <= 上限}。
// 上限取 4：总阶数是二维多项式外推的度数，1..4 覆盖常量、平面、
// 二次、三次、四次曲面；再往上需要更长的因果邻域，对 8 bit 量化
// 噪声越来越敏感，收益递减。
const MaxPredictOrder = 4

// GTR2 头：magic(4) + 宽(2) + 高(2) + 水平阶(1) + 竖直阶(1)。
const predictHeaderSize = 10

// Candidate 是一个预测候选：水平方向阶数 KX 与竖直方向阶数 KY。
// KX+KY == 0 表示不预测，此时残差等于原像素。
type Candidate struct {
	KX, KY int
}

// String 返回候选的短名：none 或 x{kx}/y{ky}。
func (c Candidate) String() string {
	if c.KX == 0 && c.KY == 0 {
		return "none"
	}
	return "x" + itoa(c.KX) + "/y" + itoa(c.KY)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// OrderStat 是某一个候选的统计结果。
// Hist 是残差（uint8，模 256）在 0..255 上的计数；
// Entropy 是按该直方图算出的经验一阶熵，单位 bit/符号。
type OrderStat struct {
	Candidate Candidate
	Samples   int
	Hist      [256]int
	Entropy   float64
}

// PredictInfo 是一次预测编码的选阶依据。
type PredictInfo struct {
	Selected      Candidate // 按残差一阶熵选中的候选；零值表示不预测
	Emitted       Candidate // 实际写进流的候选；零值表示落回 GTR1
	FallbackToGTR bool      // 选中候选未缩小体积，落回 GTR1
	Samples       int
	Stats         []OrderStat
	BaselineBytes int // 同一图像关闭预测时的 GTR1 字节数
}

// binom 返回 (1 - z)^k 的系数，即 (-1)^i * C(k,i)，i=0..k。
// 对像素序列做 k 阶差分时，x[i] 的系数就是 binom(k)[0]。
func binom(k int) []int {
	out := make([]int, k+1)
	c := 1
	for i := 0; i <= k; i++ {
		v := c
		if i%2 == 1 {
			v = -v
		}
		out[i] = v
		if i < k {
			c = c * (k - i) / (i + 1)
		}
	}
	return out
}

// coef 是候选 (kx,ky) 的二维混合差分系数，行是竖直偏移 i、列是
// 水平偏移 j：coef[i][j] = ay[i]*ax[j]，作用在 (x-j, y-i) 上；
// coef[0][0] 恒为 1。
type coef [MaxPredictOrder + 1][MaxPredictOrder + 1]int

func mixedCoef(kx, ky int) coef {
	var c coef
	ax, ay := binom(kx), binom(ky)
	for i := 0; i <= ky; i++ {
		for j := 0; j <= kx; j++ {
			c[i][j] = ay[i] * ax[j]
		}
	}
	return c
}

// coefTable 预先算好所有 (kx,ky) 的系数，含边界降阶后的组合，
// 避免逐像素重算。下标越界只会发生在 0..MaxPredictOrder 之外，
// 由调用方先做范围检查。
var coefTable = func() (t [MaxPredictOrder + 1][MaxPredictOrder + 1]coef) {
	for kx := 0; kx <= MaxPredictOrder; kx++ {
		for ky := 0; ky <= MaxPredictOrder; ky++ {
			t[kx][ky] = mixedCoef(kx, ky)
		}
	}
	return t
}()

// predictAt 返回 (x,y) 处候选 (kx,ky) 的预测值（模 256）。
//
// 预测器是二维混合差分外推：令 Δx = 1 - L、Δy = 1 - U，
// 取 Δx^kx Δy^ky 的残差为零，解出当前像素：
//
//	p = -Σ_{(i,j)≠(0,0)} coef[i][j] * pix[x-j, y-i]
//
// 例如 (1,0) 即左像素；(0,1) 即上像素；(1,1) 为平面外推
// p = L + U - LU，对任意方向的线性渐变都精确。
//
// 边界处把可用阶数降为 min(kx,x) 与 min(ky,y)，即改用对应降阶
// 候选自己的系数（不是简单截断高阶系数），只用确实存在的因果邻域；
// 降到 0/0 时预测 0、残差等于原值。预测值与残差都按 mod 256 计算，
// 解码端相加即可无损还原。
func predictAt(pix []uint8, w, x, y, kx, ky int) uint8 {
	if x < kx {
		kx = x
	}
	if y < ky {
		ky = y
	}
	if kx == 0 && ky == 0 {
		return 0
	}
	c := &coefTable[kx][ky]
	sum := 0
	for i := 0; i <= ky; i++ {
		base := (y - i) * w
		for j := 0; j <= kx; j++ {
			if i == 0 && j == 0 {
				continue
			}
			sum -= c[i][j] * int(pix[base+x-j])
		}
	}
	return uint8(sum)
}

// Residuals 返回整幅图在给定候选下的残差序列（行优先）。
// 零候选（KX==0 && KY==0）表示不预测，残差即原像素。
func Residuals(pix []uint8, w int, cand Candidate) []uint8 {
	res := make([]uint8, len(pix))
	h := len(pix) / w
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			res[i] = uint8(int(pix[i]) - int(predictAt(pix, w, x, y, cand.KX, cand.KY)))
		}
	}
	return res
}

// FirstOrderEntropy 按直方图计算经验一阶熵，单位 bit/符号：
//
//	H = -Σ_{v=0}^{255} p_v * log2(p_v),  p_v = count_v / N
//
// 只统计单个残差符号的分布（零记忆），不引入相邻残差的条件项；
// count 为 0 的项按 0 计（0*log2(0) 定义为 0）。
// 统计区间是整幅图的 W*H 个残差（行优先连续扫描），边界像素同样
// 参与统计，不排除任何预热样本。
func FirstOrderEntropy(hist [256]int, n int) float64 {
	if n <= 0 {
		return 0
	}
	h := 0.0
	for _, c := range hist {
		if c == 0 {
			continue
		}
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

// candidates 生成候选集：总阶数 s = 1..maxOrder 的所有 (kx,ky)，
// 外加不预测；同一总阶数内按 KX 升序。顺序固定，便于熵相同时
// 取靠前者。
func candidates(maxOrder int) []Candidate {
	out := []Candidate{{}}
	for s := 1; s <= maxOrder; s++ {
		for kx := 0; kx <= s; kx++ {
			out = append(out, Candidate{KX: kx, KY: s - kx})
		}
	}
	return out
}

// residualStat 把候选的残差写进调用方给的缓冲区并统计直方图。
// 复用缓冲区可让候选集遍历只分配一次残差内存。
func residualStat(pix []uint8, w int, c Candidate, buf []uint8) OrderStat {
	h := len(pix) / w
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			buf[i] = uint8(int(pix[i]) - int(predictAt(pix, w, x, y, c.KX, c.KY)))
		}
	}
	var hist [256]int
	for _, v := range buf[:len(pix)] {
		hist[v]++
	}
	return OrderStat{
		Candidate: c,
		Samples:   len(pix),
		Hist:      hist,
		Entropy:   FirstOrderEntropy(hist, len(pix)),
	}
}

// SelectPredict 在候选集里按残差一阶熵最小选一个。每个候选各统计
// 一次；熵相同时取候选集中靠前者（总阶数低者优先，其次 KX 小者）。
func SelectPredict(pix []uint8, w, maxOrder int) (Candidate, []OrderStat) {
	cands := candidates(maxOrder)
	stats := make([]OrderStat, len(cands))
	buf := make([]uint8, len(pix))
	best := 0
	for i, c := range cands {
		stats[i] = residualStat(pix, w, c, buf)
		if stats[i].Entropy < stats[best].Entropy {
			best = i
		}
	}
	return cands[best], stats
}

// 残差游程流（PackBits 语义 + 扩展段），紧跟在 GTR2 头的阶数字节之后：
//
//	t = 0..127   : 字面量段，后随 t+1 个残差原字节（1..128 个）
//	t = 129..255 : 短重复段，后随 1 个残差字节，重复 257-t 次（2..128 次）
//	t = 128      : 扩展重复段，后随 uint16 小端计数（2..65535）与 1 个
//	               残差字节；更长的重复拆成多段
//
// 解码端按 W*H 个残差停，不依赖文件尾标记。扩展段让整幅常值残差
// 只需 6 字节，且不改变 128 以内短段的开销。
const packLiteralMax = 128
const packRepeatMax = 128
const packExtend = 128
const packExtendMax = 0xFFFF

func appendRuns(out []byte, res []uint8) []byte {
	i := 0
	for i < len(res) {
		run := 1
		for i+run < len(res) && res[i+run] == res[i] && run < packExtendMax {
			run++
		}
		switch {
		case run > packRepeatMax:
			var cnt [2]byte
			binary.LittleEndian.PutUint16(cnt[:], uint16(run))
			out = append(out, packExtend, cnt[0], cnt[1], res[i])
			i += run
			continue
		case run >= 2:
			out = append(out, byte(257-run), res[i])
			i += run
			continue
		}
		// 单字节：收集字面量段，遇到长度 >=2 的相同段或达到 128 就停。
		start := i
		i++
		for i < len(res) && i-start < packLiteralMax {
			n := 1
			for i+n < len(res) && res[i+n] == res[i] && n < packRepeatMax {
				n++
			}
			if n >= 2 {
				break
			}
			i++
		}
		out = append(out, byte(i-start-1))
		out = append(out, res[start:i]...)
	}
	return out
}

// readRuns 还原残差序列，恰好读取 n 个残差为止。
func readRuns(data []byte, off, n int) ([]uint8, error) {
	res := make([]uint8, 0, n)
	for len(res) < n {
		if off >= len(data) {
			return nil, errors.New("GTR2 游程被截断")
		}
		t := int(data[off])
		off++
		switch {
		case t <= 127: // 字面量
			cnt := t + 1
			if off+cnt > len(data) {
				return nil, errors.New("GTR2 字面量段被截断")
			}
			if len(res)+cnt > n {
				return nil, errors.New("游程超出画幅")
			}
			res = append(res, data[off:off+cnt]...)
			off += cnt
		case t >= 129: // 短重复
			cnt := 257 - t
			if off >= len(data) {
				return nil, errors.New("GTR2 重复段被截断")
			}
			v := data[off]
			off++
			if len(res)+cnt > n {
				return nil, errors.New("游程超出画幅")
			}
			for k := 0; k < cnt; k++ {
				res = append(res, v)
			}
		default: // 128 扩展重复
			if off+3 > len(data) {
				return nil, errors.New("GTR2 扩展段被截断")
			}
			cnt := int(binary.LittleEndian.Uint16(data[off:]))
			v := data[off+2]
			off += 3
			if cnt < packRepeatMax+1 {
				return nil, errors.New("GTR2 扩展段计数过小")
			}
			if len(res)+cnt > n {
				return nil, errors.New("游程超出画幅")
			}
			for k := 0; k < cnt; k++ {
				res = append(res, v)
			}
		}
	}
	return res, nil
}

// EncodeRLEPredict 先按残差一阶熵选候选并做预测，再对残差做游程
// 编码。maxOrder 取值 [1, MaxPredictOrder]。
//
// 选阶严格按残差一阶熵最小（见 SelectPredict）。熵最小不保证字节
// 最少：残差符号分布还决定游程长度，所以若选中候选的码流反而大于
// 关闭预测的 GTR1 基线，就落回与基线逐字节相同的 GTR1，保证启用
// 预测不会让体积变差。落回时 info.FallbackToGTR 置位，熵选结果仍
// 完整记录在 info 里。
func EncodeRLEPredict(w, h int, pix []uint8, maxOrder int) ([]byte, *PredictInfo, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, nil, errors.New("画幅或像素数不匹配")
	}
	if maxOrder < 1 || maxOrder > MaxPredictOrder {
		return nil, nil, errors.New("预测阶数超出范围")
	}
	baseline, err := EncodeRLE(w, h, pix)
	if err != nil {
		return nil, nil, err
	}
	selected, stats := SelectPredict(pix, w, maxOrder)
	info := &PredictInfo{
		Selected:      selected,
		Samples:       len(pix),
		Stats:         stats,
		BaselineBytes: len(baseline),
	}
	if selected.KX == 0 && selected.KY == 0 {
		return baseline, info, nil
	}

	res := Residuals(pix, w, selected)
	out := make([]byte, predictHeaderSize, predictHeaderSize+len(res))
	copy(out[0:], "GTR2")
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	out[8] = byte(selected.KX)
	out[9] = byte(selected.KY)
	out = appendRuns(out, res)
	if len(out) >= len(baseline) {
		info.FallbackToGTR = true
		return baseline, info, nil
	}
	info.Emitted = selected
	return out, info, nil
}

// decodeRLEv2 解析 GTR2：游程还原残差后按二维候选做逆预测。
// maxOrder<=0 表示不限制；否则流内总阶数不得超过它。
func decodeRLEv2(data []byte, maxOrder int) (int, int, []uint8, error) {
	if len(data) < predictHeaderSize {
		return 0, 0, nil, errors.New("GTR2 流被截断")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	kx, ky := int(data[8]), int(data[9])
	if kx < 0 || ky < 0 || kx > MaxPredictOrder || ky > MaxPredictOrder || kx+ky == 0 {
		return 0, 0, nil, errors.New("流内预测阶数非法")
	}
	if maxOrder > 0 && kx+ky > maxOrder {
		return 0, 0, nil, errors.New("流内预测阶数超过上限")
	}
	n := w * h
	res, err := readRuns(data, predictHeaderSize, n)
	if err != nil {
		return 0, 0, nil, err
	}
	pix := make([]uint8, n)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			pix[i] = predictAt(pix, w, x, y, kx, ky) + res[i]
		}
	}
	return w, h, pix, nil
}

// DecodeRLEPredict 解析 GTR2 预测残差游程流。
// maxOrder<=0 表示不限制。
func DecodeRLEPredict(data []byte, maxOrder int) (int, int, []uint8, error) {
	if len(data) < 4 || string(data[0:4]) != "GTR2" {
		return 0, 0, nil, errors.New("不是 GTR2 流")
	}
	return decodeRLEv2(data, maxOrder)
}

// StreamPredict 返回 GTR2 流内记录的水平阶与竖直阶；非 GTR2 返回 0,0。
func StreamPredict(data []byte) (int, int) {
	if len(data) < predictHeaderSize || string(data[0:4]) != "GTR2" {
		return 0, 0
	}
	return int(data[8]), int(data[9])
}
