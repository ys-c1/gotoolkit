package subband

import (
	"encoding/binary"
	"math"
)

// 线格式（小端）：
//
//	"SB1\n" 4 字节；W,H 各 uint16；levels,m 各 1 字节；
//	随后 m 条固定 25 字节记录（按带下标顺序）：bits(uint8)+mean/min/step 三个 float64；
//	随后 m 字节优先级顺序（带下标，载荷按此顺序打包，最重要在前）；
//	最后是量化下标位流，每系数 bits_i 位、LSB 在前，跨带连续打包。
//
// 头部长度只依赖 m，与各带比特分配无关，因此同总比特的两条流字节数严格相同。

// HeaderLen 返回 m 个带时的固定头长度。
func HeaderLen(m int) int { return 4 + 2 + 2 + 1 + 1 + 25*m + m }

// StreamBytes 返回给定用量下的完整文件字节数。
func StreamBytes(m, usedBits int) int {
	return HeaderLen(m) + (usedBits+7)/8
}

type bitWriter struct {
	buf []byte
	pos int // 已写入的比特数
}

func (w *bitWriter) writeBits(value, n int) {
	for k := 0; k < n; k++ {
		if value&(1<<uint(k)) != 0 {
			w.buf[w.pos/8] |= 1 << uint(w.pos%8)
		}
		w.pos++
	}
}

// Pack 按 priority 顺序打包全部量化下标。priority 必须是 0..m-1 的排列。
func Pack(s *Setup, res *RunResult, priority []int) []byte {
	m := len(s.Bands)
	used := res.Used
	total := StreamBytes(m, used)
	out := make([]byte, total)
	copy(out[0:4], []byte("SB1\n"))
	binary.LittleEndian.PutUint16(out[4:6], uint16(s.W))
	binary.LittleEndian.PutUint16(out[6:8], uint16(s.H))
	out[8] = byte(s.Levels)
	out[9] = byte(m)
	off := 10
	for i := 0; i < m; i++ {
		out[off] = byte(res.Bits[i])
		binary.LittleEndian.PutUint64(out[off+1:off+9], math.Float64bits(s.Stats[i].Mean))
		binary.LittleEndian.PutUint64(out[off+9:off+17], math.Float64bits(res.Quants[i].Lo))
		binary.LittleEndian.PutUint64(out[off+17:off+25], math.Float64bits(res.Quants[i].Step))
		off += 25
	}
	for k, idx := range priority {
		out[off+k] = byte(idx)
	}
	payloadStart := off + m

	// 载荷需要多少字节就开多少，bitWriter 直接落在 out 的载荷区。
	bw := &bitWriter{buf: out[payloadStart:], pos: 0}
	for _, i := range priority {
		bits := res.Bits[i]
		if bits <= 0 {
			continue
		}
		for _, idx := range res.Quants[i].Idx {
			bw.writeBits(idx, bits)
		}
	}
	return out
}

// PriorityByMarginalValue 返回“最重要→最不重要”的带序，作为位流摆放与
// 整带截断顺序。统一用方差模型的第一比特边际收益
// w_i·σ_i²·(1−4⁻¹)=0.75·w_i·σ_i² 排序（常数 0.75 不影响次序），
// 避免实测量化器在个别低比特档非单调时打乱优先级；平局按方差降序、下标升序。
func PriorityByMarginalValue(s *Setup) []int {
	m := len(s.Bands)
	idx := make([]int, m)
	for i := range idx {
		idx[i] = i
	}
	value := func(i int) float64 { return s.Weights[i] * s.Stats[i].Var }
	for a := 1; a < m; a++ {
		for b := a; b > 0; b-- {
			i, j := idx[b-1], idx[b]
			vi, vj := value(i), value(j)
			swap := false
			switch {
			case vj > vi+1e-15:
				swap = true
			case absf(vj-vi) <= 1e-12 && j < i:
				swap = true
			}
			if swap {
				idx[b-1], idx[b] = idx[b], idx[b-1]
			} else {
				break
			}
		}
	}
	return idx
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// TruncateForCap 执行“整带截断”规则以满足字节上限 capBytes：
// 按 priority 从末端（最不重要）开始把整带比特置 0，直到流长度≤capBytes。
// 返回新的比特向量、被截带下标（按截断先后）与最终字节数。
// 头部始终保留；若连头部都超过 capBytes，返回错误。
func TruncateForCap(s *Setup, bits []int, priority []int, capBytes int) ([]int, []int, int, error) {
	m := len(s.Bands)
	if HeaderLen(m) > capBytes {
		return nil, nil, 0, errCapTooSmall
	}
	nb := append([]int(nil), bits...)
	var dropped []int
	size := StreamBytes(m, TotalBits(s.NS, nb))
	// priority 末端即最不重要；从尾向头逐带截。
	for k := len(priority) - 1; k >= 0 && size > capBytes; k-- {
		i := priority[k]
		if nb[i] == 0 {
			continue
		}
		nb[i] = 0
		dropped = append(dropped, i)
		size = StreamBytes(m, TotalBits(s.NS, nb))
	}
	return nb, dropped, size, nil
}
