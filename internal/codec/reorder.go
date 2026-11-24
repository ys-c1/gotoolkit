package codec

import (
	"encoding/binary"
	"errors"
)

// 本文件实现“索引重排最小化游程数”的精确算法与近似算法，以及可逆的 GTR2 格式。
//
// 理论背景（详见 docs/palette-reorder.md）：
//   - 重排问题等价于路径图 P_n 上指定色类大小的着色问题，游程数 = 1 + 双色边数。
//   - 着色下界：k 种颜色至少产生 k 个单色块，故 R >= k；计数排序使每色一块，达到 R* = k。
//   - 分窗贪心是 ⌈n/W⌉-近似：R <= ⌈n/W⌉ * R*。

// CountRuns 统计整个序列的游程数（不抽样）。
func CountRuns(pix []uint8) int {
	if len(pix) == 0 {
		return 0
	}
	r := 1
	for i := 1; i < len(pix); i++ {
		if pix[i] != pix[i-1] {
			r++
		}
	}
	return r
}

// ReorderByValue 用计数排序把索引序列按值稳定重排，使每种值聚成一段，
// 达到游程数理论最优 R* = k（不同值个数）。
// 返回重排后的序列与位置置换 perm：seq[j] = pix[perm[j]]。
// 时间 Θ(n + U)，空间 Θ(n + U)，U = 256 为灰度级数。
func ReorderByValue(pix []uint8) (seq []uint8, perm []int) {
	var cnt, off [256]int
	for _, v := range pix {
		cnt[v]++
	}
	sum := 0
	for v := 0; v < 256; v++ {
		off[v] = sum
		sum += cnt[v]
	}
	seq = make([]uint8, len(pix))
	perm = make([]int, len(pix))
	for i, v := range pix {
		j := off[v]
		off[v]++
		seq[j] = v
		perm[j] = i
	}
	return seq, perm
}

// BatchWindowRuns 是分窗贪心近似算法：把序列切成不超过 W 的连续窗口，
// 窗内计数排序，跨窗合并同值段，返回重排后的游程数。
// 误差界：R <= ⌈n/W⌉ * R*。window <= 0 表示不分窗（退化为精确算法）。
// 时间 O(n + (n/W)*U)，额外空间 O(W + U)。
func BatchWindowRuns(pix []uint8, window int) int {
	if len(pix) == 0 {
		return 0
	}
	if window <= 0 {
		window = len(pix)
	}
	runs := 0
	prev := -1
	for start := 0; start < len(pix); start += window {
		end := start + window
		if end > len(pix) {
			end = len(pix)
		}
		var cnt [256]int
		for _, v := range pix[start:end] {
			cnt[v]++
		}
		for v := 0; v < 256; v++ {
			if cnt[v] == 0 {
				continue
			}
			if v != prev {
				runs++
				prev = v
			}
		}
	}
	return runs
}

// EncodeRLE2 输出可逆重排游程格式：
//
//	GTR2 + 宽高 + (计数, 值) 序列 + 置换的差分变长流
//
// 像素先经 ReorderByValue 重排使游程数达到最优 k；置换 perm 按
// “同值段内位置递增”的性质做差分（段内存 perm[j]-perm[j-1]，段首存 perm[j]），
// 以 LEB128 变长写出，解码端可据此逆置换回原图，逐像素一致。
func EncodeRLE2(w, h int, pix []uint8) ([]byte, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if w > 0xffff || h > 0xffff {
		return nil, errors.New("画幅超出 GTR2 头范围")
	}
	seq, perm := ReorderByValue(pix)
	out := make([]byte, 8, 8+len(pix)*2)
	copy(out[0:], "GTR2")
	binary.LittleEndian.PutUint16(out[4:], uint16(w))
	binary.LittleEndian.PutUint16(out[6:], uint16(h))
	i := 0
	for i < len(seq) {
		j := i
		for j < len(seq) && seq[j] == seq[i] && j-i < 255 {
			j++
		}
		out = append(out, uint8(j-i), seq[i])
		i = j
	}
	prev := 0
	for j := 0; j < len(seq); j++ {
		d := perm[j]
		if j > 0 && seq[j] == seq[j-1] {
			d = perm[j] - prev // 稳定排序保证同值段内位置递增，d >= 1
		}
		prev = perm[j]
		out = appendUvarint(out, uint64(d))
	}
	return out, nil
}

// DecodeRLE2 解析 GTR2 流：先解游程得重排序列，再按差分规则重建置换，
// 逆置换还原原图。返回的像素与编码前逐字节一致。
func DecodeRLE2(data []byte) (int, int, []uint8, error) {
	if len(data) < 8 || string(data[0:4]) != "GTR2" {
		return 0, 0, nil, errors.New("不是 GTR2 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return 0, 0, nil, errors.New("画幅非法")
	}
	n := w * h
	seq := make([]uint8, 0, n)
	i := 8
	for len(seq) < n {
		if i+2 > len(data) {
			return 0, 0, nil, errors.New("游程数据不足")
		}
		cnt, v := int(data[i]), data[i+1]
		if cnt == 0 {
			return 0, 0, nil, errors.New("游程计数为零")
		}
		i += 2
		for k := 0; k < cnt; k++ {
			seq = append(seq, v)
		}
		if len(seq) > n {
			return 0, 0, nil, errors.New("游程超出画幅")
		}
	}
	perm := make([]int, n)
	seen := make([]bool, n)
	prev := 0
	for j := 0; j < n; j++ {
		d, m := binary.Uvarint(data[i:])
		if m <= 0 {
			return 0, 0, nil, errors.New("置换变长流损坏")
		}
		i += m
		p := int(d)
		if j > 0 && seq[j] == seq[j-1] {
			p = prev + int(d)
		}
		if p < 0 || p >= n {
			return 0, 0, nil, errors.New("置换位置越界")
		}
		if seen[p] {
			return 0, 0, nil, errors.New("置换位置重复")
		}
		seen[p] = true
		perm[j] = p
		prev = p
	}
	if i != len(data) {
		return 0, 0, nil, errors.New("GTR2 尾部有多余字节")
	}
	pix := make([]uint8, n)
	for j := 0; j < n; j++ {
		pix[perm[j]] = seq[j]
	}
	return w, h, pix, nil
}

// appendUvarint 以 LEB128 追加无符号变长整数。
func appendUvarint(out []byte, x uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	m := binary.PutUvarint(buf[:], x)
	return append(out, buf[:m]...)
}
