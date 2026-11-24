package codec

import (
	"encoding/binary"
	"errors"
	"math"
)

// GTR2：块级自适应游程格式。
//
// 与 GTR1 的差异：GTR1 对整幅图用固定上限 255 逐游程编码，长渐变时每个游程
// 长度为 1，2 字节/像素反而膨胀。GTR2 把像素切成块，每块按自身符号熵独立
// 决定游程上限与是否转义，并对“不够细”的块递归二分。
//
// 主头（9 字节）：
//
//	"GTR2"            4 字节魔数
//	uint16 LE 宽       2 字节
//	uint16 LE 高       2 字节
//	uint8  基准块长    1 字节（64..255），编码端固定 255
//
// 块头（2 字节）：
//
//	byte0  模式与游程上限：
//	       0x00            转义块，负载为原始字节
//	       1..255          游程块，该值即游程上限 cap
//	       （游程 cap 最小为 1，故 0 可安全留作转义标记）
//	byte1  块像素长度-1（块长 1..255）
//
// 游程块负载：若干 (计数, 值)，计数 <= cap，块内计数之和等于块长。
// 转义块负载：块长个原始字节。
//
// 基准块长取 255 而非 256：GTR1 的游程上限就是 255，块边界与 GTR1 的天然
// 游程切点对齐。tier 1 下 cap 被钳在 255，当块边界恰好是游程切点时（常值、
// 或游程整除块长），该块发出的 (计数,值) 与 GTR1 对同段像素的输出逐字节
// 相同；混合数据上跨界游程仍会在块边界多切一对，这是分块格式的固有代价。
const (
	rle2Magic      = "GTR2"
	rle2HeaderSize = 9
	rle2BlockHead  = 2

	rle2MaxBlock = 255 // 与 GTR1 游程上限对齐
	rle2MinBlock = 64  // 再细的块头开销会吃掉收益

	// rle2EscapeEntropy 是转义的熵阈值（比特/像素）。
	// 依据：iid 符号源的平均游程长度 E[L]=1/(1-Σp²)，而碰撞熵 H2=-log2(Σp²)，
	// 故 E[L]=1/(1-2^-H2)。取 H2=4.0 时 E[L]=1/(1-1/16)=1.0667，平均游程已
	// 接近长度 1；此时 (计数,值) 的 2 字节开销相对 1 字节原始值接近翻倍，
	// 游程编码注定不划算，应转义。H2<4 的中间地带留给游程块，由三档 cap
	// 系数体现不同的游程上限策略。
	rle2EscapeEntropy = 4.0
)

// rle2TierK 是三档“亮度系数” K：cap = clamp(round(K/H), 1, 255)。
// 量纲为比特——K/H 把每像素比特数换算成该熵水平下值得允许的游程长度。
var rle2TierK = [3]int{2040, 512, 128}

// RLE2Stats 汇总一次 GTR2 编码的分块与体积指标。
type RLE2Stats struct {
	Tier            int     // 使用的档位 1..3
	K               int     // 该档系数
	BlockCount      int     // 最终块数
	EscapeBlocks    int     // 转义块数
	RLEBlocks       int     // 游程块数
	SplitCount      int     // 递归二分次数
	MaxBlock        int     // 起始（最大）块长
	EntropyBoundRaw int     // 仅按块熵求和的下界（字节，ceil）
	PayloadBytes    int     // 块头+块负载，不含主头
	TotalBytes      int     // 含主头的完整流长度
	WorstRatio      float64 // max(块编码字节 / 块熵下界字节)，应 <=1.10
}

