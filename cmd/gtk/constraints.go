package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gotoolkit/api"
)

// runConstraints 打印三档块长的约束可行性、冲突块与两条代价。
// 用法：gtk constraints [--fixture gradient|quadratic|texture] [--size 64]
func runConstraints(args []string) int {
	fs := flag.NewFlagSet("constraints", flag.ContinueOnError)
	fixture := fs.String("fixture", string(api.FixtureGradient), "实验图：gradient|quadratic|texture")
	size := fs.Int("size", 64, "画幅边长（正方形）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rep, err := api.RunConstraintStudy(*fixture, *size, *size)
	if err != nil {
		fmt.Fprintln(os.Stderr, "实验失败:", err)
		return 1
	}
	printStudyReport(rep)
	return 0
}

func printStudyReport(rep *api.StudyReport) {
	fmt.Printf("约束系统：ρ=%d（直流偏差上限）, τ=%d（跨边界台阶上限）\n", rep.Rho, rep.Tau)
	fmt.Printf("块间边缘强度阈值 θ = 2ρ+τ = %d；角阈值 2ρ+2τ = %d\n", 2*rep.Rho+rep.Tau, 2*rep.Rho+2*rep.Tau)
	fmt.Printf("测试图 %s，%dx%d，三档块长均整除画幅\n\n", rep.Fixture, rep.W, rep.H)
	fmt.Println("充要条件：① 边相邻 |μa−μb| ≤ 2ρ+τ   ② 角对角 |μa−μb| ≤ 2ρ+2τ")
	fmt.Println()

	for _, t := range rep.Tiers {
		status := "可行"
		if !t.Feasible {
			status = "不可行"
		}
		fmt.Printf("B=%d  [%s]  edgeMax=%d(θ=%d) cornerMax=%d  edgeMean=%.3f\n",
			t.BlockSize, status, t.EdgeMax, t.EdgeThreshold, t.CornerMax, t.EdgeMean)
		if len(t.ConflictBlocks) > 0 {
			fmt.Printf("     冲突块 %d 个（边缘强度>θ 的边的端点），冲突块坐标：%s\n",
				len(t.ConflictBlocks), fmtBlocks(t.ConflictBlocks, 8))
		}
		if t.Feasible {
			fmt.Printf("     保持连续：约束内重建 seam max=%d mean=%.3f，直流偏差 max=%d mean=%.3f，MSE=%.3f\n",
				t.ConSeamMax, t.ConSeamMean, t.ConDCDevMax, t.ConDCDevMean, t.ConMSE)
		} else {
			fmt.Printf("     处置：标记为不可行；默认出图选代价②（放弃直流预算、守住连续，MSE 更低），代价①并列对照；不等式①超出量=%d\n", t.Costs.Shortfall)
			// 两条约束的代价，分两行摆开。
			fmt.Printf("       代价① 放弃「边界连续」：残留最大缝宽 = %d 灰度级（超出 τ=%d 的部分为 %d；实测 seam max=%d mean=%.3f，MSE=%.3f）\n",
				t.Costs.GiveUpSeamMax, rep.Tau, t.Costs.GiveUpSeamMax-rep.Tau, t.KeepDCSeamMax, t.KeepDCSeamMean, t.KeepDCMSE)
			fmt.Printf("       代价② 放弃「直流保真」、选择守住连续：最小最大直流偏差 = %s 灰度级（半级下界；连续侧实测 seam max=%d mean=%.3f，整数 DC max=%d mean=%.3f，MSE=%.3f）\n",
				t.Costs.GiveUpDCText, t.KeepSeamSeamMax, t.KeepSeamSeamMean, t.KeepSeamDCDevMax, t.KeepSeamDCDevMean, t.KeepSeamMSE)
		}
		fmt.Println()
	}
}

func fmtBlocks(bs [][2]int, limit int) string {
	var parts []string
	for i, b := range bs {
		if i >= limit {
			parts = append(parts, fmt.Sprintf("…共%d个", len(bs)))
			break
		}
		parts = append(parts, fmt.Sprintf("(%d,%d)", b[0], b[1]))
	}
	return strings.Join(parts, " ")
}
