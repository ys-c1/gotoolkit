// Command jointopt 复现“抖动强度 s × 量化步长 Δ”的联合误差求解。
//
// 运行：
//
//	go run ./cmd/jointopt
//
// 程序会生成确定性测试图，用同一幅图实测 J=4E_low+E_high；一维扫描只用于
// 构造“分开调”基线和定位展开点。联合最优点由三乘三探针对称差分得到梯度与
// Hessian，再按 z*=-H^{-1}g 解析解出，并用 HΔΔ>0、det H>0 判定严格极小。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"gotoolkit/internal/codec"
)

const (
	w, h   = 256, 256
	lambda = 4.0
)

type Point struct {
	K     int     `json:"k"`
	Delta float64 `json:"delta"`
	S     float64 `json:"s"`
	J     float64 `json:"j"`
}

type Surface struct {
	K                      int           `json:"k"`
	Center                 Point         `json:"center"`
	HDelta                 float64       `json:"h_delta"`
	HS                     float64       `json:"h_s"`
	Values                 [3][3]float64 `json:"values"`
	TopGap                 float64       `json:"top_gap"`
	BottomGap              float64       `json:"bottom_gap"`
	LeftGap                float64       `json:"left_gap"`
	RightGap               float64       `json:"right_gap"`
	MinNeighborGap         float64       `json:"min_neighbor_gap"`
	Gradient               [2]float64    `json:"gradient"`
	Hessian                [2][2]float64 `json:"hessian"`
	Det                    float64       `json:"det"`
	PositiveDefinite       bool          `json:"positive_definite"`
	AnalyticDelta          float64       `json:"analytic_delta"`
	AnalyticS              float64       `json:"analytic_s"`
	AnalyticJModel         float64       `json:"analytic_j_model"`
	DistanceOriginCenter   float64       `json:"distance_origin_center"`
	DistanceOriginAnalytic float64       `json:"distance_origin_analytic"`
}

type Baseline struct {
	DeltaOnly Point `json:"delta_only"`
	SOnly     Point `json:"s_only"`
	Stacked   Point `json:"stacked"`
	Canonical Point `json:"canonical_then_s"`
}

type Result struct {
	Fixture             string           `json:"fixture"`
	Lambda              float64          `json:"lambda"`
	Baselines           map[int]Baseline `json:"baselines"`
	Joint               map[int]Point    `json:"joint"`
	Surfaces            map[int]Surface  `json:"surfaces"`
	ShiftFrom           int              `json:"shift_from"`
	ShiftTo             int              `json:"shift_to"`
	ComparisonGap       float64          `json:"comparison_gap_stacked_minus_joint"`
	RequestedSurfaceGap float64          `json:"requested_surface_gap_min_of_four"`
	RequestedDistance   float64          `json:"requested_shifted_distance_from_origin"`
}

func makeFixture() []uint8 {
	pix := make([]uint8, w*h)
	seed := uint32(20261125)
	rnd := func() float64 {
		seed = seed*1103515245 + 12345
		return float64(int32(seed>>1)) / float64(1<<30)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx := float64(x) / (w - 1)
			fy := float64(y) / (h - 1)
			v := fx*255 + 12*math.Sin(2*math.Pi*fy) + 6*rnd()
			pix[y*w+x] = uint8(math.Max(0, math.Min(255, math.Round(v))))
		}
	}
	return pix
}

var src = makeFixture()

func metric(out []uint8) (band, grain, j float64) {
	e := make([]float64, len(src))
	for i := range src {
		e[i] = float64(out[i]) - float64(src[i])
	}
	low := make([]float64, len(e))
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			sum := 0.0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					sum += e[(y+dy)*w+x+dx]
				}
			}
			low[i] = sum / 9
		}
	}

	var sb, sg float64
	n := 0
	for y := 2; y < h-2; y++ {
		for x := 2; x < w-2; x++ {
			i := y*w + x
			l := low[i]
			high := e[i] - l
			sb += l * l
			sg += high * high
			n++
		}
	}
	band = sb / float64(n)
	grain = sg / float64(n)
	j = lambda*band + grain
	return
}

func eval(K int, delta, s float64) float64 {
	out := codec.Quantize(w, h, src, K, delta, s)
	_, _, j := metric(out)
	return j
}

func optimizeJoint(K int) Point {
	d0 := 256.0 / float64(K)
	best := Point{K: K, Delta: d0, S: 0.5, J: math.Inf(1)}

	// 粗定位：Δ 步长 0.2，s 步长 0.02。它只负责找到二次展开点。
	for di := -32; di <= 10; di++ {
		d := d0 + 0.2*float64(di)
		for si := 0; si <= 50; si++ {
			s := 0.02 * float64(si)
			j := eval(K, d, s)
			if j < best.J {
				best = Point{K, d, s, j}
			}
		}
	}

	// 局部细格仅用于取离散实现中心点；解析顶点随后由 Hessian 公式给出。
	baseD, baseS := best.Delta, best.S
	fine := best
	for di := -10; di <= 10; di++ {
		d := math.Round((baseD+0.02*float64(di))*1000) / 1000
		for si := -15; si <= 15; si++ {
			s := math.Round((baseS+0.002*float64(si))*1000) / 1000
			if s < 0 || s > 1 {
				continue
			}
			j := eval(K, d, s)
			if j < fine.J {
				fine = Point{K, d, s, j}
			}
		}
	}
	return fine
}