// shannonEntropy 返回块的香农熵（比特/像素）。
func shannonEntropy(block []uint8) float64 {
	if len(block) == 0 {
		return 0
	}
	var freq [256]int
	for _, v := range block {
		freq[v]++
	}
	n := float64(len(block))
	h := 0.0
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// rle2Cap 按档位系数 K 与块熵 H 反解游程上限。
// H==0（常值块）时无不确定性，直接给最大上限 255。
func rle2Cap(k int, h float64) int {
	if h <= 0 {
		return 255
	}
	capv := int(math.Round(float64(k) / h))
	if capv < 1 {
		capv = 1
	}
	if capv > 255 {
		capv = 255
	}
	return capv
}

// rle2ShouldEscape 判断块是否转义为原始字节。两个条件满足其一即触发：
//  1. 香农熵 >= rle2EscapeEntropy（4.0 比特/像素）：理论游程期望已接近 1，
//     游程编码注定膨胀；
//  2. 实测游程对负载（2*runs）严格超过原始字节数：即使熵略低，该具体块的
//     游程也压不住。块长 <=2 时豁免：一个游程对最多 2 字节，与原始字节
//     同价，谈不上膨胀（避免 1 像素尾块被误判转义）。
//
// 游程计数统一走 rle2RunCount（255 钳制），与编码口径一致。
func rle2ShouldEscape(block []uint8, h float64) bool {
	if h >= rle2EscapeEntropy {
		return true
	}
	if len(block) <= 2 {
		return false
	}
	return rle2RunCount(block)*2 > len(block)
}

// rle2RunCount 统计块内（上限 255 的）游程数，用于实测转义判断。
func rle2RunCount(block []uint8) int {
	return rle2RunCountCap(block, 255)
}

// rle2RunCountCap 统计块内按指定 cap 切分后的游程对数：超过 cap 的连续
// 游程会被拆成多对。用于递归时按该块实际 cap 预估游程负载。
func rle2RunCountCap(block []uint8, cap int) int {
	runs := 0
	for i := 0; i < len(block); {
		j := i
		for j < len(block) && block[j] == block[i] && j-i < cap {
			j++
		}
		runs++
		i = j
	}
	return runs
}

// encodeRLE2Block 编码单个块（不再递归），返回块头+负载。
// 块长必须在 1..rle2MaxBlock(255) 内，否则块头长度字节无法表示。
func encodeRLE2Block(block []uint8, tier int) []byte {
	if len(block) == 0 || len(block) > rle2MaxBlock {
		panic("encodeRLE2Block: 块长越界")
	}
	k := rle2TierK[tier-1]
	h := shannonEntropy(block)
	out := make([]byte, 0, rle2BlockHead+len(block)*2)
	if rle2ShouldEscape(block, h) {
		out = append(out, 0x00, uint8(len(block)-1))
		out = append(out, block...)
		return out
	}
	capv := rle2Cap(k, h)
	out = append(out, uint8(capv), uint8(len(block)-1))
	for i := 0; i < len(block); {
		j := i
		for j < len(block) && block[j] == block[i] && j-i < capv {
			j++
		}
		out = append(out, uint8(j-i), block[i])
		i = j
	}
	return out
}

// rle2EncodeRec 递归编码。
//
// 一成判据只对游程块生效：游程块负载是 (计数,值) 对，可能显著超过熵下界；
// 转义块负载就是原始字节，对熵下界的比值恒在 1.0 附近（ceil 取整），
// 不存在“分块不够细”，直接接受，不为它浪费块头去递归。
func rle2EncodeRec(block []uint8, tier int, st *RLE2Stats, out *[]byte) {
	h := shannonEntropy(block)
	bound := int(math.Ceil(h * float64(len(block)) / 8.0))
	escaped := rle2ShouldEscape(block, h)

	if !escaped && len(block) > rle2MinBlock && bound > 0 {
		// 按该块实际 cap 预估游程负载 = 2 * 切分后游程对数；超下界一成则二分。
		capv := rle2Cap(rle2TierK[tier-1], h)
		payloadEst := 2 * rle2RunCountCap(block, capv)
		if float64(payloadEst) > 1.10*float64(bound) {
			st.SplitCount++
			mid := len(block) / 2
			rle2EncodeRec(block[:mid], tier, st, out)
			rle2EncodeRec(block[mid:], tier, st, out)
			return
		}
	}

	enc := encodeRLE2Block(block, tier)
	payload := len(enc) - rle2BlockHead
	st.BlockCount++
	st.EntropyBoundRaw += bound
	st.PayloadBytes += len(enc)
	if escaped {
		st.EscapeBlocks++
	} else {
		st.RLEBlocks++
	}
	// 最差块比按实际负载口径统计；常值块 bound=0，记 0 表示无熵可对照。
	ratio := 0.0
	if bound > 0 {
		ratio = float64(payload) / float64(bound)
	}
	if ratio > st.WorstRatio {
		st.WorstRatio = ratio
	}
	*out = append(*out, enc...)
}

// EncodeRLE2 输出 GTR2 块级自适应游程流。tier 取 1..3。
// st 可为 nil；非 nil 时填充分块统计。
func EncodeRLE2(w, h int, pix []uint8, tier int, st *RLE2Stats) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if tier < 1 || tier > 3 {
		return nil, errors.New("档位必须在 1..3")
	}
	out := make([]byte, rle2HeaderSize, rle2HeaderSize+len(pix)*2)
	copy(out[0:], rle2Magic)
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	out[8] = uint8(rle2MaxBlock)

	local := &RLE2Stats{Tier: tier, K: rle2TierK[tier-1], MaxBlock: rle2MaxBlock}
	for off := 0; off < len(pix); off += rle2MaxBlock {
		end := off + rle2MaxBlock
		if end > len(pix) {
			end = len(pix)
		}
		// 进入递归的块至多 255 像素，块头长度字节可直接存 len-1。
		rle2EncodeRec(pix[off:end], tier, local, &out)
	}
	local.TotalBytes = len(out)
	if st != nil {
		*st = *local
	}
	return out, nil
}

// DecodeRLE2 解析 GTR2 流并回还原像素。
func DecodeRLE2(data []byte) (int, int, []uint8, error) {
	if len(data) < rle2HeaderSize || string(data[0:4]) != rle2Magic {
		return 0, 0, nil, errors.New("不是 GTR2 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	base := int(data[8])
	if base < rle2MinBlock || base > rle2MaxBlock {
		return 0, 0, nil, errors.New("基准块长非法")
	}
	pix := make([]uint8, 0, w*h)
	i := rle2HeaderSize
	for i < len(data) {
		if i+rle2BlockHead > len(data) {
			return 0, 0, nil, errors.New("块头被截断")
		}
		mode := data[i]
		blen := int(data[i+1]) + 1
		i += rle2BlockHead
		if mode == 0 {
			// 转义块：负载恰好 blen 个原始字节，可直接按块长截断检查。
			if i+blen > len(data) {
				return 0, 0, nil, errors.New("转义块负载被截断")
			}
			pix = append(pix, data[i:i+blen]...)
			i += blen
			continue
		}
		// 游程块：负载是变长的 (计数,值) 序列，按游程对扫描，累计计数
		// 达到块长 blen 即定位到下一块；不能用 blen 当负载字节数。
		capv := int(mode)
		consumed := 0
		for consumed < blen {
			if i+1 >= len(data) {
				return 0, 0, nil, errors.New("游程被截断")
			}
			n, v := int(data[i]), data[i+1]
			i += 2
			if n == 0 || n > capv {
				return 0, 0, nil, errors.New("游程计数越界")
			}
			for k := 0; k < n; k++ {
				pix = append(pix, v)
			}
			consumed += n
			if consumed > blen {
				return 0, 0, nil, errors.New("块游程长度不匹配")
			}
		}
	}
	if len(pix) != w*h {
		return 0, 0, nil, errors.New("像素数不足")
	}
	return w, h, pix, nil
}
