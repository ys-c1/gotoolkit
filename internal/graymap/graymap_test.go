package graymap

import "testing"

func TestBuildAllBits(t *testing.T) {
	for _, bits := range []int{Bits8, Bits10, Bits12} {
		lut, rep, err := Build(bits)
		if err != nil {
			t.Fatalf("bits=%d 构建失败: %v", bits, err)
		}
		n := 1 << bits
		if len(lut.Raw) != n || len(lut.Mono) != n || len(lut.Fixed) != n {
			t.Fatalf("bits=%d 长度错误", bits)
		}

		// 原始映射必须能观察到倒挂，否则证明对象不存在
		if rep.InversionCount == 0 {
			t.Fatalf("bits=%d 原始映射未出现倒挂，实验前提不成立", bits)
		}
		if len(rep.Inversions) != rep.InversionCount {
			t.Fatalf("bits=%d 倒挂明细数与计数不一致", bits)
		}
		// 每一处倒挂都必须满足充要条件：相邻理想差 < 1
		for _, iv := range rep.Inversions {
			d := ideal(iv.I1, n) - ideal(iv.I0, n)
			if d >= 1 {
				t.Fatalf("bits=%d 倒挂@(%d,%d) 的理想差 %.4f >= 1，充要条件被破坏",
					bits, iv.I0, iv.I1, d)
			}
		}

		// cummax 与最终映射零倒挂
		if !IsNonDecreasing(lut.Mono) {
			t.Fatalf("bits=%d Mono 仍有倒挂", bits)
		}
		if !IsNonDecreasing(lut.Fixed) {
			t.Fatalf("bits=%d Fixed 仍有倒挂", bits)
		}

		// 端点复核：外层端点在 cummax / 铺开后都不得改变
		if lut.Mono[0] != lut.Raw[0] || lut.Mono[n-1] != lut.Raw[n-1] {
			t.Fatalf("bits=%d cummax 改动了端点: raw=(%d,%d) mono=(%d,%d)",
				bits, lut.Raw[0], lut.Raw[n-1], lut.Mono[0], lut.Mono[n-1])
		}
		if lut.Fixed[0] != 0 || lut.Fixed[n-1] != n-1 {
			t.Fatalf("bits=%d 最终端点错误: (%d,%d)", bits, lut.Fixed[0], lut.Fixed[n-1])
		}
		if rep.FixedEnd0 != rep.RawEnd0 || rep.FixedEndN != rep.RawEndN {
			t.Fatalf("bits=%d 报告端点不一致", bits)
		}

		// 值域合法
		for i, v := range lut.Fixed {
			if v < 0 || v > n-1 {
				t.Fatalf("bits=%d Fixed[%d]=%d 越界", bits, i, v)
			}
		}

		// 平台铺开后，凡是 Spread=true 的平台段内必须严格递增
		for _, p := range rep.Plateaus {
			if !p.Spread {
				continue
			}
			for k := p.Start + 1; k <= p.End; k++ {
				if lut.Fixed[k] <= lut.Fixed[k-1] {
					t.Fatalf("bits=%d 平台[%d,%d] 未严格递增 @%d", bits, p.Start, p.End, k)
				}
			}
			if p.End+1 < n && lut.Fixed[p.End+1] <= lut.Fixed[p.End] {
				t.Fatalf("bits=%d 平台[%d,%d] 撞上右邻（重新产生并列）", bits, p.Start, p.End)
			}
		}

		// cummax 的误差界：逐点抬升不超过 1 LSB
		if rep.MaxDeviationMono > 1 {
			t.Fatalf("bits=%d cummax 偏差 %d 超过 1 LSB 误差界", bits, rep.MaxDeviationMono)
		}
	}
}

func TestRejectsBadBits(t *testing.T) {
	for _, bits := range []int{0, 7, 16, -8} {
		if _, _, err := Build(bits); err == nil {
			t.Fatalf("bits=%d 应被拒绝", bits)
		}
	}
}

func TestCumulativeMaxPreservesEndpoints(t *testing.T) {
	// 合成一个含倒挂的序列，直接核对 cummax 的端点保持性质
	raw := []int{3, 5, 4, 7, 6, 9}
	mono := cumulativeMax(raw)
	want := []int{3, 5, 5, 7, 7, 9}
	for i := range want {
		if mono[i] != want[i] {
			t.Fatalf("cummax[%d]=%d, want %d", i, mono[i], want[i])
		}
	}
	if mono[0] != raw[0] || mono[len(raw)-1] != raw[len(raw)-1] {
		t.Fatal("cummax 改动了端点")
	}
}

func TestSpreadPlateauStrictWhenRoom(t *testing.T) {
	// 有富余码级：平台 [1,3]=4，右邻 9；span=9-1-4=4 >= need=2，应严格铺开且不撞右邻
	mono := []int{0, 4, 4, 4, 9, 10}
	out, ps := spreadPlateaus(mono)
	if !IsNonDecreasing(out) {
		t.Fatal("铺开后出现倒挂")
	}
	plateau := ps[0]
	if !plateau.Spread || plateau.Start != 1 || plateau.End != 3 {
		t.Fatalf("平台记录错误: %+v", plateau)
	}
	want := []int{0, 4, 6, 8, 9, 10}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("铺开结果 %v, want %v", out, want)
		}
	}
	if !(out[1] < out[2] && out[2] < out[3] && out[3] < out[4]) {
		t.Fatalf("平台未严格递增: %v", out)
	}

	// 无富余码级：span=5-1-4=0 < need=2，保留平台并标记不可铺
	mono2 := []int{0, 4, 4, 4, 5, 6}
	out2, ps2 := spreadPlateaus(mono2)
	if ps2[0].Spread {
		t.Fatal("位深不足的平台不应被标记为可铺")
	}
	if out2[1] != 4 || out2[2] != 4 || out2[3] != 4 {
		t.Fatalf("固有平台应原样保留: %v", out2)
	}

	// 末段平台且有富余：n=6，L-1=5，平台 [2,5]=3，span=5-3=2 >= need=3 不成立，
	// 换一组：平台 [3,5]=3，need=2，span=2 >= need=2，末点钉在 5
	mono3 := []int{0, 1, 2, 3, 3, 3}
	out3, ps3 := spreadPlateaus(mono3)
	if !ps3[0].Spread {
		t.Fatalf("末段平台应可铺开: %+v", ps3[0])
	}
	want3 := []int{0, 1, 2, 3, 4, 5}
	for i := range want3 {
		if out3[i] != want3[i] {
			t.Fatalf("末段铺开 %v, want %v", out3, want3)
		}
	}
}
