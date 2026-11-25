// Package hybrid 实现按 16×16 块分区、平坦块/纹理块走两条编码路径的混合编码。
//
// # 分区判据
//
// 每个块统计精确整数方差：n·Σ(x−μ)² = n·Σx² − (Σx)²。块判为平坦当且仅当
//
//	12·(n·Σx² − (Σx)²) < 287·n²
//
// 它等价于 σ² < 287/12，即 σ < √(287/12) ≈ 4.8905。这个阈值不是超参数，而是
// 令两条路径的单位像素失真相等解出来的：纹理路径恒为 24，平坦路径为 σ²+1/12
// （1/12 是块均值取整的均方误差），交叉点 σ²=287/12。推导见《报告》。
// 全部比较用 int64，没有浮点，因此判定在任何平台上逐位一致。
//
// # 两条路径
//
//	平坦块：用块均值四舍五入后的单值重建，每块 1 字节（这就是“色块”）。
//	纹理块：16 级量化器（步长 17，电平 0,17,…,255），叠加由像素坐标哈希出的
//	        确定相位减性抖动，每像素 4 bit（每块 128 字节）。相位 e 取 −8..8 的
//	        17 个余数，误差恰为 x+e 模 17 的最小余数，绝对值 ≤8、均方恰为 24。
//	        相位不写入码流，解码端按同一哈希重算。
//
// # 容器
//
//	"GTH1"(4) | 块边长(1)=16 | 宽(2, LE) | 高(2, LE) | 掩码 | 平坦块单值 | 纹理块半字节
//
// 掩码按块光栅序、每字节高位优先，1 表示纹理块；总长 (块数+7)/8 字节。
// 宽高必须是 16 的倍数，且都能用 uint16 表示。
package hybrid

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// 格式常量。步长与块边长写进码流（块边长 1 字节），解码端据此校验。
const (
	Block  = 16 // 块边长（像素）
	Step   = 17 // 纹理量化步长：16 级恰好铺满 0..255
	Levels = 16 // 16 级，每像素 4 bit
	Header = 9  // "GTH1" + 块边长 + 宽 + 高
	Mask1  = 0  // 掩码位：平坦
	Mask2  = 1  // 掩码位：纹理
)

// 失真模型（单位：每像素均方误差）。
const (
	TexMSE  = 24.0       // 纹理路径：17 个余数 −8..8 的均方 = 2(1²+…+8²)/17 = 24
	FlatOff = 1.0 / 12   // 平坦路径：块均值取整的均方误差
	TauSq   = 287.0 / 12 // TexMSE − FlatOff，交叉点
)

// Tau 是平坦/纹理的分界标准差 √(287/12) ≈ 4.8905。仅用于报告与测试；判定走整数式。
var Tau = math.Sqrt(TauSq)

var (
	ErrSize   = errors.New("混合格式要求宽高为 16 的正整数倍")
	ErrFormat = errors.New("不是 GTH1 流")
	ErrBody   = errors.New("GTH1 数据被截断或长度不符")
)

// BlockIndex 把块坐标映射为光栅序下标。
func BlockIndex(bx, by, bpr int) int { return by*bpr + bx }

// FlatByModel 返回块是否应判为平坦：12(nΣx²−(Σx)²) < 287n²。
// 调用方保证 n>0，且 sum/sumSq 在 int64 范围内（16×16 块恒成立）。
func FlatByModel(n int, sum, sumSq int64) bool {
	lhs := 12 * (int64(n)*sumSq - sum*sum)
	rhs := int64(287) * int64(n) * int64(n)
	return lhs < rhs
}

// Variance 返回块方差 Σ(x−μ)²/n 的精确值（双精度只用于展示）。
func Variance(n int, sum, sumSq int64) float64 {
	// σ² = Σx²/n − (Σx/n)²，用整数先算 n·Σ(x−μ)² 再除，避免相消误差。
	num := int64(n)*sumSq - sum*sum // = n·Σ(x−μ)²
	return float64(num) / float64(int64(n)*int64(n))
}

// Sigma 返回块标准差。
func Sigma(n int, sum, sumSq int64) float64 { return mathSqrt(Variance(n, sum, sumSq)) }

// mathSqrt 是 math.Sqrt 的薄封装。
func mathSqrt(x float64) float64 { return math.Sqrt(x) }

// Mean 返回块均值。
func Mean(n int, sum int64) float64 { return float64(sum) / float64(n) }

// FlatLevel 返回平坦块的单值重建：块均值四舍五入到最近的整数灰度。
func FlatLevel(n int, sum int64) uint8 {
	// round(sum/n) = floor((2·sum + n) / (2n))，sum≥0 时成立。
	v := (2*sum + int64(n)) / (2 * int64(n))
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(v)
}

// dither 返回像素 (x,y) 的确定相位偏移，取值 −8..8，均匀覆盖 17 个余数。
// 用 splitmix32 对全局像素下标做终混；编解码两端各自重算，不占码流。
func dither(x, y, w int) int {
	idx := uint32(y*w + x)
	z := idx + 0x9E3779B9
	z = (z ^ (z >> 16)) * 0x21F0AAAD
	z = (z ^ (z >> 15)) * 0x735A2D97
	z ^= z >> 15
	return int(z%uint32(Step)) - Step/2
}

// texRecon 返回一个原始像素在纹理路径下的重建值。
func texRecon(x, e int) uint8 {
	return texReconFromIndex(texIndex(x, e), e)
}

// texIndex 返回纹理路径的 4 bit 索引；texRecon 可由索引与相位重建。
func texIndex(x, e int) uint8 {
	q := (x + e + Step/2) / Step // x+e+8 ∈ [0,271)，非负
	if q < 0 {
		q = 0
	}
	if q > Levels-1 {
		q = Levels - 1
	}
	return uint8(q)
}

