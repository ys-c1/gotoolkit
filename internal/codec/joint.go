package codec

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// 本文件实现「预测器 × 块长」联合代价实验所用的 GTR3（jnt）格式与
// 熵/代价统计。它与 GTR1 并存：当预测器为 none 且 BlockLen==0（整幅
// 一块、不切块）时，EncodeJoint 直接返回 EncodeRLE 的 GTR1 字节，
// 保证当前组合的产物与现在逐字节一致。
//
// 其余组合输出 GTR3：残差按块切分，每块独立做一次 DEFLATE（flate，
// Huffman+LZ 的通用熵编码）作为「按残差熵压缩」的可测实现；块前是
// BlockHeaderSize 字节定长块头（BK 标记 + uint32 压缩负载长度）。
// 联合代价模型只计香农熵负载与定长块头；DEFLATE 自身的动态码表/流
// 复位开销属于编码器实现开销，不计入模型，但会如实进入实测体积。

// JointMagic 是 GTR3 联合分块流的文件头。
const JointMagic = "GTR3"

// JointStreamHeaderSize：magic(4)+宽(2)+高(2)+预测器编号(1)+块长(4)。
const JointStreamHeaderSize = 13

// BlockMagic 是每个残差块的块头标记。
const BlockMagic = "BK"

// BlockHeaderSize 是固定块头长度：BlockMagic(2)+压缩负载字节数 uint32(4)。
// 联合代价里的「块头开销」就取这个常数：h = 8*BlockHeaderSize bit。
const BlockHeaderSize = 6

// 预测器候选名。本题只取两种：
//   - none：零阶/不预测，预测值恒为 0，残差即原像素（当前 GTR1 行为）；
//   - left：左像素预测，p_hat(x,y)=p(x-1,y)，行首 x=0 处预测 0。
const (
	PredictNone = "none"
	PredictLeft = "left"
)

// PredictorID 把预测器名映射进流头字节。
func PredictorID(name string) (byte, error) {
	switch name {
	case "", PredictNone:
		return 0, nil
	case PredictLeft:
		return 1, nil
	default:
		return 0, errors.New("未知预测器: " + name)
	}
}

// NormalizePredictor 把空串归一为 none，并校验取值。
func NormalizePredictor(name string) (string, error) {
	switch name {
	case "", PredictNone:
		return PredictNone, nil
	case PredictLeft:
		return PredictLeft, nil
	default:
		return "", errors.New("预测器只能取 none|left: " + name)
	}
}

func predictorName(id byte) (string, error) {
	switch id {
	case 0:
		return PredictNone, nil
	case 1:
		return PredictLeft, nil
	default:
		return "", errors.New("流内预测器编号非法")
	}
}

// JointBlockHeaderBits 是每个块的固定头比特数。
const JointBlockHeaderBits = 8 * BlockHeaderSize

// ResidualsJoint 按指定预测器生成行优先残差序列，模 256 计算，
// 解码端相加即可无损还原。所有 W*H 个样本都参与（含行首边界样本，
// 边界处预测值取 0，不做预热剔除）。
func ResidualsJoint(pix []uint8, w int, predictor string) ([]uint8, error) {
	name, err := NormalizePredictor(predictor)
	if err != nil {
		return nil, err
	}
	n := len(pix)
	r := make([]uint8, n)
	h := n / w
	switch name {
	case PredictNone:
		copy(r, pix)
	case PredictLeft:
		for y := 0; y < h; y++ {
			base := y * w
			r[base] = pix[base] // x=0：预测 0
			for x := 1; x < w; x++ {
				i := base + x
				r[i] = uint8(int(pix[i]) - int(pix[i-1]))
			}
		}
	}
	return r, nil
}

