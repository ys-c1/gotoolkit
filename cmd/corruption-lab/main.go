// Command corruption-lab 生成“连续坏段 + 检测 + 可视化恢复”的完整实验产物。
//
// 它只使用 api 包暴露的能力，输出默认放在 artifacts/corruption-lab。
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gotoolkit/api"
)

const (
	imgW = 96
	imgH = 72

	// 该偏移是 32 的倍数，且位于 PGM 像素载荷中。三档长度也都是 32 的倍数，
	// 因而 SHA-256 块清单检出的块区间与注入坏段逐字节重合。
	injectStart = 2048
	blockSize   = 32
	pngScale    = 6
)

type tier struct {
	Name   string
	Length int
}

var tiers = []tier{
	{Name: "tier1-32B", Length: 32},
	{Name: "tier2-256B", Length: 256},
	{Name: "tier3-1024B", Length: 1024},
}

type CaseReport struct {
	Tier                    string           `json:"tier"`
	BadRange                api.ByteRange    `json:"bad_range_file_bytes"`
	BadLengthBytes          int              `json:"bad_length_bytes"`
	Overwrite               string           `json:"overwrite"`
	DetectedRanges          []api.ByteRange  `json:"detected_ranges"`
	DetectionExact          bool             `json:"detection_exact"`
	BadPixelRanges          []api.PixelRange `json:"bad_pixel_ranges"`
	TotalFileBytes          int              `json:"total_file_bytes"`
	TheoryGoodBytes         int              `json:"theory_good_bytes"`
	TheoryRecoveryRatio     float64          `json:"theory_recovery_ratio"`
	MeasuredChangedBytes    int              `json:"measured_changed_bytes_vs_original"`
	MeasuredGoodBytes       int              `json:"measured_good_bytes"`
	MeasuredRecoveryRatio   float64          `json:"measured_recovery_ratio"`
	RatioDifference         float64          `json:"ratio_difference"`
	PixelTotal              int              `json:"pixel_total"`
	GoodPixels              int              `json:"good_pixels"`
	MarkerPixels            int              `json:"marker_pixels"`
	PixelRecoveryRatio      float64          `json:"pixel_recovery_ratio_payload_only"`
	GoodPixelsIdentical     bool             `json:"good_pixels_identical_to_original"`
	FirstPixelMismatch      int              `json:"first_pixel_mismatch"`
	MarkedGrayFallbackValue byte             `json:"marked_gray_fallback_value"`
	Files                   CaseFiles        `json:"files"`
}

type CaseFiles struct {
	CorruptedPGM  string `json:"corrupted_pgm"`
	RecoveredPPM  string `json:"recovered_ppm"`
	RecoveredPNG  string `json:"recovered_png"`
	MarkedGrayPGM string `json:"marked_gray_pgm"`
	BadMaskPGM    string `json:"bad_mask_pgm"`
}

type Report struct {
	GeneratedAt        string       `json:"generated_at"`
	Format             string       `json:"format"`
	Width              int          `json:"width"`
	Height             int          `json:"height"`
	MarkerSentinel     int16        `json:"marker_sentinel"`
	MarkerRGB          [3]int       `json:"marker_rgb"`
	GrayFallbackMarker byte         `json:"gray_fallback_marker"`
	BlockSize          int          `json:"integrity_block_size_bytes"`
	SourcePGM          string       `json:"source_pgm"`
	ManifestJSON       string       `json:"manifest_json"`
	IntactDecodedPGM   string       `json:"intact_decoded_pgm"`
	IntactPNG          string       `json:"intact_png"`
	IntactOutputSame   bool         `json:"intact_output_byte_identical"`
	SourceMaxGray      int          `json:"source_max_gray"`
	Cases              []CaseReport `json:"cases"`
}

func main() {
	out := flag.String("out", "artifacts/corruption-lab", "实验产物输出目录")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "corruption-lab 失败:", err)
		os.Exit(1)
	}
}

