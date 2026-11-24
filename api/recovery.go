package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"

	"gotoolkit/internal/codec"
)

// RecoveryMarker 是恢复结果中的“损坏/不可信”哨兵。
//
// PGM P5/maxval=255 的合法灰度域是 [0,255]；256 紧贴其上且不在灰度范围内。
// 因为原始像素类型是 uint8，恢复结果必须使用更宽的整数类型才能保留这个哨兵。
// 可视化时它被渲染成品红 RGB(255,0,255)；三通道不相等，因此也不是任何灰度。
const RecoveryMarker int16 = 256

// FallbackGrayMarker 是只能输出 8 位 PGM 时使用的兜底标记。
// 它本身落在灰度域内，所以不能只凭数值判断：必须同时查看 BadMask PGM
// 或报告中的 BadPixelRanges。本次实验原图还被限制为最大 254，因此 255
// 在实验图中不会与真实像素碰撞。
const FallbackGrayMarker byte = 255

// DefaultIntegrityBlockSize 是完整性清单的默认分块字节数。
const DefaultIntegrityBlockSize = 32

var (
	// ErrIntegrity 表示完整性清单校验失败。
	ErrIntegrity = errors.New("完整性校验失败")
	// ErrRange 表示字节区间非法。
	ErrRange = errors.New("字节区间非法")
)

// ByteRange 是文件中的左闭右开字节区间 [Start,End)。
type ByteRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Length 返回坏段字节数。
func (r ByteRange) Length() int { return r.End - r.Start }

// PixelRange 是线性像素下标的左闭右开区间，并附带便于人读的坐标。
type PixelRange struct {
	Start      int `json:"start"`
	End        int `json:"end"`
	StartX     int `json:"start_x"`
	StartY     int `json:"start_y"`
	EndX       int `json:"end_x_exclusive"`
	EndY       int `json:"end_y_exclusive"`
	PixelCount int `json:"pixel_count"`
}

// IntegrityBlock 是一个定长文件块的 SHA-256 记录。
type IntegrityBlock struct {
	Index  int       `json:"index"`
	Range  ByteRange `json:"range"`
	SHA256 string    `json:"sha256"`
}

// IntegrityManifest 是不改动原文件的旁路完整性清单。
//
// 检测模型要求清单本身来自可信通道（例如与原文件一起签名/分发）。
// PGM 的任意单字节在 maxval=255 下都可能是合法灰度，因此没有清单、
// 原始图或其他冗余信息时，数学上无法区分“被改写的合法灰度”和原值。
type IntegrityManifest struct {
	Format    string           `json:"format"`
	FileSize  int              `json:"file_size"`
	BlockSize int              `json:"block_size"`
	Algorithm string           `json:"algorithm"`
	Blocks    []IntegrityBlock `json:"blocks"`
}

// RecoveredImage 是带损坏哨兵的恢复图。
//
// Pix 的合法灰度仍是 0..255；损坏位置为 RecoveryMarker(256)。
type RecoveredImage struct {
	W, H         int
	Pix          []int16
	FileRanges   []ByteRange
	PixelRanges  []PixelRange
	GoodPixels   int
	MarkerPixels int
}

// BuildIntegrityManifest 为 data 生成分块 SHA-256 清单。blockSize<=0 时使用默认值。
func BuildIntegrityManifest(data []byte, format string, blockSize int) (*IntegrityManifest, error) {
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	if blockSize <= 0 {
		blockSize = DefaultIntegrityBlockSize
	}
	m := &IntegrityManifest{
		Format:    format,
		FileSize:  len(data),
		BlockSize: blockSize,
		Algorithm: "SHA-256",
	}
	for start, idx := 0, 0; start < len(data); start, idx = start+blockSize, idx+1 {
		end := minInt(start+blockSize, len(data))
		sum := sha256.Sum256(data[start:end])
		m.Blocks = append(m.Blocks, IntegrityBlock{
			Index:  idx,
			Range:  ByteRange{Start: start, End: end},
			SHA256: hex.EncodeToString(sum[:]),
		})
	}
	return m, nil
}

// MarshalJSON 保留稳定、可读的 JSON 编码。
func (m *IntegrityManifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// LoadIntegrityManifest 解析清单 JSON。
func LoadIntegrityManifest(raw []byte) (*IntegrityManifest, error) {
	var m IntegrityManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("完整性清单 JSON 非法: %w", err)
	}
	if m.FileSize < 0 || m.BlockSize <= 0 || len(m.Blocks) == 0 {
		return nil, fmt.Errorf("%w: 清单字段非法", ErrIntegrity)
	}
	return &m, nil
}

