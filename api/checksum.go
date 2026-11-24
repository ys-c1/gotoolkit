package api

import (
	"encoding/binary"
	"errors"
	"fmt"

	"gotoolkit/internal/checksum"
)

// ErrChecksum 表示校验和不匹配：文件内容被改动。
var ErrChecksum = errors.New("校验和不匹配")

// ChecksumError 是校验失败的具体信息，被改动位置精确到字节偏移。
type ChecksumError struct {
	Offset     int    // 最可能的被改动字节偏移；-1 表示已检出但无法定位为单字节改动
	Candidates []int  // 全部可解释本次失配的字节偏移（32 位档通常唯一；16 位档大文件上可能多个）
	Stored     uint32 // 头中存放的校验值
	Actual     uint32 // 按文件体实际算出的校验值
	Detail     string // 补充说明
}

func (e *ChecksumError) Error() string {
	if e.Offset >= 0 {
		return fmt.Sprintf("%v: 文件偏移 %d 的字节被改动（%s）", ErrChecksum, e.Offset, e.Detail)
	}
	return fmt.Sprintf("%v: 数据被改动，无法定位为单字节（%s）", ErrChecksum, e.Detail)
}

func (e *ChecksumError) Unwrap() error { return ErrChecksum }

// defaultPolynomial 是 Polynomial 留空时使用的多项式名字。
const defaultPolynomial = "crc32-ieee"

// resolveCRC 把公开配置里的多项式名字解析成计算器；非法名字在此被拒绝。
func resolveCRC(opt Options) (*checksum.CRC, error) {
	name := opt.Polynomial
	if name == "" {
		name = defaultPolynomial
	}
	def, err := checksum.ByName(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadOption, err)
	}
	return checksum.New(def), nil
}

// seal 按格式把校验值写进文件头，返回带校验的完整文件。
// 校验覆盖除校验字段本身外的全部字节（头+体）。
func seal(plain []byte, format string, c *checksum.CRC) []byte {
	switch format {
	case "pgm":
		// 头第二行插入定宽注释行，覆盖其余全部字节。
		line := []byte(fmt.Sprintf("#GTKC%d %0*x\n", c.Width(), c.Width()/4, c.Sum(plain)))
		out := make([]byte, 0, len(plain)+len(line))
		out = append(out, plain[:3]...) // "P5\n"
		out = append(out, line...)
		out = append(out, plain[3:]...)
		return out
	case "bmp":
		// 写入 BMP 头 6..9 保留字节（原编码恒为 0），覆盖其余全部字节。
		out := make([]byte, len(plain))
		copy(out, plain)
		sum := c.Sum(out) // 字段此时为 0
		putLE(out[6:], sum, c.Width()/8)
		return out
	default: // rle
		// 魔数升为 GTR2，校验写在其后 2/4 字节，覆盖其余全部字节。
		w := c.Width() / 8
		out := make([]byte, len(plain)+w)
		copy(out[0:], "GTR2")
		copy(out[4:], plain[4:8])
		copy(out[8+w:], plain[8:])
		sum := c.Sum(out) // 字段此时为 0
		putLE(out[8:], sum, w)
		return out
	}
}

// unseal 校验并剥掉头中的校验字段，返回可交给 codec 的原始字节流。
func unseal(data []byte, format string, c *checksum.CRC) ([]byte, error) {
	switch format {
	case "pgm":
		return unsealPGM(data, c)
	case "bmp":
		return unsealBMP(data, c)
	default:
		return unsealRLE(data, c)
	}
}

func unsealPGM(data []byte, c *checksum.CRC) ([]byte, error) {
	tmpl := fmt.Sprintf("#GTKC%d ", c.Width())
	hexW := c.Width() / 4
	fieldLen := len(tmpl) + hexW + 1 // 模板 + 十六进制 + '\n'
	if len(data) < 3+fieldLen {
		return nil, &ChecksumError{Offset: -1, Detail: "文件过短，头部校验字段缺失"}
	}
	field := data[3 : 3+fieldLen]
	// 模板逐字节比对，字段本身损坏时偏移同样精确到字节。
	for i := 0; i < len(tmpl); i++ {
		if field[i] != tmpl[i] {
			return nil, &ChecksumError{Offset: 3 + i, Candidates: []int{3 + i}, Detail: "头部校验字段损坏"}
		}
	}
	if field[fieldLen-1] != '\n' {
		return nil, &ChecksumError{Offset: 3 + fieldLen - 1, Candidates: []int{3 + fieldLen - 1}, Detail: "头部校验字段损坏"}
	}
	var stored uint32
	for i := 0; i < hexW; i++ {
		v, ok := hexVal(field[len(tmpl)+i])
		if !ok {
			return nil, &ChecksumError{Offset: 3 + len(tmpl) + i, Candidates: []int{3 + len(tmpl) + i}, Detail: "头部校验字段损坏"}
		}
		stored = stored<<4 | uint32(v)
	}
	covered := make([]byte, 0, len(data)-fieldLen)
	covered = append(covered, data[:3]...)
	covered = append(covered, data[3+fieldLen:]...)
	actual := c.Sum(covered)
	if actual == stored {
		return covered, nil
	}
	var offsets []int
	for _, cd := range c.LocateAll(covered, stored) {
		off := cd.Offset
		if off >= 3 {
			off += fieldLen
		}
		offsets = append(offsets, off)
	}
	// 校验字段的十六进制字符被改：差分只落在单个半字节。
	if k, ok := singleNibbleDiff(stored^actual, hexW); ok {
		offsets = append(offsets, 3+len(tmpl)+(hexW-1-k))
	}
	if len(offsets) > 0 {
		return nil, &ChecksumError{Offset: offsets[0], Candidates: offsets, Stored: stored, Actual: actual,
			Detail: locateDetail(len(offsets), stored, actual, hexW)}
	}
	return nil, &ChecksumError{Offset: -1, Stored: stored, Actual: actual,
		Detail: "多处字节被改动"}
}