func run(outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	original := makeSourceImage()
	source, err := api.Encode(original, api.Options{Format: "pgm"})
	if err != nil {
		return err
	}
	if injectStart+tiers[len(tiers)-1].Length > len(source) {
		return fmt.Errorf("注入区间越界: file=%d end=%d", len(source), injectStart+tiers[len(tiers)-1].Length)
	}

	manifest, err := api.BuildIntegrityManifest(source, "pgm", blockSize)
	if err != nil {
		return err
	}
	manifestJSON, err := manifest.Marshal()
	if err != nil {
		return err
	}

	// 完好文件走原有 Decode/Encode 路径，验证默认输出不变。
	decodedIntact, err := api.Decode(source, api.Options{Format: "pgm"})
	if err != nil {
		return err
	}
	intactPGM, err := api.Encode(decodedIntact, api.Options{Format: "pgm"})
	if err != nil {
		return err
	}
	intactSame := bytes.Equal(source, intactPGM)
	intactRecovered, err := api.RecoverPGM(source, nil)
	if err != nil {
		return err
	}
	intactPNG, err := api.RenderMarkedPNG(intactRecovered, pngScale)
	if err != nil {
		return err
	}

	rep := Report{
		GeneratedAt:        time.Now().Format(time.RFC3339),
		Format:             "pgm (P5, maxval=255, 8-bit gray, one byte per pixel)",
		Width:              imgW,
		Height:             imgH,
		MarkerSentinel:     api.RecoveryMarker,
		MarkerRGB:          [3]int{255, 0, 255},
		GrayFallbackMarker: api.FallbackGrayMarker,
		BlockSize:          blockSize,
		SourcePGM:          "source.pgm",
		ManifestJSON:       "integrity-manifest.json",
		IntactDecodedPGM:   "intact.decoded.pgm",
		IntactPNG:          "intact.png",
		IntactOutputSame:   intactSame,
		SourceMaxGray:      maxGray(original.Pix),
	}
	if err := writeFile(filepath.Join(outDir, rep.SourcePGM), source); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(outDir, rep.ManifestJSON), manifestJSON); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(outDir, rep.IntactDecodedPGM), intactPGM); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(outDir, rep.IntactPNG), intactPNG); err != nil {
		return err
	}

	for _, tc := range tiers {
		cr, err := runCase(outDir, source, original, manifest, tc)
		if err != nil {
			return fmt.Errorf("%s: %w", tc.Name, err)
		}
		rep.Cases = append(rep.Cases, cr)
	}

	reportJSON, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(outDir, "report.json"), append(reportJSON, '\n')); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(outDir, "ratios.csv"), []byte(buildCSV(rep.Cases))); err != nil {
		return err
	}
	markdown := []byte(buildMarkdown(rep))
	if err := writeFile(filepath.Join(outDir, "README.md"), markdown); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(outDir, "REPORT.md"), markdown); err != nil {
		return err
	}

	fmt.Printf("corruption-lab OK -> %s (%d cases)\n", outDir, len(rep.Cases))
	for _, c := range rep.Cases {
		fmt.Printf("%s: range=[%d,%d) L=%d theory=%.6f%% measured=%.6f%% goodPixelsIdentical=%v\n",
			c.Tier, c.BadRange.Start, c.BadRange.End, c.BadLengthBytes,
			c.TheoryRecoveryRatio*100, c.MeasuredRecoveryRatio*100, c.GoodPixelsIdentical)
	}
	return nil
}