// FirstOrderEntropyJoint 按直方图计算经验一阶香农熵，单位 bit/符号：
//
//	H = -Σ_v p_v log2 p_v, p_v = count_v/n
//
// count 为 0 的项按 0 计。
func FirstOrderEntropyJoint(hist [256]int, n int) float64 {
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

// BlockStat 是一个残差块的熵与代价明细。
type BlockStat struct {
	Index       int
	Start       int
	N           int     // 块内残差符号数（末块可能小于 BlockLen）
	Entropy     float64 // 本块经验一阶熵 bit/符号
	EntropyBits float64 // N*Entropy
	HeaderBits  float64 // 固定块头开销
	CostBits    float64 // EntropyBits + HeaderBits
}

// JointStat 是某个 (预测器, 块长) 组合的联合代价统计。
type JointStat struct {
	Predict     string
	W, H, N     int
	BlockLen    int // 调用方给定值，0 表示整幅一块
	EffBlockLen int // 实际块长，0 已展开为 N
	Blocks      int // m = ceil(N/EffBlockLen)
	HGlobal     float64
	EntropyBits float64 // Σ_k n_k H_k
	HeaderBits  float64 // JointBlockHeaderBits * m
	CostBits    float64 // EntropyBits + HeaderBits
	CostBytes   float64 // CostBits / 8，未取整的理论字节
	ModelBytes  int     // ceil(CostBits/8)
	BlockStats  []BlockStat
}

// jointCost 对已生成的残差序列按块长分块统计。
func jointCost(r []uint8, w int, predictor string, requestedBlock int) (JointStat, error) {
	name, err := NormalizePredictor(predictor)
	if err != nil {
		return JointStat{}, err
	}
	n := len(r)
	if w <= 0 || n%w != 0 {
		return JointStat{}, errors.New("画幅或像素数不匹配")
	}
	b := requestedBlock
	if b == 0 || b > n {
		b = n
	}
	if b <= 0 {
		return JointStat{}, errors.New("块长非法")
	}
	m := (n + b - 1) / b

	var ghist [256]int
	for _, v := range r {
		ghist[v]++
	}
	gH := FirstOrderEntropyJoint(ghist, n)

	st := JointStat{
		Predict:     name,
		W:           w,
		H:           n / w,
		N:           n,
		BlockLen:    requestedBlock,
		EffBlockLen: b,
		Blocks:      m,
		HGlobal:     gH,
		HeaderBits:  float64(JointBlockHeaderBits) * float64(m),
		BlockStats:  make([]BlockStat, 0, m),
	}
	for k := 0; k < m; k++ {
		a := k * b
		z := a + b
		if z > n {
			z = n
		}
		var hist [256]int
		for _, v := range r[a:z] {
			hist[v]++
		}
		cn := z - a
		ent := FirstOrderEntropyJoint(hist, cn)
		eb := ent * float64(cn)
		bs := BlockStat{
			Index:       k,
			Start:       a,
			N:           cn,
			Entropy:     ent,
			EntropyBits: eb,
			HeaderBits:  float64(JointBlockHeaderBits),
			CostBits:    eb + float64(JointBlockHeaderBits),
		}
		st.BlockStats = append(st.BlockStats, bs)
		st.EntropyBits += eb
	}
	st.CostBits = st.EntropyBits + st.HeaderBits
	st.CostBytes = st.CostBits / 8.0
	st.ModelBytes = int(math.Ceil(st.CostBits / 8.0))
	return st, nil
}

// AnalyzeJoint 计算一个组合的残差熵与联合代价，不做编码。
// blockLen==0 表示整幅一块（当前档）。
func AnalyzeJoint(w, h int, pix []uint8, predictor string, blockLen int) (JointStat, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return JointStat{}, errors.New("画幅或像素数不匹配")
	}
	if blockLen < 0 {
		return JointStat{}, errors.New("块长不能为负")
	}
	r, err := ResidualsJoint(pix, w, predictor)
	if err != nil {
		return JointStat{}, err
	}
	return jointCost(r, w, predictor, blockLen)
}

// SolveJoint 在候选组合中取联合代价最小者。代价相同依次按：
// 块数更少（块长更长）、预测器名字典序靠前，保证结果确定。
func SolveJoint(stats []JointStat) (JointStat, int) {
	best := 0
	for i := 1; i < len(stats); i++ {
		a, b := stats[best], stats[i]
		switch {
		case b.CostBits < a.CostBits:
			best = i
		case b.CostBits == a.CostBits && b.Blocks < a.Blocks:
			best = i
		case b.CostBits == a.CostBits && b.Blocks == a.Blocks && b.Predict < a.Predict:
			best = i
		}
	}
	return stats[best], best
}

