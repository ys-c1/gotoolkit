package codec

import (
	"math/rand"
	"testing"
)

// randDCField 造一个随机块直流网格（整块网格，W/H 被 BS 整除）。
func randDCField(rng *rand.Rand, nx, ny, bs int) DCField {
	dc := make([]int, nx*ny)
	for i := range dc {
		dc[i] = rng.Intn(256)
	}
	return DCField{W: nx * bs, H: ny * bs, BS: bs, NX: nx, NY: ny, DC: dc}
}

// TestTwoInequalitiesAreNecessaryAndSufficient 是定理的正面与反面：
// 两不等式成立 ⟺ 差分约束可行（无负环）。整块网格、BS≥2。
func TestTwoInequalitiesAreNecessaryAndSufficient(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 4000; trial++ {
		nx := 2 + rng.Intn(3)
		ny := 2 + rng.Intn(3)
		bs := 2 + rng.Intn(3) // BS≥2：块内至少 2×2，是两不等式成立的充分性前提
		f := randDCField(rng, nx, ny, bs)
		p := ConstraintParams{Rho: rng.Intn(12), Tau: rng.Intn(24)}
		closed := CheckFeasibility(f, p).Feasible
		exact := ExactFeasible(f, p)
		if closed != exact {
			t.Fatalf("两不等式与差分约束不一致: %dx%d bs=%d ρ=%d τ=%d closed=%v exact=%v dc=%v",
				nx, ny, bs, p.Rho, p.Tau, closed, exact, f.DC)
		}
	}
}

// TestTwoInequalitiesNecessity 单独确认每条不等式都必要：
// 把一条边或一条对角线推到阈值之上，可行性必须翻成 false。
func TestTwoInequalitiesNecessity(t *testing.T) {
	p := ConstraintParams{Rho: 4, Tau: 8} // θ边=16，θ角=24
	// 2x2 块网格，bs=4（块内 4×4）。
	base := func() DCField {
		return DCField{W: 8, H: 8, BS: 4, NX: 2, NY: 2, DC: []int{100, 100, 100, 100}}
	}
	if !ExactFeasible(base(), p) {
		t.Fatal("常数直流应可行")
	}
	// ① 边越界：右块比左块高 θ边+1，其余不动。对角随之也高。
	f := base()
	f.DC = []int{100, 100 + p.EdgeThreshold() + 1, 100, 100}
	if CheckFeasibility(f, p).Feasible || ExactFeasible(f, p) {
		t.Fatal("边差 > 2ρ+τ 应不可行")
	}
	// 刚好等于阈值应可行（≤ 是允许的）。
	f = base()
	f.DC = []int{100, 100 + p.EdgeThreshold(), 100, 100}
	if !ExactFeasible(f, p) {
		t.Fatal("边差 = 2ρ+τ 应可行")
	}
	// ② 对角越界但四条边都不越界：2x2，μ=[0, 2ρ+τ, 2ρ+τ, 2ρ+2τ+1]。
	// 边差 = 2ρ+τ（=θ边，恰好不越界），对角差 = 2ρ+2τ+1 > θ角。
	q := ConstraintParams{Rho: 4, Tau: 8}
	f = DCField{W: 8, H: 8, BS: 4, NX: 2, NY: 2, DC: []int{
		0,
		2*q.Rho + q.Tau,
		2*q.Rho + q.Tau,
		2*q.Rho + 2*q.Tau + 1,
	}}
	r := CheckFeasibility(f, q)
	if !r.EdgeOK {
		t.Fatalf("构造错误：四条边差应都 ≤ θ边，实际 EdgeOK=false（EdgeMax=%d）", r.EdgeMax)
	}
	if r.CornerOK {
		t.Fatal("对角差 > 2ρ+2τ 时角约束应被判定为不成立")
	}
	if ExactFeasible(f, q) {
		t.Fatal("对角差 > 2ρ+2τ 应不可行（角约束是独立的必要条件）")
	}
}

// TestFeasibleReconstructionSatisfiesBoth 可行时，约束内重建必须实测满足
// seam≤τ 与 dcdev≤ρ（端到端验证，不只是解析判定）。
func TestFeasibleReconstructionSatisfiesBoth(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for trial := 0; trial < 200; trial++ {
		nx := 2 + rng.Intn(3)
		ny := 2 + rng.Intn(3)
		bs := 2 + rng.Intn(4)
		f := randDCField(rng, nx, ny, bs)
		p := ConstraintParams{Rho: rng.Intn(10) + 2, Tau: rng.Intn(20) + 2}
		if !ExactFeasible(f, p) {
			continue
		}
		out, ok := ReconstructConstrained(f, p)
		if !ok {
			t.Fatal("ExactFeasible 为真但重建失败")
		}
		seam := MeasureSeamStat(f.W, f.H, bs, out)
		dc := MeasureDCDevStat(f, out)
		if seam.Max > p.Tau {
			t.Fatalf("可行重建缝宽 %d > τ=%d (bs=%d ρ=%d)", seam.Max, p.Tau, bs, p.Rho)
		}
		if dc.Max > p.Rho {
			t.Fatalf("可行重建直流偏差 %d > ρ=%d (bs=%d τ=%d)", dc.Max, p.Rho, bs, p.Tau)
		}
	}
}