func runCase(outDir string, source []byte, original *api.Image, manifest *api.IntegrityManifest, tc tier) (CaseReport, error) {
	dir := filepath.Join(outDir, tc.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CaseReport{}, err
	}
	badRange := api.ByteRange{Start: injectStart, End: injectStart + tc.Length}
	corrupt := append([]byte(nil), source...)
	for i := badRange.Start; i < badRange.End; i++ {
		corrupt[i] = ^source[i] // 逐位取反，保证该段每个字节都实际改变。
	}

	detected, err := manifest.Detect(corrupt)
	if err != nil {
		return CaseReport{}, err
	}
	exactRanges, err := api.DiffByteRanges(source, corrupt)
	if err != nil {
		return CaseReport{}, err
	}
	detectionExact := rangesEqual(detected, exactRanges) && len(exactRanges) == 1 && exactRanges[0] == badRange

	recovered, err := api.RecoverPGM(corrupt, detected)
	if err != nil {
		return CaseReport{}, err
	}
	identical, firstMismatch := recovered.CompareGoodPixels(original)

	ppm, err := api.RenderMarkedPPM(recovered)
	if err != nil {
		return CaseReport{}, err
	}
	pngBytes, err := api.RenderMarkedPNG(recovered, pngScale)
	if err != nil {
		return CaseReport{}, err
	}
	grayPGM, err := api.RenderGrayMarkerPGM(recovered, api.FallbackGrayMarker)
	if err != nil {
		return CaseReport{}, err
	}
	maskPGM, err := api.RenderMaskPGM(recovered)
	if err != nil {
		return CaseReport{}, err
	}

	files := CaseFiles{
		CorruptedPGM:  filepath.ToSlash(filepath.Join(tc.Name, "corrupted.pgm")),
		RecoveredPPM:  filepath.ToSlash(filepath.Join(tc.Name, "recovered.ppm")),
		RecoveredPNG:  filepath.ToSlash(filepath.Join(tc.Name, "recovered.png")),
		MarkedGrayPGM: filepath.ToSlash(filepath.Join(tc.Name, "marked-gray-255.pgm")),
		BadMaskPGM:    filepath.ToSlash(filepath.Join(tc.Name, "bad-mask.pgm")),
	}
	if err := writeFile(filepath.Join(outDir, files.CorruptedPGM), corrupt); err != nil {
		return CaseReport{}, err
	}
	if err := writeFile(filepath.Join(outDir, files.RecoveredPPM), ppm); err != nil {
		return CaseReport{}, err
	}
	if err := writeFile(filepath.Join(outDir, files.RecoveredPNG), pngBytes); err != nil {
		return CaseReport{}, err
	}
	if err := writeFile(filepath.Join(outDir, files.MarkedGrayPGM), grayPGM); err != nil {
		return CaseReport{}, err
	}
	if err := writeFile(filepath.Join(outDir, files.BadMaskPGM), maskPGM); err != nil {
		return CaseReport{}, err
	}

	changed := 0
	for i := range source {
		if source[i] != corrupt[i] {
			changed++
		}
	}
	theory, measured, theoryGood, measuredGood := api.ByteRecoveryRatios(len(source), tc.Length, changed)
	pixelTotal := imgW * imgH
	return CaseReport{
		Tier:                    tc.Name,
		BadRange:                badRange,
		BadLengthBytes:          tc.Length,
		Overwrite:               "每个坏段字节写为原字节的按位补码 (corrupt[i] = ^original[i])",
		DetectedRanges:          detected,
		DetectionExact:          detectionExact,
		BadPixelRanges:          recovered.PixelRanges,
		TotalFileBytes:          len(source),
		TheoryGoodBytes:         theoryGood,
		TheoryRecoveryRatio:     theory,
		MeasuredChangedBytes:    changed,
		MeasuredGoodBytes:       measuredGood,
		MeasuredRecoveryRatio:   measured,
		RatioDifference:         measured - theory,
		PixelTotal:              pixelTotal,
		GoodPixels:              recovered.GoodPixels,
		MarkerPixels:            recovered.MarkerPixels,
		PixelRecoveryRatio:      float64(recovered.GoodPixels) / float64(pixelTotal),
		GoodPixelsIdentical:     identical,
		FirstPixelMismatch:      firstMismatch,
		MarkedGrayFallbackValue: api.FallbackGrayMarker,
		Files:                   files,
	}, nil
}

func makeSourceImage() *api.Image {
	pix := make([]uint8, imgW*imgH)
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			// 渐变 + 块状结构，值域刻意限制在 0..254，给兜底灰标记 255 留空。
			v := (x*2 + y*3 + ((x/8)%2)*28 + ((y/8)%2)*42 + (x*y)%11) % 255
			pix[y*imgW+x] = byte(v)
		}
	}
	return &api.Image{W: imgW, H: imgH, Pix: pix}
}

func maxGray(p []uint8) int {
	m := 0
	for _, v := range p {
		if int(v) > m {
			m = int(v)
		}
	}
	return m
}