// Detect 用清单检查 data，合并所有失配块为连续字节区间。
//
// 若坏段恰好覆盖若干完整块，返回区间就是精确坏段；若坏段未按块对齐，
// 返回的是外层失配块区间，分辨率为一个块。
func (m *IntegrityManifest) Detect(data []byte) ([]ByteRange, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: 清单为空", ErrIntegrity)
	}
	if len(data) != m.FileSize {
		return nil, fmt.Errorf("%w: 文件长度由 %d 变为 %d；本实验只处理等长覆盖", ErrIntegrity, m.FileSize, len(data))
	}
	var bad []ByteRange
	for _, b := range m.Blocks {
		if b.Range.Start < 0 || b.Range.End > len(data) || b.Range.Start >= b.Range.End {
			return nil, fmt.Errorf("%w: 块 %d 区间非法", ErrIntegrity, b.Index)
		}
		sum := sha256.Sum256(data[b.Range.Start:b.Range.End])
		if hex.EncodeToString(sum[:]) != b.SHA256 {
			bad = append(bad, b.Range)
		}
	}
	return MergeRanges(bad), nil
}

// DiffByteRanges 是实验用的“真值裁判”：逐字节比较原文件与损坏文件，
// 返回实际值发生变化的连续区间。真实恢复场景没有原文件，应使用清单 Detect。
func DiffByteRanges(want, got []byte) ([]ByteRange, error) {
	if len(want) != len(got) {
		return nil, fmt.Errorf("长度不一致: want=%d got=%d", len(want), len(got))
	}
	var out []ByteRange
	start := -1
	for i := range want {
		if want[i] != got[i] {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			out = append(out, ByteRange{Start: start, End: i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, ByteRange{Start: start, End: len(want)})
	}
	return out, nil
}

// MergeRanges 合并相交或相邻的字节区间。
func MergeRanges(ranges []ByteRange) []ByteRange {
	if len(ranges) == 0 {
		return nil
	}
	rs := append([]ByteRange(nil), ranges...)
	for i := 0; i < len(rs); i++ {
		for j := i + 1; j < len(rs); j++ {
			if rs[j].Start < rs[i].Start {
				rs[i], rs[j] = rs[j], rs[i]
			}
		}
	}
	out := []ByteRange{rs[0]}
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.Start <= last.End {
			if r.End > last.End {
				last.End = r.End
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// RecoverPGM  salvage 一个 P5/maxval=255 PGM。
//
// bad 是已检测出的文件字节区间。区间覆盖的像素被置为 RecoveryMarker；
// 其他像素直接从损坏文件复制，不使用原图修补，因此未损坏像素保持原值。
func RecoverPGM(data []byte, bad []ByteRange) (*RecoveredImage, error) {
	w, h, payloadOffset, pix, err := codec.DecodePGMDetail(data)
	if err != nil {
		return nil, err
	}
	bad = MergeRanges(bad)
	out := &RecoveredImage{
		W:          w,
		H:          h,
		Pix:        make([]int16, w*h),
		FileRanges: bad,
	}
	for i, v := range pix {
		out.Pix[i] = int16(v)
	}
	for _, r := range bad {
		if r.Start < 0 || r.End < r.Start || r.End > len(data) {
			return nil, fmt.Errorf("%w: [%d,%d) 文件长度=%d", ErrRange, r.Start, r.End, len(data))
		}
		// P5/maxval=255 时恰好一个文件字节对应一个线性像素。
		ps := maxInt(0, r.Start-payloadOffset)
		pe := minInt(w*h, r.End-payloadOffset)
		if pe <= ps {
			continue // 坏段落在头部或 PGM 行分隔空白；本函数不把它映射成像素。
		}
		pr := PixelRange{Start: ps, End: pe, PixelCount: pe - ps}
		pr.StartX, pr.StartY = ps%w, ps/w
		pr.EndX, pr.EndY = pe%w, pe/w
		out.PixelRanges = append(out.PixelRanges, pr)
		for p := ps; p < pe; p++ {
			out.Pix[p] = RecoveryMarker
		}
	}
	for _, v := range out.Pix {
		if v == RecoveryMarker {
			out.MarkerPixels++
		} else {
			out.GoodPixels++
		}
	}
	return out, nil
}

// CompareGoodPixels 验证所有未标记位置是否与原图逐像素一致。
// 它不要求损坏位置等于原图；那些位置应保持哨兵。
func (r *RecoveredImage) CompareGoodPixels(original *Image) (bool, int) {
	if r == nil || original == nil || r.W != original.W || r.H != original.H || len(original.Pix) != len(r.Pix) {
		return false, -1
	}
	for i, v := range r.Pix {
		if v == RecoveryMarker {
			continue
		}
		if int(v) != int(original.Pix[i]) {
			return false, i
		}
	}
	return true, -1
}

// PixelMatchOutsideRanges 是不依赖 RecoveredImage 状态的区间核验：
// 所有不在坏像素区间内的像素必须逐字节相同。
func PixelMatchOutsideRanges(original, recovered []uint8, w, h int, bad []PixelRange) (bool, int) {
	if len(original) != w*h || len(recovered) != w*h {
		return false, -1
	}
	badSet := make([]bool, w*h)
	for _, r := range bad {
		for i := r.Start; i < r.End && i < w*h; i++ {
			if i >= 0 {
				badSet[i] = true
			}
		}
	}
	for i := range original {
		if !badSet[i] && original[i] != recovered[i] {
			return false, i
		}
	}
	return true, -1
}

// RenderMarkedPNG 把恢复结果渲染为最近邻放大的 PNG。
// scale=1 表示一个像素对应一个 PNG 像素。
func RenderMarkedPNG(r *RecoveredImage, scale int) ([]byte, error) {
	if r == nil || r.W <= 0 || r.H <= 0 || len(r.Pix) != r.W*r.H {
		return nil, ErrBadSize
	}
	if scale <= 0 {
		scale = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, r.W*scale, r.H*scale))
	for y := 0; y < r.H; y++ {
		for x := 0; x < r.W; x++ {
			v := r.Pix[y*r.W+x]
			c := grayColor(uint8(v))
			if v == RecoveryMarker {
				c = color.RGBA{R: 255, G: 0, B: 255, A: 255} // 品红：不在灰度轴上。
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetRGBA(x*scale+dx, y*scale+dy, c)
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderMarkedPPM 输出 unscaled P6 PPM。普通灰度写成 R=G=B；哨兵写品红。
func RenderMarkedPPM(r *RecoveredImage) ([]byte, error) {
	if r == nil || r.W <= 0 || r.H <= 0 || len(r.Pix) != r.W*r.H {
		return nil, ErrBadSize
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "P6\n%d %d\n255\n", r.W, r.H)
	for _, v := range r.Pix {
		if v == RecoveryMarker {
			buf.WriteByte(255)
			buf.WriteByte(0)
			buf.WriteByte(255)
			continue
		}
		g := byte(v)
		buf.WriteByte(g)
		buf.WriteByte(g)
		buf.WriteByte(g)
	}
	return buf.Bytes(), nil
}

// RenderGrayMarkerPGM 输出只能使用 8 位灰度时的兜底图。
func RenderGrayMarkerPGM(r *RecoveredImage, marker byte) ([]byte, error) {
	if r == nil || r.W <= 0 || r.H <= 0 || len(r.Pix) != r.W*r.H {
		return nil, ErrBadSize
	}
	pix := make([]uint8, len(r.Pix))
	for i, v := range r.Pix {
		if v == RecoveryMarker {
			pix[i] = marker
		} else {
			pix[i] = uint8(v)
		}
	}
	return Encode(&Image{W: r.W, H: r.H, Pix: pix}, Options{Format: "pgm"})
}

// RenderMaskPGM 输出坏像素掩码：坏点白(255)，好点黑(0)。
// 当灰度标记值与原图碰撞时，以这份掩码为准。
func RenderMaskPGM(r *RecoveredImage) ([]byte, error) {
	if r == nil || r.W <= 0 || r.H <= 0 || len(r.Pix) != r.W*r.H {
		return nil, ErrBadSize
	}
	pix := make([]uint8, len(r.Pix))
	for i, v := range r.Pix {
		if v == RecoveryMarker {
			pix[i] = 255
		}
	}
	return Encode(&Image{W: r.W, H: r.H, Pix: pix}, Options{Format: "pgm"})
}

// ByteRecoveryRatios 按文件字节计算理论/实测可恢复比例。
//
// theoryGoodBytes 仅由坏段长度推出：N-L。
// measuredChangedBytes 是与真值逐字节比较后实际变化的字节数；
// 对“整段按位取反”的受控注入，它等于 L。
func ByteRecoveryRatios(totalBytes, badLength, measuredChangedBytes int) (theory, measured float64, theoryGood, measuredGood int) {
	theoryGood = totalBytes - badLength
	measuredGood = totalBytes - measuredChangedBytes
	theory = float64(theoryGood) / float64(totalBytes)
	measured = float64(measuredGood) / float64(totalBytes)
	// 避免展示层出现 -0。
	if math.Abs(theory-measured) < 1e-15 {
		measured = theory
	}
	return
}

func grayColor(v uint8) color.RGBA {
	return color.RGBA{R: v, G: v, B: v, A: 255}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