func separateBaseline(K int) Baseline {
	d0 := 256.0 / float64(K)

	dOnly := Point{K: K, Delta: d0, S: 0, J: math.Inf(1)}
	for di := -60; di <= 8; di++ {
		d := d0 + 0.1*float64(di)
		j := eval(K, d, 0)
		if j < dOnly.J {
			dOnly = Point{K, d, 0, j}
		}
	}

	sOnly := Point{K: K, Delta: d0, S: 0, J: math.Inf(1)}
	for i := 0; i <= 100; i++ {
		s := 0.01 * float64(i)
		j := eval(K, d0, s)
		if j < sOnly.J {
			sOnly = Point{K, d0, s, j}
		}
	}

	stackD := dOnly.Delta
	stackS := sOnly.S
	stacked := Point{K, stackD, stackS, eval(K, stackD, stackS)}
	canonical := Point{K, d0, stackS, eval(K, d0, stackS)}
	return Baseline{DeltaOnly: dOnly, SOnly: sOnly, Stacked: stacked, Canonical: canonical}
}

func makeSurface(p Point, hd, hs float64) Surface {
	K := p.K
	var v [3][3]float64
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			v[i+1][j+1] = eval(K, p.Delta+float64(i)*hd, p.S+float64(j)*hs)
		}
	}

	gD := (v[2][1] - v[0][1]) / (2 * hd)
	gS := (v[1][2] - v[1][0]) / (2 * hs)
	hDD := (v[2][1] - 2*v[1][1] + v[0][1]) / (hd * hd)
	hSS := (v[1][2] - 2*v[1][1] + v[1][0]) / (hs * hs)
	hDS := (v[2][2] - v[2][0] - v[0][2] + v[0][0]) / (4 * hd * hs)
	det := hDD*hSS - hDS*hDS

	dDelta := math.NaN()
	dS := math.NaN()
	modelJ := math.NaN()
	if math.Abs(det) > 1e-12 {
		// H [dDelta,dS]^T = -g
		dDelta = (-gD*hSS + gS*hDS) / det
		dS = (gD*hDS - gS*hDD) / det
		modelJ = p.J + 0.5*(gD*dDelta+gS*dS)
	}

	top := v[0][1] - p.J
	bottom := v[2][1] - p.J
	left := v[1][0] - p.J
	right := v[1][2] - p.J
	minGap := math.Min(math.Min(top, bottom), math.Min(left, right))

	y := p.Delta / 256.0
	dCenter := math.Hypot(p.S, y)
	dAnalytic := math.Hypot(p.S+dS, (p.Delta+dDelta)/256.0)

	return Surface{
		K: K, Center: p, HDelta: hd, HS: hs, Values: v,
		TopGap: top, BottomGap: bottom, LeftGap: left, RightGap: right,
		MinNeighborGap: minGap,
		Gradient:       [2]float64{gD, gS},
		Hessian:        [2][2]float64{{hDD, hDS}, {hDS, hSS}},
		Det:            det, PositiveDefinite: hDD > 0 && det > 0,
		AnalyticDelta: p.Delta + dDelta, AnalyticS: p.S + dS,
		AnalyticJModel:       modelJ,
		DistanceOriginCenter: dCenter, DistanceOriginAnalytic: dAnalytic,
	}
}

func main() {
	outPath := flag.String("out", "artifacts/joint_objective.json", "JSON 结果输出路径")
	flag.Parse()

	ks := []int{4, 8, 16}
	gaps := map[int][2]float64{
		4:  {0.10, 0.01},
		8:  {0.10, 0.01},
		16: {0.10, 0.05},
	}

	res := Result{
		Fixture:   "deterministic 256x256 gradient + sinusoid + LCG noise seed=20261125",
		Lambda:    lambda,
		Baselines: map[int]Baseline{},
		Joint:     map[int]Point{},
		Surfaces:  map[int]Surface{},
		ShiftFrom: 8,
		ShiftTo:   16,
	}

	for _, K := range ks {
		res.Baselines[K] = separateBaseline(K)
		p := optimizeJoint(K)
		res.Joint[K] = p
		sf := makeSurface(p, gaps[K][0], gaps[K][1])
		res.Surfaces[K] = sf

		fmt.Printf("K=%2d separate stacked Δ=%.2f s=%.2f J=%.4f | joint Δ=%.3f s=%.3f J=%.4f gap=%.4f\n",
			K, res.Baselines[K].Stacked.Delta, res.Baselines[K].Stacked.S,
			res.Baselines[K].Stacked.J, p.Delta, p.S, p.J,
			res.Baselines[K].Stacked.J-p.J)
		fmt.Printf("     analytic Δ=%.4f s=%.4f Hdd=%.3f Hss=%.3f Hds=%.3f det=%.3g PD=%v min4gap=%.6f rhoCenter=%.6f rhoAnalytic=%.6f\n",
			sf.AnalyticDelta, sf.AnalyticS, sf.Hessian[0][0], sf.Hessian[1][1],
			sf.Hessian[0][1], sf.Det, sf.PositiveDefinite, sf.MinNeighborGap,
			sf.DistanceOriginCenter, sf.DistanceOriginAnalytic)
	}

	res.ComparisonGap = res.Baselines[8].Stacked.J - res.Joint[8].J
	res.RequestedSurfaceGap = res.Surfaces[8].MinNeighborGap
	// 换档 K=8 -> K=16 后，用实测三乘三中心格（可实现的离散最优点）
	// 计算到归一化原点 (s, Δ/256) 的距离。
	res.RequestedDistance = res.Surfaces[16].DistanceOriginCenter

	if *outPath != "" {
		if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(*outPath, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("wrote", *outPath)
	}
}