func rangesEqual(a, b []api.ByteRange) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func buildCSV(cases []CaseReport) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{
		"tier", "bad_start", "bad_end", "bad_length_bytes", "total_bytes",
		"theory_good_bytes", "theory_ratio", "measured_changed_bytes",
		"measured_good_bytes", "measured_ratio", "difference",
		"good_pixels_identical", "pixel_recovery_ratio",
	})
	for _, c := range cases {
		_ = w.Write([]string{
			c.Tier,
			fmt.Sprint(c.BadRange.Start), fmt.Sprint(c.BadRange.End), fmt.Sprint(c.BadLengthBytes),
			fmt.Sprint(c.TotalFileBytes), fmt.Sprint(c.TheoryGoodBytes),
			fmt.Sprintf("%.10f", c.TheoryRecoveryRatio),
			fmt.Sprint(c.MeasuredChangedBytes), fmt.Sprint(c.MeasuredGoodBytes),
			fmt.Sprintf("%.10f", c.MeasuredRecoveryRatio),
			fmt.Sprintf("%.3e", c.RatioDifference),
			fmt.Sprint(c.GoodPixelsIdentical),
			fmt.Sprintf("%.10f", c.PixelRecoveryRatio),
		})
	}
	w.Flush()
	return buf.String()
}