// texReconFromIndex 由 4 bit 索引和相位还原灰度，与 Encode 的路径逐位一致。
func texReconFromIndex(q uint8, e int) uint8 {
	v := int(q)*Step - e
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(v)
}

// Encode 把 16 的整数倍画幅的灰度图编码为 GTH1 混合码流。
func Encode(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || w > 0xFFFF || h > 0xFFFF ||
		w%Block != 0 || h%Block != 0 || len(pix) != w*h {
		return nil, fmt.Errorf("%w: %dx%d", ErrSize, w, h)
	}
	bpr, bpc := w/Block, h/Block
	nBlocks := bpr * bpc
	mask := make([]byte, (nBlocks+7)/8)
	type info struct {
		flat  bool
		level uint8
	}
	blocks := make([]info, nBlocks)
	flatN, texN := 0, 0
	for by := 0; by < bpc; by++ {
		for bx := 0; bx < bpr; bx++ {
			var sum, sumSq int64
			for y := by * Block; y < by*Block+Block; y++ {
				row := y * w
				for x := bx * Block; x < bx*Block+Block; x++ {
					v := int64(pix[row+x])
					sum += v
					sumSq += v * v
				}
			}
			idx := BlockIndex(bx, by, bpr)
			b := info{flat: FlatByModel(Block*Block, sum, sumSq), level: FlatLevel(Block*Block, sum)}
			blocks[idx] = b
			if !b.flat {
				mask[idx/8] |= 1 << (7 - uint(idx%8))
				texN++
			} else {
				flatN++
			}
		}
	}
	out := make([]byte, 0, Header+len(mask)+flatN+texN*Block*Block/2)
	out = append(out, 'G', 'T', 'H', '1', byte(Block))
	out = binary.LittleEndian.AppendUint16(out, uint16(w))
	out = binary.LittleEndian.AppendUint16(out, uint16(h))
	out = append(out, mask...)
	// 先写全部平坦块单值，再写全部纹理块半字节：解码端按掩码计数即可定位。
	for _, b := range blocks {
		if b.flat {
			out = append(out, b.level)
		}
	}
	nib := make([]byte, 0, Block*Block/2)
	for by := 0; by < bpc; by++ {
		for bx := 0; bx < bpr; bx++ {
			if blocks[BlockIndex(bx, by, bpr)].flat {
				continue
			}
			nib = nib[:0]
			for y := by * Block; y < by*Block+Block; y++ {
				for x := bx * Block; x < bx*Block+Block; x += 2 {
					hi := texIndex(int(pix[y*w+x]), dither(x, y, w))
					lo := texIndex(int(pix[y*w+x+1]), dither(x+1, y, w))
					nib = append(nib, hi<<4|lo)
				}
			}
			out = append(out, nib...)
		}
	}
	return out, nil
}

// Decode 解码 GTH1 码流，返回画幅与像素。
func Decode(data []byte) (int, int, []uint8, error) {
	w, h, pix, _, err := DecodeMasked(data)
	return w, h, pix, err
}

// DecodeMasked 解码 GTH1，同时返回整幅的 0/255 掩码图（0 平坦、255 纹理）。
func DecodeMasked(data []byte) (int, int, []uint8, []uint8, error) {
	if len(data) < Header || string(data[0:4]) != "GTH1" {
		return 0, 0, nil, nil, ErrFormat
	}
	if data[4] != Block {
		return 0, 0, nil, nil, fmt.Errorf("%w: 块边长 %d", ErrFormat, data[4])
	}
	w := int(binary.LittleEndian.Uint16(data[5:7]))
	h := int(binary.LittleEndian.Uint16(data[7:9]))
	if w <= 0 || h <= 0 || w%Block != 0 || h%Block != 0 {
		return 0, 0, nil, nil, fmt.Errorf("%w: %dx%d", ErrSize, w, h)
	}
	bpr, bpc := w/Block, h/Block
	nBlocks := bpr * bpc
	maskLen := (nBlocks + 7) / 8
	off := Header + maskLen
	if len(data) < off {
		return 0, 0, nil, nil, ErrBody
	}
	maskBits := data[Header:off]
	flatN := 0
	for i := 0; i < nBlocks; i++ {
		if maskBits[i/8]&(1<<(7-uint(i%8))) == 0 {
			flatN++
		}
	}
	texN := nBlocks - flatN
	want := off + flatN + texN*Block*Block/2
	if len(data) != want {
		return 0, 0, nil, nil, fmt.Errorf("%w: 长度 %d，期望 %d", ErrBody, len(data), want)
	}
	levels := data[off : off+flatN]
	off += flatN
	pix := make([]uint8, w*h)
	mask := make([]uint8, w*h)
	li := 0
	for by := 0; by < bpc; by++ {
		for bx := 0; bx < bpr; bx++ {
			idx := BlockIndex(bx, by, bpr)
			base := by * Block * w
			textured := maskBits[idx/8]&(1<<(7-uint(idx%8))) != 0
			for y := 0; y < Block; y++ {
				row := base + y*w + bx*Block
				if !textured {
					for x := 0; x < Block; x++ {
						pix[row+x] = levels[li]
						mask[row+x] = 0
					}
				} else {
					for x := 0; x < Block; x++ {
						gx, gy := bx*Block+x, by*Block+y
						b := data[off+(y*Block+x)/2]
						var q uint8
						if x%2 == 0 {
							q = b >> 4
						} else {
							q = b & 0x0F
						}
						e := dither(gx, gy, w)
						pix[row+x] = texReconFromIndex(q, e)
						mask[row+x] = 255
					}
				}
			}
			if textured {
				off += Block * Block / 2
			}
			if !textured {
				li++
			}
		}
	}
	return w, h, pix, mask, nil
}
