package codec

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
)

// 压缩链路容器 GTC1：在 pgm/bmp/rle 字节流之外套一层链路头。
// 链路顺序固定为：预测编码 -> 游程合并 -> 体校验和（游程一定在预测之后做）。
//
// 头部布局（偏移精确到字节）：
//
//	0   "GTC1"   4 字节 magic
//	4   flags    1 字节  bit0=预测 bit1=游程 bit2=校验和
//	5   order    1 字节  实际使用的预测阶数（未启用为 0）
//	6   crc32    4 字节  小端，对 payload（头部之后全部字节）的 CRC32；未启用为 0
//	10  format   1 字节  0=pgm 1=bmp 2=rle
//	11  reserved 1 字节  0
//	12  payload...
const (
	PipeMagic      = "GTC1"
	PipeHeaderSize = 12
	// ChecksumOffset 是体校验和在文件头中的字节偏移，精确定位到字节。
	ChecksumOffset = 6
)

const (
	flagPredict  uint8 = 1 << 0
	flagMergeRun uint8 = 1 << 1
	flagChecksum uint8 = 1 << 2
)

// 内层格式标识。
const (
	FormatPGM uint8 = 0
	FormatBMP uint8 = 1
	FormatRLE uint8 = 2
)

// PredictOrderCandidates 是预测阶数的候选集。
// 自动选阶时只在这三档上各算一次残差熵，取熵最小者。
var PredictOrderCandidates = []int{1, 2, 3}

var (
	ErrNotPipe          = errors.New("不是 GTC1 流")
	ErrBadPredictOrder  = errors.New("预测阶数非法")
	ErrBadChecksumParam = errors.New("校验参数非法")
	ErrChecksumMismatch = errors.New("体校验和不匹配")
	ErrRunStream        = errors.New("游程流损坏")
)

// PipeOptions 是压缩链路的三项开关。
type PipeOptions struct {
	Predict   bool   // 启用预测编码
	Order     int    // 0=自动（按残差熵最小从候选集 {1,2,3} 选）；1..3=固定阶数
	MergeRuns bool   // 游程合并，在预测之后执行
	Checksum  string // ""=关闭；"crc32"=把体校验和写进文件头 ChecksumOffset 处
}

// IsPipe 判断数据是否带 GTC1 链路头。
func IsPipe(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == PipeMagic
}

// EncodePipe 把已编码的格式字节流送入压缩链路并套 GTC1 头。
func EncodePipe(body []byte, format uint8, po PipeOptions) ([]byte, error) {
	if po.Order < 0 || po.Order > 3 {
		return nil, ErrBadPredictOrder
	}
	if po.Checksum != "" && po.Checksum != "crc32" {
		return nil, ErrBadChecksumParam
	}
	payload := body
	var flags uint8
	order := 0
	if po.Predict {
		order = po.Order
		if order == 0 {
			order, _ = SelectOrder(payload)
		}
		payload = PredictEncode(payload, order)
		flags |= flagPredict
	}
	if po.MergeRuns {
		payload = mergeRuns(payload)
		flags |= flagMergeRun
	}
	var crc uint32
	if po.Checksum == "crc32" {
		crc = crc32.ChecksumIEEE(payload)
		flags |= flagChecksum
	}
	out := make([]byte, PipeHeaderSize, PipeHeaderSize+len(payload))
	copy(out[0:], PipeMagic)
	out[4] = flags
	out[5] = uint8(order)
	binary.LittleEndian.PutUint32(out[ChecksumOffset:], crc)
	out[10] = format
	return append(out, payload...), nil
}

// DecodePipe 解开 GTC1 容器：先验体校验和，再依次逆游程、逆预测。
func DecodePipe(data []byte) (body []byte, format uint8, err error) {
	if len(data) < PipeHeaderSize || !IsPipe(data) {
		return nil, 0, ErrNotPipe
	}
	flags := data[4]
	order := int(data[5])
	format = data[10]
	payload := data[PipeHeaderSize:]
	if flags&flagChecksum != 0 {
		stored := binary.LittleEndian.Uint32(data[ChecksumOffset:])
		if crc32.ChecksumIEEE(payload) != stored {
			return nil, 0, ErrChecksumMismatch
		}
	}
	if flags&flagMergeRun != 0 {
		if payload, err = splitRuns(payload); err != nil {
			return nil, 0, err
		}
	}
	if flags&flagPredict != 0 {
		if order < 1 || order > 3 {
			return nil, 0, ErrBadPredictOrder
		}
		payload = PredictDecode(payload, order)
	}
	return payload, format, nil
}

// PredictEncode 做 order 阶差分预测：r[i] = data[i] - data[i-order]（模 256，i<order 时前值为 0）。
func PredictEncode(data []uint8, order int) []uint8 {
	out := make([]uint8, len(data))
	for i := range data {
		var prev uint8
		if i >= order {
			prev = data[i-order]
		}
		out[i] = data[i] - prev
	}
	return out
}

// PredictDecode 是 PredictEncode 的逆变换。
func PredictDecode(data []uint8, order int) []uint8 {
	out := make([]uint8, len(data))
	for i := range data {
		var prev uint8
		if i >= order {
			prev = out[i-order]
		}
		out[i] = data[i] + prev
	}
	return out
}

// ResidualEntropy 计算 data 在 order 阶预测后的残差熵（比特/字节）。
func ResidualEntropy(data []uint8, order int) float64 {
	if len(data) == 0 {
		return 0
	}
	var hist [256]int
	for i := range data {
		var prev uint8
		if i >= order {
			prev = data[i-order]
		}
		hist[uint8(data[i]-prev)]++
	}
	n := float64(len(data))
	h := 0.0
	for _, c := range hist {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// SelectOrder 在候选集 {1,2,3} 上各算一次残差熵，返回熵最小的阶数（并列取低阶）。
// 选取依据：残差熵越低，残差分布越集中，后续游程合并的可压缩空间越大。
func SelectOrder(data []uint8) (best int, entropies [3]float64) {
	bestIdx := 0
	for i, order := range PredictOrderCandidates {
		entropies[i] = ResidualEntropy(data, order)
		if entropies[i] < entropies[bestIdx] { // 严格小于：熵并列时保留低阶
			bestIdx = i
		}
	}
	return PredictOrderCandidates[bestIdx], entropies
}

// mergeRuns 把字节流压成 (计数, 值) 对，计数上限 255。
func mergeRuns(data []uint8) []uint8 {
	out := make([]uint8, 0, len(data))
	i := 0
	for i < len(data) {
		j := i
		for j < len(data) && data[j] == data[i] && j-i < 255 {
			j++
		}
		out = append(out, uint8(j-i), data[i])
		i = j
	}
	return out
}

// splitRuns 是 mergeRuns 的逆变换。
func splitRuns(data []uint8) ([]uint8, error) {
	if len(data)%2 != 0 {
		return nil, ErrRunStream
	}
	out := make([]uint8, 0, len(data)*2)
	for i := 0; i < len(data); i += 2 {
		n, v := int(data[i]), data[i+1]
		if n == 0 {
			return nil, ErrRunStream
		}
		for k := 0; k < n; k++ {
			out = append(out, v)
		}
	}
	return out, nil
}
