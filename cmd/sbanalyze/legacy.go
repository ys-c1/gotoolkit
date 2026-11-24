package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sb "gotoolkit/internal/subband"
)

type legacyRow struct {
	I       int
	Label   string
	N       int
	Bits    int
	Var     float64
	MSE     float64
	Norm    float64
	Suspect bool

	NeighborLabel string
	NeighborBits  int
	NeighborMSE   float64
	NeighborVar   float64
}

// buildLegacyRows 实测旧分配的逐带 MSE，并圈出最符合题面描述的一行：
// 它比某个隔壁带比特少，但 MSE 相对隔壁的比值落在 [0.9,1.6]（只高一点点，
// 甚至更低）。取其中比值最接近 1（且≥1）的一对，保证只标一行。
func buildLegacyRows(st *sb.Setup, bits []int) []legacyRow {
	m := len(st.Bands)
	rows := make([]legacyRow, m)
	mse := make([]float64, m)
	for i := 0; i < m; i++ {
		mse[i] = st.MSEAt(i, bits[i])
		norm := mse[i] / math.Max(st.Stats[i].Var, 1e-12)
		rows[i] = legacyRow{
			I: i, Label: st.Stats[i].Label, N: st.Stats[i].N,
			Bits: bits[i], Var: st.Stats[i].Var, MSE: mse[i], Norm: norm,
		}
	}

	bestI, bestJ := -1, -1
	bestRatio := math.Inf(1)
	for i := 0; i < m; i++ {
		for _, j := range []int{i - 1, i + 1} {
			if j < 0 || j >= m {
				continue
			}
			if bits[j] <= bits[i] { // 隔壁必须分到更多比特
				continue
			}
			r := mse[i] / math.Max(mse[j], 1e-12)
			if r < 0.9 || r > 1.6 {
				continue
			}
			// 偏好“只高一点点”：比值 ≥1 且离 1 最近；否则记录但不优先。
			score := r
			if r >= 1 {
				score = r // 越小越接近 1
			} else {
				score = 2 + (1 - r) // 低于隔壁的情形排在后面
			}
			if score < bestRatio {
				bestRatio = score
				bestI, bestJ = i, j
			}
		}
	}
	if bestI >= 0 {
		r := &rows[bestI]
		r.Suspect = true
		r.NeighborLabel = st.Stats[bestJ].Label
		r.NeighborBits = bits[bestJ]
		r.NeighborMSE = mse[bestJ]
		r.NeighborVar = st.Stats[bestJ].Var
	}
	return rows
}

func writeErrorTable(path string, st *sb.Setup, bits []int, rows []legacyRow) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	total := sb.TotalBits(st.NS, bits)
	fmt.Fprintf(&b, "# 旧版手工比特表 · 逐带实测误差\n\n")
	fmt.Fprintf(&b, "总比特 %d（%.3f bpp，画幅 %d 像素）。MSE 由与正式实验相同的优选端点均匀量化器实测。\n\n",
		total, float64(total)/float64(st.N), st.N)
	b.WriteString("| i | 子带 | 系数数 | 比特/系数 | σ² | 实测 MSE | MSE/σ² |\n")
	b.WriteString("|--:|---|--:|--:|--:|--:|--:|\n")
	for _, r := range rows {
		flag := ""
		if r.Suspect {
			flag = " ⚠️"
		}
		fmt.Fprintf(&b, "| %d | %s%s | %d | %d | %.1f | %.1f | %.4f |\n",
			r.I, r.Label, flag, r.N, r.Bits, r.Var, r.MSE, r.Norm)
	}
	for _, r := range rows {
		if r.Suspect {
			fmt.Fprintf(&b, "\n**可疑行 %s ⚠️**：%s 只分 %d bit，隔壁 %s 分 %d bit（更多），"+
				"但两者实测 MSE 为 %.1f vs %.1f，%s仅高 %.0f%%。"+
				"两带 σ² 分别为 %.1f 与 %.1f（相差 %.1f 倍）——低方差带用少量比特就够，"+
				"这行不是测量错误，而是“按方差分配”的直接证据。\n",
				r.Label, r.Label, r.Bits, r.NeighborLabel, r.NeighborBits,
				r.MSE, r.NeighborMSE, r.Label, 100*(r.MSE/r.NeighborMSE-1),
				r.Var, r.NeighborVar, r.NeighborVar/math.Max(r.Var, 1e-12))
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeErrorCSV(path string, st *sb.Setup, bits []int, rows []legacyRow) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"i", "band", "n", "bits", "variance", "mse", "mse_over_var", "suspect"})
	for _, r := range rows {
		_ = w.Write([]string{
			strconv.Itoa(r.I), r.Label, strconv.Itoa(r.N), strconv.Itoa(r.Bits),
			fmt.Sprintf("%.6f", r.Var), fmt.Sprintf("%.6f", r.MSE),
			fmt.Sprintf("%.6f", r.Norm), strconv.FormatBool(r.Suspect),
		})
	}
	return w.Error()
}
