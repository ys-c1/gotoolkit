package quant

import (
	"fmt"
	"strings"
	"testing"
)

// TestFixedTablesSatisfyBothConstraints 量化区间与重建电平都必须单调。
func TestFixedTablesSatisfyBothConstraints(t *testing.T) {
	for bits := 1; bits <= 8; bits++ {
		tab, err := New(bits)
		if err != nil {
			t.Fatalf("bits=%d 构造失败: %v", bits, err)
		}
		if err := tab.Check(); err != nil {
			t.Fatalf("bits=%d 不满足单调性约束: %v", bits, err)
		}
		// 量化地复核两条约束（不依赖 Check 内部实现）。
		if tab.Bounds[0] != 0 || tab.Bounds[tab.Levels] != 256 {
			t.Fatalf("bits=%d 区间端点错误: %v", bits, tab.Bounds)
		}
		for j := 0; j < tab.Levels; j++ {
			if tab.Bounds[j] >= tab.Bounds[j+1] {
				t.Fatalf("bits=%d 区间边界不严格递增 at %d", bits, j)
			}
			if j > 0 && tab.Recon[j] <= tab.Recon[j-1] {
				t.Fatalf("bits=%d 重建电平不严格递增 at %d", bits, j)
			}
		}
	}
}

// TestRampCheckedPointByPoint 修后：黑到白全 256 点逐对比较，三档位深零破坏。
func TestRampCheckedPointByPoint(t *testing.T) {
	for _, bits := range []int{2, 3, 4} {
		tab, err := New(bits)
		if err != nil {
			t.Fatal(err)
		}
		m := MeasureRamp(tab)
		if m.Points != 256 || m.Pairs != 255 {
			t.Fatalf("bits=%d 检查规模应为 256 点 / 255 对，实为 %d 点 / %d 对",
				bits, m.Points, m.Pairs)
		}
		if m.BreakCount != 0 {
			t.Fatalf("bits=%d 修后仍有 %d 个破坏点: %v", bits, m.BreakCount, m.FirstBreaks)
		}
		if m.WorstDelta < 0 {
			t.Fatalf("bits=%d 修后最大反转量应为非负，实为 %d", bits, m.WorstDelta)
		}
		if m.WorstDrop != 0 {
			t.Fatalf("bits=%d 修后向下幅度应为 0，实为 %d", bits, m.WorstDrop)
		}
	}
}

// TestRampMonotoneAllDepths 全位深扫描同样不允许任何破坏点。
func TestRampMonotoneAllDepths(t *testing.T) {
	for bits := 1; bits <= 8; bits++ {
		tab, _ := New(bits)
		if got := BreakIndices(Reconstruct(tab)); len(got) != 0 {
			t.Fatalf("bits=%d 出现破坏点 %v", bits, got)
		}
	}
}

// TestDeltaSignConvention 锁定题目定义：Δ_i=y[i+1]-y[i]，向下为负。
func TestDeltaSignConvention(t *testing.T) {
	// 160 -> 96 是一次向下反转，Δ 必须为 -64。
	y := []uint8{32, 160, 96, 224}
	d := Deltas(y)
	want := []int{128, -64, 128}
	if fmt.Sprint(d) != fmt.Sprint(want) {
		t.Fatalf("相邻差应为 %v，实为 %v", want, d)
	}
	if got := BreakIndices(y); len(got) != 1 || got[0] != 1 {
		t.Fatalf("破坏点应为 [1]，实为 %v", got)
	}
	if w := WorstDelta(y); w != -64 {
		t.Fatalf("最大反转量（带符号）应为 -64，实为 %d", w)
	}
	if m := WorstDropMagnitude(y); m != 64 {
		t.Fatalf("最大向下幅度应为 64，实为 %d", m)
	}
}

// TestLegacyBugPreserved 留档的修前量化器必须确实非单调，且实测数字被锁定，
// 防止“样例与解释不一致”或后续误改把 bug 悄悄修掉。
func TestLegacyBugPreserved(t *testing.T) {
	want := map[int]struct {
		breaks int
		worst  int
		drop   int
	}{
		2: {1, -64, 64},
		3: {3, -160, 160},
		4: {7, -208, 208},
	}
	for _, bits := range []int{2, 3, 4} {
		tab, err := NewLegacyBitReversed(bits)
		if err != nil {
			t.Fatal(err)
		}
		// 区间约束没坏，坏的是重建电平单调性。
		if err := tab.Check(); err == nil {
			t.Fatalf("bits=%d 修前量化器应被 Check 判为违反电平单调", bits)
		} else if !strings.Contains(err.Error(), "重建电平单调性被破坏") {
			t.Fatalf("bits=%d 应报告重建电平约束被破坏，实为: %v", bits, err)
		}
		m := MeasureRamp(tab)
		w := want[bits]
		if m.BreakCount != w.breaks {
			t.Fatalf("bits=%d 修前破坏点数应为 %d，实为 %d（位置 %v）",
				bits, w.breaks, m.BreakCount, m.FirstBreaks)
		}
		if m.WorstDelta != w.worst {
			t.Fatalf("bits=%d 修前最大反转量应为 %d，实为 %d", bits, w.worst, m.WorstDelta)
		}
		if m.WorstDrop != w.drop {
			t.Fatalf("bits=%d 修前向下幅度应为 %d，实为 %d", bits, w.drop, m.WorstDrop)
		}
	}
}

// TestIntervalInversionAlsoBreaksConstraint 证明区间单调性这一半同样被检查：
// 人为交换两条边界后 Check 必须报错（即便电平递增也不合法）。
func TestIntervalInversionAlsoBreaksConstraint(t *testing.T) {
	tab, _ := New(3)
	tab.Bounds[3], tab.Bounds[4] = tab.Bounds[4], tab.Bounds[3] // 96 与 128 互换，制造边界逆序
	if err := tab.Check(); err == nil {
		t.Fatal("区间边界逆序应被 Check 拦截")
	}
}

// TestBitReverseVectors 固定位反转表，文档中的排列数字必须与代码一致。
func TestBitReverseVectors(t *testing.T) {
	cases := []struct {
		bits int
		want []int
	}{
		{2, []int{0, 2, 1, 3}},
		{3, []int{0, 4, 2, 6, 1, 5, 3, 7}},
		{4, []int{0, 8, 4, 12, 2, 10, 6, 14, 1, 9, 5, 13, 3, 11, 7, 15}},
	}
	for _, c := range cases {
		got := make([]int, 1<<c.bits)
		for i := range got {
			got[i] = BitReverse(i, c.bits)
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Fatalf("bits=%d 位反转表应为 %v，实为 %v", c.bits, c.want, got)
		}
	}
}