// TestInfeasibleHasNoSolution 不可行时 ReconstructConstrained 必须拒绝，
// 且两不等式的冲突集非空。
func TestInfeasibleHasNoSolution(t *testing.T) {
	p := ConstraintParams{Rho: 8, Tau: 16}
	f := DCField{W: 8, H: 8, BS: 4, NX: 2, NY: 2, DC: []int{0, 100, 100, 0}}
	if ExactFeasible(f, p) {
		t.Fatal("对角差 0→100 超过 2ρ+2τ=48，应不可行")
	}
	if _, ok := ReconstructConstrained(f, p); ok {
		t.Fatal("不可行时重建不应声称成功")
	}
	r := CheckFeasibility(f, p)
	if r.Feasible || len(r.ConflictEdges) == 0 {
		t.Fatalf("不可行应列出冲突边，实际 feasible=%v conflicts=%d", r.Feasible, len(r.ConflictEdges))
	}
}

// TestConflictBlocksAreEndpoints 冲突块必须是“边缘强度>θ 的边的端点”，且去重。
func TestConflictBlocksAreEndpoints(t *testing.T) {
	p := ConstraintParams{Rho: 2, Tau: 2} // θ=6
	nx, ny, bs := 4, 3, 4
	dc := []int{
		50, 50, 50, 50,
		50, 50, 90, 50, // 块 (2,1) 与左邻 (1,1) 差 40 > 6
		50, 50, 50, 50,
	}
	f := DCField{W: nx * bs, H: ny * bs, BS: bs, NX: nx, NY: ny, DC: dc}
	r := CheckFeasibility(f, p)
	if r.Feasible {
		t.Fatal("应不可行")
	}
	if len(r.ConflictEdges) != 4 { // 中心 (2,1) 与左、右、上、下四个 50 邻块都差 40
		t.Fatalf("冲突边数应为 4，实际 %d", len(r.ConflictEdges))
	}
	want := map[[2]int]bool{
		{1, 1}: true, {2, 1}: true, {3, 1}: true,
		{2, 0}: true, {2, 2}: true,
	}
	if len(r.ConflictBlocks) != 5 {
		t.Fatalf("冲突块应为 5 个（去重），实际 %d: %v", len(r.ConflictBlocks), r.ConflictBlocks)
	}
	for _, b := range r.ConflictBlocks {
		if !want[b] {
			t.Fatalf("意外冲突块 %v", b)
		}
	}
}

// TestThreeTierStudyScenarios 锁死三张内置图在 ρ=8/τ=16 下的三档可行性模式：
// gradient 三档全可行、quadratic 4/8 可行 16 不可行、texture 三档全不可行。
func TestThreeTierStudyScenarios(t *testing.T) {
	cases := []struct {
		name string
		want [3]bool
	}{
		{"gradient", [3]bool{true, true, true}},
		{"quadratic", [3]bool{true, true, false}},
		{"texture", [3]bool{false, false, false}},
	}
	for _, c := range cases {
		s := RunStudy(FixtureName(c.name), 64, 64, StudyParams)
		for i, tr := range s.Tiers {
			if tr.Feasible != c.want[i] {
				t.Fatalf("%s B=%d 可行性=%v，期望 %v", c.name, tr.BlockSize, tr.Feasible, c.want[i])
			}
		}
	}
}

// TestInfeasibleCostsMatchMeasured 解析代价与实测对齐：
//   - 放弃连续性：实测 seam max 等于 SeamGiveUpMax；
//   - 放弃直流：实测 dcdev max 不超过 DCGiveUpHalf 的上取整。
func TestInfeasibleCostsMatchMeasured(t *testing.T) {
	s := RunStudy(FixtureQuadratic, 64, 64, StudyParams)
	var got bool
	for _, tr := range s.Tiers {
		if tr.Feasible {
			continue
		}
		got = true
		if tr.KeepDC.SeamMax != tr.costSeamGiveUp() {
			t.Fatalf("B=%d 放弃连续性：实测 seam max=%d，解析 %d", tr.BlockSize, tr.KeepDC.SeamMax, tr.costSeamGiveUp())
		}
		half := tr.costDCGiveUpHalf()
		if tr.KeepSeam.DCDevMax > (half+1)/2 {
			t.Fatalf("B=%d 放弃直流：实测 dcdev max=%d 超过解析上界 %d", tr.BlockSize, tr.KeepSeam.DCDevMax, (half+1)/2)
		}
	}
	if !got {
		t.Fatal("quadratic 应至少有一档不可行")
	}
}