// flateCompress 对一块残差做一次独立 DEFLATE。
func flateCompress(block []uint8) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(block); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EncodeJoint 编码联合分块熵编码流。
//
// 当前组合（predictor 为 none 且 blockLen==0）逐字节走 GTR1 EncodeRLE，
// 因此与现有 testdata/golden/rle.bin 完全一致。其余组合输出 GTR3：
// 残差按块切分，每块独立 DEFLATE，块前写 6 字节定长块头。
func EncodeJoint(w, h int, pix []uint8, predictor string, blockLen int) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if w > 65535 || h > 65535 {
		return nil, errors.New("宽高超出 uint16 表达范围")
	}
	if blockLen < 0 || blockLen > 0xFFFFFFFF {
		return nil, errors.New("块长非法")
	}
	name, err := NormalizePredictor(predictor)
	if err != nil {
		return nil, err
	}
	// 当前组合：直接返回今日 GTR1 产物。
	if name == PredictNone && blockLen == 0 {
		return EncodeRLE(w, h, pix)
	}

	r, err := ResidualsJoint(pix, w, name)
	if err != nil {
		return nil, err
	}
	b := blockLen
	if b == 0 || b > len(r) {
		b = len(r)
	}
	pid, _ := PredictorID(name)

	out := make([]byte, 0, JointStreamHeaderSize+len(r))
	out = append(out, JointMagic...)
	var u16 [2]byte
	binary.LittleEndian.PutUint16(u16[:], uint16(w))
	out = append(out, u16[0], u16[1])
	binary.LittleEndian.PutUint16(u16[:], uint16(h))
	out = append(out, u16[0], u16[1])
	out = append(out, pid)
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], uint32(blockLen)) // 0 即整幅
	out = append(out, u32[:]...)

	for a := 0; a < len(r); a += b {
		z := a + b
		if z > len(r) {
			z = len(r)
		}
		cb, err := flateCompress(r[a:z])
		if err != nil {
			return nil, err
		}
		if len(cb) > 0xFFFFFFFF {
			return nil, errors.New("压缩块过长")
		}
		out = append(out, BlockMagic[0], BlockMagic[1])
		binary.LittleEndian.PutUint32(u32[:], uint32(len(cb)))
		out = append(out, u32[:]...)
		out = append(out, cb...)
	}
	return out, nil
}

// DecodeJoint 解析 jnt 格式：按文件头 magic 自动识别 GTR1（当前组合的
// 兼容产物）或 GTR3 分块熵编码流，无损还原像素。
func DecodeJoint(data []byte) (int, int, []uint8, error) {
	if len(data) < 4 {
		return 0, 0, nil, errors.New("jnt 数据被截断")
	}
	switch string(data[0:4]) {
	case "GTR1":
		return DecodeRLE(data)
	case JointMagic:
		return decodeGTR3(data)
	default:
		return 0, 0, nil, errors.New("不是 GTR1/GTR3 联合流")
	}
}

func decodeGTR3(data []byte) (int, int, []uint8, error) {
	if len(data) < JointStreamHeaderSize {
		return 0, 0, nil, errors.New("GTR3 流头被截断")
	}
	w := int(binary.LittleEndian.Uint16(data[4:6]))
	h := int(binary.LittleEndian.Uint16(data[6:8]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	name, err := predictorName(data[8])
	if err != nil {
		return 0, 0, nil, err
	}
	reqB := int(binary.LittleEndian.Uint32(data[9:13]))
	n := w * h
	b := reqB
	if b == 0 || b > n {
		b = n
	}

	res := make([]uint8, 0, n)
	off := JointStreamHeaderSize
	for len(res) < n {
		remain := n - len(res)
		want := b
		if remain < want {
			want = remain
		}
		if off+BlockHeaderSize > len(data) {
			return 0, 0, nil, errors.New("GTR3 块头被截断")
		}
		if string(data[off:off+2]) != BlockMagic {
			return 0, 0, nil, errors.New("GTR3 块标记损坏")
		}
		clen := int(binary.LittleEndian.Uint32(data[off+2 : off+6]))
		off += BlockHeaderSize
		if clen <= 0 || off+clen > len(data) {
			return 0, 0, nil, errors.New("GTR3 压缩块长度非法或被截断")
		}
		zr := flate.NewReader(bytes.NewReader(data[off : off+clen]))
		block := make([]byte, want)
		if _, err := io.ReadFull(zr, block); err != nil {
			zr.Close()
			return 0, 0, nil, errors.New("GTR3 块解压失败: " + err.Error())
		}
		if err := zr.Close(); err != nil {
			return 0, 0, nil, errors.New("GTR3 块校验失败: " + err.Error())
		}
		res = append(res, block...)
		off += clen
	}
	if len(res) != n || off != len(data) {
		return 0, 0, nil, errors.New("GTR3 长度与画幅不符")
	}

	pix := make([]uint8, n)
	switch name {
	case PredictNone:
		copy(pix, res)
	case PredictLeft:
		for y := 0; y < h; y++ {
			base := y * w
			for x := 0; x < w; x++ {
				i := base + x
				pred := 0
				if x > 0 {
					pred = int(pix[i-1])
				}
				pix[i] = uint8(pred + int(res[i]))
			}
		}
	}
	return w, h, pix, nil
}
