package quality

import "fmt"

// Image 是一帧待分析的灰度原图（可选的外部 --in 走这里）。
type Image struct {
	Label string
	W, H  int
	Pix   []uint8
}

// ThresholdRun 是“一个阈值 × 五档尺寸”的完整结果。
type ThresholdRun struct {
	Threshold    int           `json:"threshold"`
	PlanShift    int           `json:"planShift"`
	PlanBitDepth int           `json:"planBitDepth"`
	PlanBound    int           `json:"planBound"`
	Tiers        []*TierReport `json:"tiers"`
}

// RunThresholdOn 对给定的若干画幅各跑一次同一阈值分析。
func RunThresholdOn(images []Image, tau int) (*ThresholdRun, error) {
	if tau < 0 || tau > MaxThreshold {
		return nil, fmt.Errorf("threshold=%d 超出 [0,%d]", tau, MaxThreshold)
	}
	ps := ShiftForThreshold(tau)
	run := &ThresholdRun{
		Threshold:    tau,
		PlanShift:    ps,
		PlanBitDepth: BitDepth(ps),
		PlanBound:    ErrorBound(ps),
	}
	for i, im := range images {
		label := im.Label
		if label == "" {
			label = fmt.Sprintf("档%d", i+1)
		}
		tr, err := AnalyzeTier(im.W, im.H, im.Pix, tau, label)
		if err != nil {
			return nil, err
		}
		run.Tiers = append(run.Tiers, tr)
	}
	return run, nil
}

// CanonicalImages 生成五档规范样本。
func CanonicalImages(sizes [][2]int) []Image {
	if sizes == nil {
		sizes = DefaultSizes
	}
	imgs := make([]Image, 0, len(sizes))
	for i, sz := range sizes {
		w, h := sz[0], sz[1]
		imgs = append(imgs, Image{
			Label: fmt.Sprintf("档%d", i+1),
			W:     w, H: h,
			Pix: Pattern(w, h),
		})
	}
	return imgs
}

// RunCanonical 用规范五档样本在多个阈值下各跑一遍。
func RunCanonical(thresholds []int) (*BenchReport, error) {
	imgs := CanonicalImages(DefaultSizes)
	rep := &BenchReport{Pattern: PatternName}
	for _, sz := range DefaultSizes {
		rep.Sizes = append(rep.Sizes, [2]int{sz[0], sz[1]})
	}
	for _, tau := range thresholds {
		run, err := RunThresholdOn(imgs, tau)
		if err != nil {
			return nil, err
		}
		rep.Runs = append(rep.Runs, run)
	}
	if len(rep.Runs) == 2 {
		rep.Consistency = CompareRuns(rep.Runs[0], rep.Runs[1])
	}
	return rep, nil
}

// BenchReport 是多阈值运行 + 一致性比对的总报告。
type BenchReport struct {
	Pattern     string          `json:"pattern"`
	Sizes       [][2]int        `json:"sizes"`
	Runs        []*ThresholdRun `json:"runs"`
	Consistency *Consistency    `json:"consistency,omitempty"`
}

// ConsistencyItem 是单个画幅在两次阈值运行中的结论对照。
type ConsistencyItem struct {
	Label       string `json:"label"`
	Dims        string `json:"dims"`
	FormatA     string `json:"formatA"`
	BytesA      int    `json:"bytesA"`
	FormatB     string `json:"formatB"`
	BytesB      int    `json:"bytesB"`
	FormatAgree bool   `json:"formatAgree"`
	PairAgree   bool   `json:"pairAgree"` // 格式与体积成对相同
}

// Consistency 是两次运行的逐档一致性结论。
type Consistency struct {
	ThresholdA int               `json:"thresholdA"`
	ThresholdB int               `json:"thresholdB"`
	Items      []ConsistencyItem `json:"items"`
	// FormatsAgree：每档选中格式都相同（体积可以随阈值不同而不同）。
	FormatsAgree bool `json:"formatsAgree"`
	// PairsAgree：每档“选中格式 + 体积”对都完全相同（同一离散量化档时成立）。
	PairsAgree bool `json:"pairsAgree"`
}

// CompareRuns 按档位标签/画幅对齐两次运行，逐档比较结论。
func CompareRuns(a, b *ThresholdRun) *Consistency {
	c := &Consistency{ThresholdA: a.Threshold, ThresholdB: b.Threshold, FormatsAgree: true, PairsAgree: true}
	for _, ta := range a.Tiers {
		tb := findTier(b, ta.Label, ta.W, ta.H)
		if tb == nil {
			c.FormatsAgree = false
			c.PairsAgree = false
			continue
		}
		it := ConsistencyItem{
			Label: ta.Label, Dims: ta.Dims(),
			FormatA: ta.SelectedFormat, BytesA: ta.SelectedBytes,
			FormatB: tb.SelectedFormat, BytesB: tb.SelectedBytes,
		}
		it.FormatAgree = it.FormatA == it.FormatB
		it.PairAgree = it.FormatAgree && it.BytesA == it.BytesB
		if !it.FormatAgree {
			c.FormatsAgree = false
		}
		if !it.PairAgree {
			c.PairsAgree = false
		}
		c.Items = append(c.Items, it)
	}
	return c
}

func findTier(r *ThresholdRun, label string, w, h int) *TierReport {
	for _, t := range r.Tiers {
		if t.Label == label || (t.W == w && t.H == h) {
			return t
		}
	}
	return nil
}
