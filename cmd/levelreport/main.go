// Command levelreport 复现“任意步长序列 → 单调重建电平”的构造与对照数据：
//
//	go run ./cmd/levelreport            # 打印三档的构造、误差与预测
//	go run ./cmd/levelreport -md        # 额外打印可粘贴的 Markdown 汇总表
//	go run ./cmd/levelreport -json      # 打印结构化结果（供落盘/核验）
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	"gotoolkit/internal/monolevel"
)

// eps 是生成“凑出来的名义表”时逐格补对比度的幅度（以本格步长为单位）。
// 偶数格 +0.42Δ、奇数格 −0.42Δ，相邻格必然交叉，从而名义表非单调。
const eps = 0.42

func main() {
	md := flag.Bool("md", false, "输出 Markdown 汇总表后退出")
	asJSON := flag.Bool("json", false, "输出结构化 JSON 后退出")
	asCSV := flag.Bool("csv", false, "输出逐档 CSV 指标后退出")
	flag.Parse()

	rows := make([]summary, 0, len(monolevel.Tiers))
	for t := range monolevel.Tiers {
		s, err := measure(t)
		if err != nil {
			fail(err)
		}
		rows = append(rows, s)
	}

	switch {
	case *asJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(buildReport(rows)); err != nil {
			fail(err)
		}
	case *asCSV:
		writeCSV(rows)
	case *md:
		printMarkdown(rows)
	default:
		printText(rows)
	}
}

