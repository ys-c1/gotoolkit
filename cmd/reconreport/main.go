// Command reconreport 解约束最小二乘五抽头重建核，并在同一组彩色图上与双线性路径比色差。
//
//	go run ./cmd/reconreport                 # 求解 + 生成 artifacts/recon/report.md
//	go run ./cmd/reconreport -method bilinear -in a.ppm -out b.ppm
//	go run ./cmd/reconreport -method clsk     -in a.ppm -out b.ppm
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/internal/recon"
)

func main() {
	method := flag.String("method", "", "单图切换: bilinear | cosited | clsk（需配合 -in/-out）")
	in := flag.String("in", "", "输入 P6 PPM")
	out := flag.String("out", "", "输出 P6 PPM")
	dir := flag.String("dir", "artifacts/recon", "报告与预览输出目录")
	flag.Parse()

	if *in != "" {
		if err := runSingle(*method, *in, *out); err != nil {
			fmt.Fprintln(os.Stderr, "失败:", err)
			os.Exit(1)
		}
		return
	}
	if err := runReport(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "报告失败:", err)
		os.Exit(1)
	}
}

func runSingle(method, in, out string) error {
	if method == "" {
		return fmt.Errorf("单图模式必须给 -method bilinear|cosited|clsk")
	}
	src, err := recon.ReadPPM(in)
	if err != nil {
		return err
	}
	// 与基准一致：先得到柔化观测，再走所选重建路径。
	observed := recon.Soften(src)
	var h [recon.TapCount]float64
	if method == string(recon.MethodCLSK) {
		imgs := recon.Suite()
		A, y := recon.BuildRegression(imgs)
		sol, err := recon.SolveConstrainedLS(A, y)
		if err != nil {
			return err
		}
		h = sol.H
	}
	got, err := recon.Reconstruct(recon.Method(method), observed, h)
	if err != nil {
		return err
	}
	if out == "" {
		out = "recon_out.ppm"
	}
	if err := recon.WritePPM(out, got); err != nil {
		return err
	}
	fmt.Printf("method=%s %dx%d -> %s\n", method, got.W, got.H, out)
	return nil
}

// Baseline 是写入 artifacts 的机器可读结果。
type Baseline struct {
	Taps        [5]float64 `json:"taps"`
	BilMean     float64    `json:"bilinear_lab_mean"`
	BilMax      float64    `json:"bilinear_lab_max"`
	ClskMean    float64    `json:"clsk_lab_mean"`
	ClskMax     float64    `json:"clsk_lab_max"`
	BilRGBMean  float64    `json:"bilinear_rgb_mean"`
	BilRGBMax   float64    `json:"bilinear_rgb_max"`
	ClskRGBMean float64    `json:"clsk_rgb_mean"`
	ClskRGBMax  float64    `json:"clsk_rgb_max"`
}

func runReport(dir string) error {
	rep, err := recon.Run()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// 预览第一张图的三种结果，便于人工核对“同一组图”。
	imgs := recon.Suite()
	s0 := recon.Soften(imgs[0])
	_ = recon.WritePPM(filepath.Join(dir, "00_original.ppm"), imgs[0])
	_ = recon.WritePPM(filepath.Join(dir, "00_observed_soft.ppm"), s0)
	_ = recon.WritePPM(filepath.Join(dir, "00_bilinear.ppm"), recon.BilinearRoundTrip(s0))
	_ = recon.WritePPM(filepath.Join(dir, "00_clsk.ppm"), recon.ApplyFive(s0, rep.Solved.H))

	b := Baseline{
		Taps:        rep.Solved.H,
		BilMean:     rep.BilLab.Mean,
		BilMax:      rep.BilLab.Max,
		ClskMean:    rep.ClskLab.Mean,
		ClskMax:     rep.ClskLab.Max,
		BilRGBMean:  rep.BilRGB.Mean,
		BilRGBMax:   rep.BilRGB.Max,
		ClskRGBMean: rep.ClskRGB.Mean,
		ClskRGBMax:  rep.ClskRGB.Max,
	}
	jb, _ := json.MarshalIndent(b, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "numbers.json"), jb, 0o644); err != nil {
		return err
	}

	eq := struct {
		AtA                 [5][5]float64 `json:"AtA"`
		Aty                 [5]float64    `json:"Aty"`
		C                   [3][5]float64 `json:"C"`
		D                   [3]float64    `json:"d"`
		KKT                 [8][8]float64 `json:"kkt"`
		RHS                 [8]float64    `json:"rhs"`
		H                   [5]float64    `json:"h"`
		Mu                  [3]float64    `json:"mu"`
		ConstraintResiduals [3]float64    `json:"constraint_residuals"`
		KKTResidual         float64       `json:"kkt_residual"`
	}{
		AtA:                 rep.Solved.AtA,
		Aty:                 rep.Solved.Aty,
		C:                   rep.Solved.C,
		D:                   rep.Solved.D,
		KKT:                 rep.Solved.KKT,
		RHS:                 rep.Solved.RHS,
		H:                   rep.Solved.H,
		Mu:                  rep.Solved.Mu,
		ConstraintResiduals: rep.Solved.ConstraintResiduals(),
		KKTResidual:         rep.Solved.KKTResidual(),
	}
	ej, _ := json.MarshalIndent(eq, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "equations.json"), ej, 0o644); err != nil {
		return err
	}

	md := recon.RenderMarkdown(rep)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(md), 0o644); err != nil {
		return err
	}
	fmt.Print(md)
	fmt.Printf("\n[written] %s/report.md, numbers.json, equations.json, *.ppm\n", dir)
	return nil
}