func unsealBMP(data []byte, c *checksum.CRC) ([]byte, error) {
	const fieldAt = 6 // BMP 头保留字节
	w := c.Width() / 8
	if len(data) < fieldAt+w {
		return nil, &ChecksumError{Offset: -1, Detail: "文件过短，头部校验字段缺失"}
	}
	stored := getLE(data[fieldAt:], w)
	covered := make([]byte, len(data))
	copy(covered, data)
	for i := 0; i < w; i++ {
		covered[fieldAt+i] = 0
	}
	actual := c.Sum(covered)
	if actual == stored {
		return covered, nil
	}
	var offsets []int
	for _, cd := range c.LocateAll(covered, stored) {
		offsets = append(offsets, cd.Offset) // 字段清零模式，偏移一一对应
	}
	if j, ok := singleByteDiff(stored^actual, w); ok {
		offsets = append(offsets, fieldAt+j)
	}
	if len(offsets) > 0 {
		return nil, &ChecksumError{Offset: offsets[0], Candidates: offsets, Stored: stored, Actual: actual,
			Detail: locateDetail(len(offsets), stored, actual, c.Width()/4)}
	}
	return nil, &ChecksumError{Offset: -1, Stored: stored, Actual: actual,
		Detail: "多处字节被改动"}
}

func unsealRLE(data []byte, c *checksum.CRC) ([]byte, error) {
	const fieldAt = 8 // "GTR2" + 宽 + 高 之后
	w := c.Width() / 8
	if len(data) < fieldAt+w {
		return nil, &ChecksumError{Offset: -1, Detail: "文件过短，头部校验字段缺失"}
	}
	for i := 0; i < 4; i++ {
		if data[i] != "GTR2"[i] {
			return nil, &ChecksumError{Offset: i, Candidates: []int{i}, Detail: "头部魔数被改动（校验档应为 GTR2）"}
		}
	}
	stored := getLE(data[fieldAt:], w)
	covered := make([]byte, len(data))
	copy(covered, data)
	for i := 0; i < w; i++ {
		covered[fieldAt+i] = 0
	}
	actual := c.Sum(covered)
	if actual == stored {
		plain := make([]byte, 0, len(data)-w)
		plain = append(plain, "GTR1"...)
		plain = append(plain, covered[4:8]...)
		plain = append(plain, covered[8+w:]...)
		return plain, nil
	}
	var offsets []int
	for _, cd := range c.LocateAll(covered, stored) {
		offsets = append(offsets, cd.Offset) // 字段清零模式，偏移一一对应
	}
	if j, ok := singleByteDiff(stored^actual, w); ok {
		offsets = append(offsets, fieldAt+j)
	}
	if len(offsets) > 0 {
		return nil, &ChecksumError{Offset: offsets[0], Candidates: offsets, Stored: stored, Actual: actual,
			Detail: locateDetail(len(offsets), stored, actual, c.Width()/4)}
	}
	return nil, &ChecksumError{Offset: -1, Stored: stored, Actual: actual,
		Detail: "多处字节被改动"}
}

// locateDetail 组装定位说明；16 位档在大文件上可能有多个候选，如实报告。
func locateDetail(n int, stored, actual uint32, hexW int) string {
	d := fmt.Sprintf("存 %0*x 算 %0*x", hexW, stored, hexW, actual)
	if n > 1 {
		d += fmt.Sprintf("，共 %d 个候选偏移", n)
	}
	return d
}

// singleByteDiff 若 s 只有第 j 个字节非零（LSB 为 0），返回 j。
func singleByteDiff(s uint32, w int) (int, bool) {
	idx, seen := -1, false
	for j := 0; j < w; j++ {
		if byte(s>>(8*j)) != 0 {
			if seen {
				return -1, false
			}
			idx, seen = j, true
		}
	}
	return idx, seen
}

// singleNibbleDiff 若 s 只有第 k 个半字节非零（LSB 为 0），返回 k。
func singleNibbleDiff(s uint32, hexW int) (int, bool) {
	idx, seen := -1, false
	for k := 0; k < hexW; k++ {
		if (s>>(4*k))&0xF != 0 {
			if seen {
				return -1, false
			}
			idx, seen = k, true
		}
	}
	return idx, seen
}

func hexVal(ch byte) (uint32, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return uint32(ch - '0'), true
	case ch >= 'a' && ch <= 'f':
		return uint32(ch-'a') + 10, true
	case ch >= 'A' && ch <= 'F':
		return uint32(ch-'A') + 10, true
	}
	return 0, false
}

func putLE(dst []byte, v uint32, w int) {
	if w == 2 {
		binary.LittleEndian.PutUint16(dst, uint16(v))
	} else {
		binary.LittleEndian.PutUint32(dst, v)
	}
}

func getLE(src []byte, w int) uint32 {
	if w == 2 {
		return uint32(binary.LittleEndian.Uint16(src))
	}
	return binary.LittleEndian.Uint32(src)
}