func buildMarkdown(r Report) string {
	var b strings.Builder
	b.WriteString("# 连续坏段检测与恢复实验\n\n")
	b.WriteString("这个目录由 `go run ./cmd/corruption-lab` 重新生成。实验选择 **PGM P5 / maxval=255**：\n")
	b.WriteString("它是无压缩灰度流，载荷中恰好 1 字节对应 1 像素，因此“文件字节恢复比例”和像素映射都可直接核验。\n\n")

	b.WriteString("## 检测模型与标记值\n\n")
	b.WriteString(fmt.Sprintf("- 原图：`%s`，%d×%d；PGM 文件共 **%d 字节**。\n", r.SourcePGM, r.Width, r.Height, r.Cases[0].TotalFileBytes))
	b.WriteString(fmt.Sprintf("- 可信旁路清单：`%s`，按 **%d 字节**分块记录 SHA-256。清单必须来自未损坏/可信通道；没有清单、原图或其他冗余时，PGM 中被改写的合法灰度字节无法被唯一检出。\n", r.ManifestJSON, r.BlockSize))
	b.WriteString(fmt.Sprintf("- 逻辑标记值：**`api.RecoveryMarker = %d`**。合法灰度域是 0..255，所以 %d 在灰度范围之外。\n", r.MarkerSentinel, r.MarkerSentinel))
	b.WriteString(fmt.Sprintf("- 彩色可视化：标记渲染为 **RGB(%d,%d,%d) / #FF00FF（品红）**；R 与 B 为 255、G 为 0，三通道不等，因此不是任何灰度值。PNG 为 %d× 最近邻放大。\n", r.MarkerRGB[0], r.MarkerRGB[1], r.MarkerRGB[2], pngScale))
	b.WriteString(fmt.Sprintf("- 若只能输出 8 位灰度 PGM，兜底标记为 **%d**。它落在灰度范围内；本实验原图最大灰度为 %d，故不碰撞。通用文件若本身含 %d，不能只看该数值，必须同时使用 `bad-mask.pgm` 或 `report.json` 的区间/掩码区分。\n\n", r.GrayFallbackMarker, r.SourceMaxGray, r.GrayFallbackMarker))

	b.WriteString("## 完好文件\n\n")
	b.WriteString(fmt.Sprintf("- 原编码经现有 `api.Decode` + `api.Encode` 后与源文件逐字节相同：**%v**。\n", r.IntactOutputSame))
	b.WriteString("- 默认编解码和 `testdata/golden/` 均未修改；完整性清单是旁路可选能力。\n")
	b.WriteString("- 完好文件的恢复图中标记像素数为 0，输出图保持纯灰度：\n\n")
	b.WriteString(fmt.Sprintf("![intact](%s)\n\n", r.IntactPNG))

	b.WriteString("## 坏段位置、长度与恢复比例\n\n")
	b.WriteString("所有案例都是独立文件，每个文件只注入一个连续坏段；位置均为文件字节区间 ")
	b.WriteString(fmt.Sprintf("`[%d, ...)`，注入方式为整段按位取反。恢复比例按**整个 PGM 文件的字节数**计算：\n\n", injectStart))
	b.WriteString("```text\n")
	b.WriteString("理论可恢复比例 = (文件总字节 N − 坏段长度 L) / N\n")
	b.WriteString("实测可恢复比例 = (N − 与原图相比实际改变的字节数 C) / N\n")
	b.WriteString("```\n\n")
	b.WriteString("| 档位 | 坏段文件区间 | 坏段长度 L (B) | N (B) | 理论可恢复 | 实测可恢复 | 差值 | 未损坏像素与原图一致 | 像素口径恢复率 |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|:---:|---:|\n")
	for _, c := range r.Cases {
		diffPP := c.RatioDifference * 100
		fmt.Fprintf(&b, "| %s | `[%d,%d)` | %d | %d | %.6f%% (%d B) | %.6f%% (%d/%d B) | %+.6f 个百分点 | %v (首个失配=%d) | %.4f%% (%d/%d) |\n",
			c.Tier, c.BadRange.Start, c.BadRange.End, c.BadLengthBytes, c.TotalFileBytes,
			c.TheoryRecoveryRatio*100, c.TheoryGoodBytes,
			c.MeasuredRecoveryRatio*100, c.MeasuredGoodBytes, c.TotalFileBytes,
			diffPP, c.GoodPixelsIdentical, c.FirstPixelMismatch,
			c.PixelRecoveryRatio*100, c.GoodPixels, c.PixelTotal)
	}
	b.WriteString("\n因为注入时对每个坏字节按位取反，实际改变字节数 C=L，所以字节口径的实测值与理论值并列且相等；差值为 0。\n")
	b.WriteString("像素口径 `good_pixels / W·H` 的分母不含 PGM 头部，因此与文件字节口径有 13 字节头部带来的微小差异，仅作补充。\n\n")

	b.WriteString("## 可视化产物\n\n")
	for _, c := range r.Cases {
		b.WriteString(fmt.Sprintf("### %s：文件区间 `[%d,%d)`，长度 %d B\n\n", c.Tier, c.BadRange.Start, c.BadRange.End, c.BadLengthBytes))
		b.WriteString(fmt.Sprintf("- SHA-256 块清单检出区间：`%s`；与注入区间精确重合：**%v**。\n", rangesString(c.DetectedRanges), c.DetectionExact))
		b.WriteString(fmt.Sprintf("- PGM 像素载荷从文件偏移 13 开始；坏段映射到线性像素区间：`%s`（坐标为 `(x列,y行)`，右端点为排他边界）。\n", pixelRangesString(c.BadPixelRanges)))
		b.WriteString(fmt.Sprintf("- 标记像素：%d；正常恢复像素：%d。\n", c.MarkerPixels, c.GoodPixels))
		b.WriteString(fmt.Sprintf("- 彩色标记图：[`%s`](%s)（另有 unscaled [`%s`](%s)）\n", c.Files.RecoveredPNG, c.Files.RecoveredPNG, c.Files.RecoveredPPM, c.Files.RecoveredPPM))
		b.WriteString(fmt.Sprintf("- 8-bit 灰度兜底：[`%s`](%s)（值 %d）；区分用掩码：[`%s`](%s)\n\n", c.Files.MarkedGrayPGM, c.Files.MarkedGrayPGM, r.GrayFallbackMarker, c.Files.BadMaskPGM, c.Files.BadMaskPGM))
		b.WriteString(fmt.Sprintf("![%s recovered](%s)\n\n", c.Tier, c.Files.RecoveredPNG))
	}

	b.WriteString("## 复现与自动核验\n\n")
	b.WriteString("```bash\n")
	b.WriteString("go test ./...\n")
	b.WriteString("go run ./cmd/corruption-lab --out artifacts/corruption-lab\n")
	b.WriteString("```\n\n")
	b.WriteString("机器可读结果见 `report.json` 与 `ratios.csv`。\n")
	return b.String()
}

func rangesString(rs []api.ByteRange) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("[%d,%d)", r.Start, r.End))
	}
	return strings.Join(parts, ", ")
}

func pixelRangesString(rs []api.PixelRange) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf(
			"linear[%d,%d), (x=%d,y=%d)->(x=%d,y=%d), %d px",
			r.Start, r.End, r.StartX, r.StartY, r.EndX, r.EndY, r.PixelCount,
		))
	}
	return strings.Join(parts, "; ")
}
