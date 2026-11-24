package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gotoolkit/api"
)

type rangeFlag []api.ByteRange

func (r *rangeFlag) String() string {
	parts := make([]string, 0, len(*r))
	for _, v := range *r {
		parts = append(parts, fmt.Sprintf("%d:%d", v.Start, v.End))
	}
	return strings.Join(parts, ",")
}

func (r *rangeFlag) Set(s string) error {
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		sep := strings.IndexAny(item, ":-")
		if sep < 0 {
			return fmt.Errorf("区间 %q 应为 start:end", item)
		}
		start, err := strconv.Atoi(item[:sep])
		if err != nil {
			return fmt.Errorf("区间起点非法 %q: %w", item, err)
		}
		end, err := strconv.Atoi(item[sep+1:])
		if err != nil {
			return fmt.Errorf("区间终点非法 %q: %w", item, err)
		}
		if start < 0 || end <= start {
			return fmt.Errorf("区间非法 %q", item)
		}
		*r = append(*r, api.ByteRange{Start: start, End: end})
	}
	return nil
}

func runManifest(args []string) int {
	fs := flag.NewFlagSet("manifest", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输入格式标签（不改变摘要计算）")
	in := fs.String("in", "", "待保护文件")
	out := fs.String("out", "", "输出 JSON 清单")
	blockSize := fs.Int("block-size", api.DefaultIntegrityBlockSize, "分块字节数")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	m, err := api.BuildIntegrityManifest(raw, *format, *blockSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成清单失败:", err)
		return 1
	}
	body, err := m.Marshal()
	if err != nil {
		fmt.Fprintln(os.Stderr, "序列化清单失败:", err)
		return 1
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	fmt.Printf("manifest %s: %d bytes, %d blocks of <=%d bytes\n", *out, m.FileSize, len(m.Blocks), *blockSize)
	return 0
}

type recoverReport struct {
	Input           string          `json:"input"`
	Format          string          `json:"format"`
	DetectedRanges  []api.ByteRange `json:"detected_ranges"`
	Width           int             `json:"width"`
	Height          int             `json:"height"`
	GoodPixels      int             `json:"good_pixels"`
	MarkerPixels    int             `json:"marker_pixels"`
	MarkerSentinel  int16           `json:"marker_sentinel"`
	GoodPixelsMatch *bool           `json:"good_pixels_match_original,omitempty"`
	FirstMismatch   int             `json:"first_mismatch,omitempty"`
}

func runRecover(args []string) int {
	fs := flag.NewFlagSet("recover", flag.ContinueOnError)
	format := fs.String("format", "pgm", "损坏文件格式；当前 salvage 仅支持 pgm")
	in := fs.String("in", "", "损坏的输入 PGM")
	out := fs.String("out", "", "主输出：.png/.ppm 为品红标记彩图，.pgm 为灰度兜底图")
	manifestPath := fs.String("manifest", "", "可信 SHA-256 分块清单 JSON")
	ranges := rangeFlag{}
	fs.Var(&ranges, "range", "已知坏段 start:end，可逗号分隔或重复；无清单时使用")
	scale := fs.Int("scale", 6, "PNG 最近邻放大倍数")
	ppmOut := fs.String("ppm-out", "", "额外输出 unscaled P6 PPM")
	grayOut := fs.String("gray-pgm-out", "", "额外输出值 255 标记的灰度 PGM")
	maskOut := fs.String("mask-pgm-out", "", "额外输出坏像素掩码 PGM")
	original := fs.String("original", "", "可选原图 PGM，用于核验未损坏像素")
	reportOut := fs.String("report", "", "可选 JSON 报告路径")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	if *format != "pgm" {
		fmt.Fprintln(os.Stderr, "当前可定位像素映射的 salvage 仅支持 --format pgm")
		return 2
	}

	raw, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	bad := []api.ByteRange(ranges)
	if *manifestPath != "" {
		manifestRaw, err := os.ReadFile(*manifestPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取清单失败:", err)
			return 1
		}
		m, err := api.LoadIntegrityManifest(manifestRaw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "解析清单失败:", err)
			return 1
		}
		detected, err := m.Detect(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, "检测失败:", err)
			return 1
		}
		bad = append(bad, detected...)
	}
	bad = api.MergeRanges(bad)

	rec, err := api.RecoverPGM(raw, bad)
	if err != nil {
		fmt.Fprintln(os.Stderr, "恢复失败:", err)
		return 1
	}

	if err := writeRecoverPrimary(*out, rec, *scale); err != nil {
		fmt.Fprintln(os.Stderr, "写出主图失败:", err)
		return 1
	}
	if *ppmOut != "" {
		body, err := api.RenderMarkedPPM(rec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(*ppmOut, body, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "写出 PPM 失败:", err)
			return 1
		}
	}
	if *grayOut != "" {
		body, err := api.RenderGrayMarkerPGM(rec, api.FallbackGrayMarker)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(*grayOut, body, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "写出灰度 PGM 失败:", err)
			return 1
		}
	}
	if *maskOut != "" {
		body, err := api.RenderMaskPGM(rec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(*maskOut, body, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "写出掩码失败:", err)
			return 1
		}
	}

	rep := recoverReport{
		Input:          *in,
		Format:         *format,
		DetectedRanges: bad,
		Width:          rec.W,
		Height:         rec.H,
		GoodPixels:     rec.GoodPixels,
		MarkerPixels:   rec.MarkerPixels,
		MarkerSentinel: api.RecoveryMarker,
		FirstMismatch:  -1,
	}
	if *original != "" {
		origRaw, err := os.ReadFile(*original)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读取原图失败:", err)
			return 1
		}
		orig, err := api.Decode(origRaw, api.Options{Format: "pgm"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "解码原图失败:", err)
			return 1
		}
		ok, at := rec.CompareGoodPixels(orig)
		rep.GoodPixelsMatch = &ok
		rep.FirstMismatch = at
		if !ok {
			fmt.Fprintf(os.Stderr, "未损坏像素核验失败，首个失配线性下标=%d\n", at)
			return 1
		}
	}
	if *reportOut != "" {
		body, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "报告序列化失败:", err)
			return 1
		}
		if err := os.WriteFile(*reportOut, append(body, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "报告写出失败:", err)
			return 1
		}
	}

	fmt.Printf("recovered %dx%d: %d good pixels, %d marker pixels, ranges=%v -> %s\n",
		rec.W, rec.H, rec.GoodPixels, rec.MarkerPixels, bad, *out)
	return 0
}

func writeRecoverPrimary(path string, rec *api.RecoveredImage, scale int) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		body, err := api.RenderMarkedPNG(rec, scale)
		if err != nil {
			return err
		}
		return os.WriteFile(path, body, 0o644)
	case ".ppm":
		body, err := api.RenderMarkedPPM(rec)
		if err != nil {
			return err
		}
		return os.WriteFile(path, body, 0o644)
	case ".pgm":
		body, err := api.RenderGrayMarkerPGM(rec, api.FallbackGrayMarker)
		if err != nil {
			return err
		}
		return os.WriteFile(path, body, 0o644)
	default:
		return fmt.Errorf("不支持的输出扩展名 %q（请用 .png/.ppm/.pgm）", filepath.Ext(path))
	}
}
