package target

import "testing"

// stepMeasure 用已知的严格单调阶梯体积 S(Q)=1000*d(Q) 充当真实编码测量，
// 便于精确断言二分解与最接近兜底。
func stepMeasure() Measure {
	return func(q int) (int, error) {
		return 1000 * DepthForQuality(q), nil
	}
}

func TestDepthForQualityMonotonicAndEndpoints(t *testing.T) {
	if DepthForQuality(1) != 1 {
		t.Fatalf("Q=1 应映射 d=1，实际 %d", DepthForQuality(1))
	}
	if DepthForQuality(100) != 8 {
		t.Fatalf("Q=100 应映射 d=8，实际 %d", DepthForQuality(100))
	}
	prev := 0
	for q := MinQuality; q <= MaxQuality; q++ {
		d := DepthForQuality(q)
		if d < 1 || d > 8 || d < prev {
			t.Fatalf("位深映射非法或不单调: Q=%d d=%d prev=%d", q, d, prev)
		}
		prev = d
	}
	// 越界夹取。
	if DepthForQuality(-5) != 1 || DepthForQuality(999) != 8 {
		t.Fatal("越界 Q 应被夹到端点")
	}
}

func TestSolveReachableExactPoint(t *testing.T) {
	// d=4 的体积恰为 4000；d(Q)=ceil(8Q/100)=4 的最小 Q 是 38。
	sol, err := Solve(4000, 4000, stepMeasure())
	if err != nil {
		t.Fatal(err)
	}
	if !sol.InRange || sol.Quality != 38 || sol.Bytes != 4000 {
		t.Fatalf("精确点应在 Q=38 命中 4000: %+v", sol)
	}
	if sol.GapBytes != 0 || sol.Side != "inside" {
		t.Fatalf("命中时间距应为 0/inside: gap=%d side=%s", sol.GapBytes, sol.Side)
	}
	if len(sol.Iterations) > 8 {
		t.Fatalf("二分 100 档应在 <=7 次收敛，实际试探 %d 次", len(sol.Iterations))
	}
}

func TestSolveFallbackAboveMax(t *testing.T) {
	// 区间 [4500,4700] 落在 d4=4000 与 d5=5000 的空档；
	// d5=5000 距上界 300，比 d4=4000 距下界 500 更近 => 取 d5/Q=51。
	sol, err := Solve(4500, 4700, stepMeasure())
	if err != nil {
		t.Fatal(err)
	}
	if sol.InRange {
		t.Fatal("该区间应不可达")
	}
	if sol.ClosestQuality != 51 || sol.ClosestBytes != 5000 {
		t.Fatalf("最接近解应为 Q=51/5000，实际 Q=%d B=%d", sol.ClosestQuality, sol.ClosestBytes)
	}
	if sol.GapBytes != 300 || sol.Side != "above_max" {
		t.Fatalf("差距应为 300/above_max，实际 %d/%s", sol.GapBytes, sol.Side)
	}
	if sol.Quality != sol.ClosestQuality || sol.Bytes != sol.ClosestBytes {
		t.Fatal("未命中时采用参数应等于最接近参数")
	}
}

func TestSolveFallbackBelowMin(t *testing.T) {
	// 最大体积 8000，目标区间 [90000,100000]：全部偏小。
	sol, err := Solve(90000, 100000, stepMeasure())
	if err != nil {
		t.Fatal(err)
	}
	if sol.InRange || sol.BinaryQuality != -1 {
		t.Fatalf("应不存在 lower_bound: in=%v bq=%d", sol.InRange, sol.BinaryQuality)
	}
	// d=8 平台从 Q=88 开始；平局取达到该体积的最小 Q（与 lower_bound 一致）。
	if sol.ClosestQuality != 88 || sol.ClosestBytes != 8000 {
		t.Fatalf("应取最高体积平台的最小 Q=88，实际 Q=%d B=%d", sol.ClosestQuality, sol.ClosestBytes)
	}
	if sol.GapBytes != 82000 || sol.Side != "below_min" {
		t.Fatalf("差距应为 82000/below_min，实际 %d/%s", sol.GapBytes, sol.Side)
	}
}

func TestSolveFallbackAboveMaxAllTooBig(t *testing.T) {
	// 最小体积 1000，目标区间 [100,500]：全部偏大，取最小 Q=1。
	sol, err := Solve(100, 500, stepMeasure())
	if err != nil {
		t.Fatal(err)
	}
	if sol.InRange {
		t.Fatal("应不可达")
	}
	if sol.ClosestQuality != 1 || sol.ClosestBytes != 1000 {
		t.Fatalf("应取最低质量端，实际 Q=%d B=%d", sol.ClosestQuality, sol.ClosestBytes)
	}
	if sol.GapBytes != 500 || sol.Side != "above_max" {
		t.Fatalf("差距应为 500/above_max，实际 %d/%s", sol.GapBytes, sol.Side)
	}
}

func TestSolveInvalidInterval(t *testing.T) {
	if _, err := Solve(0, 100, stepMeasure()); err == nil {
		t.Fatal("零下界应报错")
	}
	if _, err := Solve(500, 400, stepMeasure()); err == nil {
		t.Fatal("上界小于下界应报错")
	}
	if _, err := Solve(100, 200, nil); err == nil {
		t.Fatal("nil measure 应报错")
	}
}

func TestDistanceAndClassify(t *testing.T) {
	if DistanceToRange(50, 100, 200) != 50 {
		t.Fatal("低于下界距离错误")
	}
	if DistanceToRange(250, 100, 200) != 50 {
		t.Fatal("高于上界距离错误")
	}
	if DistanceToRange(150, 100, 200) != 0 {
		t.Fatal("区间内距离应为 0")
	}
	if Classify(50, 100, 200) != "below_min" || Classify(250, 100, 200) != "above_max" || Classify(150, 100, 200) != "inside" {
		t.Fatal("Classify 错误")
	}
}
