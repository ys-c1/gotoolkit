package guard

import (
	"fmt"
	"io"
	"strings"
)

// WriteReport 把字节结论与像素结论写进同一份输出。
// 输出是确定性的：同样的两侧字节必然得到同样的文本。
func WriteReport(w io.Writer, rep *Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "基准目录 %s（只读）\n", rep.GoldenDir)
	fmt.Fprintf(&b, "参照目录 %s（只读）\n", rep.RefDir)
	if rep.OutDir != "" {
		fmt.Fprintf(&b, "输出目录 %s\n", rep.OutDir)
	}
	for _, r := range rep.Results {
		b.WriteString("\n== " + r.Format + " ==\n")
		for _, p := range r.Pairs {
			fmt.Fprintf(&b, "[%s] %s vs %s\n", p.Name, p.Expected, p.Actual)
			writeBytes(&b, p.Bytes)
			writeDump(&b, p.Dump)
		}
	}
	b.WriteString("\n")
	if len(rep.Drift) == 0 {
		fmt.Fprintf(&b, "结论: 基准与实现均逐字节一致，两侧解码图一致\n")
	} else {
		fmt.Fprintf(&b, "结论: 漂移 [%s]；基准与参照只读，只能改实现\n", strings.Join(rep.Drift, ", "))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeBytes(b *strings.Builder, v BytesVerdict) {
	if v.Equal {
		fmt.Fprintf(b, "字节: 一致 (%d 字节)\n", v.GoldenLen)
		return
	}
	fmt.Fprintf(b, "字节: 不一致 期望=%d 实际=%d 首个差异 @ %d (0x%x)\n",
		v.GoldenLen, v.FreshLen, v.FirstDiff, v.FirstDiff)
	lo := max(0, v.FirstDiff-8)
	fmt.Fprintf(b, "  期望 @%d: %s\n", lo, hexWindow(v.Context.Golden, v.FirstDiff-lo))
	fmt.Fprintf(b, "  实际 @%d: %s\n", lo, hexWindow(v.Context.Fresh, v.FirstDiff-lo))
	switch {
	case v.FirstDiff >= v.GoldenLen:
		fmt.Fprintf(b, "  期望侧在该偏移已结束（实际多出 %d 字节）\n", v.FreshLen-v.GoldenLen)
	case v.FirstDiff >= v.FreshLen:
		fmt.Fprintf(b, "  实际侧在该偏移已结束（少了 %d 字节）\n", v.GoldenLen-v.FreshLen)
	default:
		fmt.Fprintf(b, "  期望=0x%02x 实际=0x%02x\n", v.Expected, v.Actual)
	}
}

// hexWindow 渲染一段字节，把 at 处（即首个差异）用 [..] 标出。
func hexWindow(bs []byte, at int) string {
	var parts []string
	for i, c := range bs {
		if i == at {
			parts = append(parts, fmt.Sprintf("[%02x]", c))
		} else {
			parts = append(parts, fmt.Sprintf("%02x", c))
		}
	}
	return strings.Join(parts, " ")
}

func writeDump(b *strings.Builder, d *Dump) {
	if d == nil {
		return
	}
	if d.Diff == nil {
		if d.GoldenErr != nil {
			fmt.Fprintf(b, "像素: 期望侧解码失败: %v\n", d.GoldenErr)
		}
		if d.FreshErr != nil {
			fmt.Fprintf(b, "像素: 实际侧解码失败: %v\n", d.FreshErr)
		}
		return
	}
	writePixel(b, d.Diff)
	if d.SidePath != "" {
		fmt.Fprintf(b, "  并排 %s（左期望 / 右实际，白框=差异像素）\n", d.SidePath)
	}
	if d.DiffPath != "" {
		fmt.Fprintf(b, "  差异 %s\n", d.DiffPath)
	}
	if d.GoldenDecoded != "" {
		fmt.Fprintf(b, "  解码 期望=%s 实际=%s\n", d.GoldenDecoded, d.FreshDecoded)
	}
}

func writePixel(b *strings.Builder, d *PixelDiff) {
	if d.Equal {
		fmt.Fprintf(b, "像素: 一致 %dx%d\n", d.GoldenW, d.GoldenH)
		return
	}
	if d.SizeDiffer {
		fmt.Fprintf(b, "像素: 画幅不同 期望=%dx%d 实际=%dx%d\n", d.GoldenW, d.GoldenH, d.FreshW, d.FreshH)
	}
	if d.Count == 0 {
		fmt.Fprintf(b, "像素: 交集内无差异\n")
		return
	}
	pct := 0.0
	if d.Total > 0 {
		pct = 100 * float64(d.Count) / float64(d.Total)
	}
	fmt.Fprintf(b, "像素: %d/%d 格不同 (%.1f%%) 首个差异 (%d,%d) 期望=%d 实际=%d\n",
		d.Count, d.Total, pct, d.FirstX, d.FirstY, d.FirstGolden, d.FirstRef)
	changes := d.Changes()
	const show = 8
	for i, c := range changes {
		if i == show {
			fmt.Fprintf(b, "  替换: …另 %d 种\n", len(changes)-show)
			break
		}
		fmt.Fprintf(b, "  替换: %d -> %d ×%d\n", c.Golden, c.Ref, c.Count)
	}
}