func writeCSV(rows []summary) {
	w := csv.NewWriter(os.Stdout)
	_ = w.Write([]string{
		"group", "tier", "L", "step_sum", "level_pairs", "ramp_pairs",
		"nominal_inv_level", "nominal_inv_ramp",
		"repaired_inv_level", "repaired_inv_ramp",
		"midpoint_inv_level", "midpoint_inv_ramp",
		"J_repaired", "J_midpoint",
		"mse_nominal_test", "mse_repaired_test", "mse_midpoint_test",
		"pred_mse_repaired_train", "pred_mse_midpoint_train",
		"kkt_primal", "kkt_stationarity", "kkt_complementarity", "kkt_min_lambda",
		"active", "inactive",
	})
	for i, s := range rows {
		rec := []string{
			strconv.Itoa(i + 1), s.name, strconv.Itoa(s.levels), strconv.Itoa(s.sum),
			strconv.Itoa(s.levelPairs), strconv.Itoa(s.rampPairs),
			strconv.Itoa(s.nomLevelBad), strconv.Itoa(s.nomRampBad),
			strconv.Itoa(s.repLevelBad), strconv.Itoa(s.repRampBad),
			strconv.Itoa(s.midLevelBad), strconv.Itoa(s.midRampBad),
			fmt.Sprintf("%.4f", s.lsRep), fmt.Sprintf("%.4f", s.lsMid),
			fmt.Sprintf("%.4f", s.mseNom), fmt.Sprintf("%.4f", s.mseRep), fmt.Sprintf("%.4f", s.mseMid),
			fmt.Sprintf("%.4f", s.predRep), fmt.Sprintf("%.4f", s.predMid),
			fmt.Sprintf("%.2e", s.kkt.PrimalViolation), fmt.Sprintf("%.2e", s.kkt.Stationarity),
			fmt.Sprintf("%.2e", s.kkt.Complementarity), fmt.Sprintf("%.2e", s.kkt.MinLambda),
			strconv.Itoa(s.kkt.Active), strconv.Itoa(s.kkt.Inactive),
		}
		if err := w.Write(rec); err != nil {
			fail(err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fail(err)
	}
}

func printMarkdown(rows []summary) {
	fmt.Println("| 档位 | 区间数 L | 步长和 | 名义逆序(电平/映射) | 修复逆序(电平/映射) | J 修复 | J 中点 | 像素MSE 修复 | 像素MSE 中点 | 预测 修复 |")
	fmt.Println("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, s := range rows {
		fmt.Printf("| %s | %d | %d | %d/%d | %d/%d | %.4f | %.4f | %.4f | %.4f | %.4f |\n",
			s.name, s.levels, s.sum, s.nomLevelBad, s.nomRampBad, s.repLevelBad, s.repRampBad,
			s.lsRep, s.lsMid, s.mseRep, s.mseMid, s.predRep)
	}
}

func printText(rows []summary) {
	fmt.Println("# 单调重建电平：两步构造、最小二乘最优性与三档实测")
	fmt.Println()
	fmt.Println("所有数字由本命令实算；`go test ./internal/monolevel` 逐项锁定。")
	fmt.Println()
	for _, s := range rows {
		fmt.Printf("## 数据组 %s（L=%d，步长和 %d）\n\n", s.name, s.levels, s.sum)
		fmt.Printf("步长        %v\n", s.steps)
		fmt.Printf("边界 t      %v\n", s.bounds)
		fmt.Printf("名义表 a    %v\n", s.nominal)
		fmt.Printf("修复后 r    %v\n", s.repaired)
		fmt.Printf("中点表 m    %v\n", s.midpoint)
		fmt.Println()
		fmt.Printf("逐对比较（电平表 %d 对 / 整条映射 %d 对，全量、不抽样）\n", s.levelPairs, s.rampPairs)
		fmt.Printf("  名义表逆序对：电平 %d，映射 %d\n", s.nomLevelBad, s.nomRampBad)
		fmt.Printf("  修复后逆序对：电平 %d，映射 %d\n", s.repLevelBad, s.repRampBad)
		fmt.Printf("  中点表逆序对：电平 %d，映射 %d\n", s.midLevelBad, s.midRampBad)
		fmt.Println()
		fmt.Printf("最小二乘目标 J（对名义表 a）：修复 %.4f，中点 %.4f（修复 ≤ 中点：%v）\n",
			s.lsRep, s.lsMid, s.lsRep <= s.lsMid)
		fmt.Printf("测试集像素 MSE：名义 %.4f，修复 %.4f，中点 %.4f（修复 ≤ 中点：%v）\n",
			s.mseNom, s.mseRep, s.mseMid, s.mseRep <= s.mseMid)
		fmt.Printf("误差预测（训练集统计 → 测试集）：修复 %.4f（实测 %.4f），中点 %.4f（实测 %.4f）\n",
			s.predRep, s.mseRep, s.predMid, s.mseMid)
		fmt.Printf("KKT（作用在实值 r 上）：原始违背 %.2e，稳定性 %.2e，互补松弛 %.2e，λmin %.2e，起作用约束 %d，非起作用 %d\n",
			s.kkt.PrimalViolation, s.kkt.Stationarity, s.kkt.Complementarity, s.kkt.MinLambda, s.kkt.Active, s.kkt.Inactive)
		fmt.Println()
	}
}

type summary struct {
	name                        string
	steps                       []int
	levels, sum                 int
	bounds                      []int
	nominal, repaired, midpoint []int
	rFloat                      []float64
	levelPairs, rampPairs       int
	nomLevelBad, nomRampBad     int
	repLevelBad, repRampBad     int
	midLevelBad, midRampBad     int
	lsRep, lsMid                float64
	mseNom, mseRep, mseMid      float64
	predRep, predMid            float64
	kkt                         monolevel.KKTReport
}

func measure(t int) (summary, error) {
	tier := monolevel.Tiers[t]
	s := summary{name: tier.Name, steps: tier.Steps, levels: len(tier.Steps)}
	for _, d := range tier.Steps {
		s.sum += d
	}

	hist, err := monolevel.TierHistogram(t)
	if err != nil {
		return s, err
	}
	train, test := monolevel.SplitCounts(hist)
	cells, err := monolevel.Stats(tier.Steps, train)
	if err != nil {
		return s, err
	}
	s.bounds = cells.Bounds
	s.nominal = monolevel.NominalLevels(cells, eps)
	a := f64(s.nominal)
	s.rFloat, _, err = monolevel.PAVA(a, cells.Weight)
	if err != nil {
		return s, err
	}
	s.repaired = monolevel.RoundLevels(s.rFloat)
	s.midpoint, err = monolevel.MidpointLevels(tier.Steps)
	if err != nil {
		return s, err
	}

	s.levelPairs, _ = monolevel.AllPairs(s.nominal)
	_, nomBad := monolevel.AllPairs(s.nominal)
	s.nomLevelBad = len(nomBad)
	_, repBad := monolevel.AllPairs(s.repaired)
	s.repLevelBad = len(repBad)
	_, midBad := monolevel.AllPairs(s.midpoint)
	s.midLevelBad = len(midBad)

	s.rampPairs, _, err = monolevel.RampAllPairs(tier.Steps, s.nominal)
	if err != nil {
		return s, err
	}
	_, nomRamp, err := monolevel.RampAllPairs(tier.Steps, s.nominal)
	if err != nil {
		return s, err
	}
	_, repRamp, err := monolevel.RampAllPairs(tier.Steps, s.repaired)
	if err != nil {
		return s, err
	}
	_, midRamp, err := monolevel.RampAllPairs(tier.Steps, s.midpoint)
	if err != nil {
		return s, err
	}
	s.nomRampBad, s.repRampBad, s.midRampBad = len(nomRamp), len(repRamp), len(midRamp)

	s.lsRep, err = monolevel.LSError(a, cells.Weight, f64(s.repaired))
	if err != nil {
		return s, err
	}
	s.lsMid, err = monolevel.LSError(a, cells.Weight, f64(s.midpoint))
	if err != nil {
		return s, err
	}
	s.mseNom, err = monolevel.PixelMSE(test, cells.Bounds, s.nominal)
	if err != nil {
		return s, err
	}
	s.mseRep, err = monolevel.PixelMSE(test, cells.Bounds, s.repaired)
	if err != nil {
		return s, err
	}
	s.mseMid, err = monolevel.PixelMSE(test, cells.Bounds, s.midpoint)
	if err != nil {
		return s, err
	}
	s.predRep, err = monolevel.PredictMSE(cells, s.repaired)
	if err != nil {
		return s, err
	}
	s.predMid, err = monolevel.PredictMSE(cells, s.midpoint)
	if err != nil {
		return s, err
	}
	s.kkt = monolevel.KKT(a, cells.Weight, s.rFloat)
	return s, nil
}

// Report / TierReport 是 -json 的对外结构，只含标准类型。
type Report struct {
	DomainN int          `json:"domain_n"`
	Eps     float64      `json:"contrast_eps"`
	Tiers   []TierReport `json:"tiers"`
}

type TierReport struct {
	Name               string    `json:"name"`
	L                  int       `json:"L"`
	StepSum            int       `json:"step_sum"`
	Steps              []int     `json:"steps"`
	Bounds             []int     `json:"bounds"`
	NominalLevels      []int     `json:"nominal_levels"`
	MonotoneLevels     []int     `json:"monotone_levels"`
	FloatLevels        []float64 `json:"float_levels"`
	MidpointLevels     []int     `json:"midpoint_levels"`
	LevelPairs         int       `json:"level_pairs_compared"`
	RampPairs          int       `json:"ramp_pairs_compared"`
	NominalInversions  [2]int    `json:"nominal_inversions_level_ramp"`
	MonotoneInversions [2]int    `json:"monotone_inversions_level_ramp"`
	MidpointInversions [2]int    `json:"midpoint_inversions_level_ramp"`
	JRepaired          float64   `json:"J_repaired_vs_nominal"`
	JMidpoint          float64   `json:"J_midpoint_vs_nominal"`
	MSESplit           string    `json:"mse_split"`
	MSE                struct {
		Nominal  float64 `json:"nominal_test"`
		Repaired float64 `json:"repaired_test"`
		Midpoint float64 `json:"midpoint_test"`
	} `json:"pixel_mse"`
	PredictedMSERepaired float64 `json:"predicted_mse_repaired_train"`
	PredictedMSEMidpoint float64 `json:"predicted_mse_midpoint_train"`
	KKT                  struct {
		PrimalViolation float64 `json:"primal_violation"`
		Stationarity    float64 `json:"stationarity_residual"`
		Complementarity float64 `json:"complementarity_residual"`
		MinLambda       float64 `json:"min_lambda"`
		Active          int     `json:"active_constraints"`
		Inactive        int     `json:"inactive_constraints"`
	} `json:"kkt"`
}

func buildReport(rows []summary) Report {
	rep := Report{DomainN: monolevel.DomainN, Eps: eps}
	for _, s := range rows {
		t := TierReport{
			Name: s.name, L: s.levels, StepSum: s.sum,
			Steps: s.steps, Bounds: s.bounds,
			NominalLevels: s.nominal, MonotoneLevels: s.repaired,
			FloatLevels: s.rFloat, MidpointLevels: s.midpoint,
			LevelPairs: s.levelPairs, RampPairs: s.rampPairs,
			NominalInversions:  [2]int{s.nomLevelBad, s.nomRampBad},
			MonotoneInversions: [2]int{s.repLevelBad, s.repRampBad},
			MidpointInversions: [2]int{s.midLevelBad, s.midRampBad},
			JRepaired:          s.lsRep, JMidpoint: s.lsMid,
			MSESplit:             "test half of deterministic train/test histogram split",
			PredictedMSERepaired: s.predRep, PredictedMSEMidpoint: s.predMid,
		}
		t.MSE.Nominal, t.MSE.Repaired, t.MSE.Midpoint = s.mseNom, s.mseRep, s.mseMid
		t.KKT.PrimalViolation = s.kkt.PrimalViolation
		t.KKT.Stationarity = s.kkt.Stationarity
		t.KKT.Complementarity = s.kkt.Complementarity
		t.KKT.MinLambda = s.kkt.MinLambda
		t.KKT.Active, t.KKT.Inactive = s.kkt.Active, s.kkt.Inactive
		rep.Tiers = append(rep.Tiers, t)
	}
	return rep
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "levelreport:", err)
	os.Exit(1)
}

func f64(v []int) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		out[i] = float64(v[i])
	}
	return out
}
