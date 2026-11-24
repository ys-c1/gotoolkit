package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"gotoolkit/api"
	"gotoolkit/internal/lowbit"
)

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// compareBytes 比较两份同尺寸的字节流，返回 PSNR 与最大绝对差。
// 若尺寸不同则退回按逐字节比较并把尺寸差计入最大误差。
func compareBytes(a, b []byte) (string, int) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	maxErr := 0
	sq := 0.0
	cnt := 0
	for i := 0; i < n; i++ {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d > maxErr {
			maxErr = d
		}
		sq += float64(d * d)
		cnt++
	}
	if len(a) != len(b) {
		d := len(a) - len(b)
		if d < 0 {
			d = -d
		}
		if d > maxErr {
			maxErr = d
		}
		cnt++
		sq += float64(d * d)
	}
	if cnt == 0 {
		return "inf", 0
	}
	mse := sq / float64(cnt)
	if mse == 0 {
		return "inf", 0
	}
	psnr := 10 * math.Log10(255.0*255.0/mse)
	return fmt.Sprintf("%.2f", psnr), maxErr
}

// dumpText 逐条列出缩略前后的调色板与索引表。
func dumpText(img *api.Image, p lowbit.Params, st *lowbit.State) {
	fmt.Printf("input      %dx%d (%d px)\n", img.W, img.H, len(img.Pix))
	fmt.Printf("bits       %d  target_err %d  q %d  eff_err %d\n", p.Bits, p.TargetErr, p.Q, p.EffErr)
	fmt.Printf("inequality %s\n", lowbit.Inequality(p.TargetErr, p.Bits))
	if st != nil {
		fmt.Printf("kernel     %s (dither=%v)\n", st.Kernel, st.DitherOn)
		fmt.Printf("thumb      %dx%d\n", st.ThumbW, st.ThumbH)
		fmt.Printf("palette_before (%d) = %v\n", len(st.PaletteBefore), st.PaletteBefore)
		fmt.Printf("palette_after  (%d) = %v\n", len(st.PaletteAfter), st.PaletteAfter)
		fmt.Println("index tables:")
		fmt.Printf("  before %dx%d, per-entry bit width %d\n", st.W, st.H, st.IndexBits)
		printIndexTable(st.IndicesBefore, st.W)
		fmt.Printf("  after  %dx%d, per-entry bit width %d\n", st.ThumbW, st.ThumbH, st.IndexBits)
		printIndexTable(st.IndicesAfter, st.ThumbW)
		return
	}
	fmt.Println("低位深未启用（bits=0 或 force=off），无索引表。")
}

func printIndexTable(idx []uint8, w int) {
	if len(idx) == 0 || w <= 0 {
		fmt.Println("    (empty)")
		return
	}
	for i := 0; i < len(idx); i += w {
		end := i + w
		if end > len(idx) {
			end = len(idx)
		}
		parts := make([]string, 0, end-i)
		for _, v := range idx[i:end] {
			parts = append(parts, fmt.Sprintf("%d", v))
		}
		fmt.Printf("    [%s]\n", strings.Join(parts, " "))
	}
}
