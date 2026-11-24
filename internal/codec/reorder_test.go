package codec

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestCountRuns(t *testing.T) {
	if got := CountRuns(nil); got != 0 {
		t.Fatalf("空序列游程数应为 0，得 %d", got)
	}
	if got := CountRuns([]uint8{7, 7, 7, 1, 2, 2}); got != 3 {
		t.Fatalf("游程数应为 3，得 %d", got)
	}
}

func TestReorderByValueOptimal(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 50; trial++ {
		n := 1 + rng.Intn(500)
		pix := make([]uint8, n)
		for i := range pix {
			pix[i] = uint8(rng.Intn(256))
		}
		seq, perm := ReorderByValue(pix)
		// 置换必须合法且逐位对应
		seen := make([]bool, n)
		for j, p := range perm {
			if p < 0 || p >= n || seen[p] {
				t.Fatal("置换非法")
			}
			seen[p] = true
			if seq[j] != pix[p] {
				t.Fatal("seq[j] != pix[perm[j]]")
			}
		}
		// 游程数必须等于不同值个数（理论最优 R* = k）
		var cnt [256]int
		for _, v := range pix {
			cnt[v]++
		}
		k := 0
		for _, c := range cnt {
			if c > 0 {
				k++
			}
		}
		if got := CountRuns(seq); got != k {
			t.Fatalf("重排后游程数 %d != k=%d", got, k)
		}
	}
}

func TestRLE2RoundTrip(t *testing.T) {
	cases := [][]uint8{
		{5},
		{9, 9, 9, 9},
		{0, 1, 2, 3, 4, 5, 6, 7},
		bytes.Repeat([]uint8{200}, 300), // 单值超过 255，触发游程计数拆分
	}
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 20; trial++ {
		pix := make([]uint8, 1+rng.Intn(2000))
		for i := range pix {
			pix[i] = uint8(rng.Intn(1 + rng.Intn(256)))
		}
		cases = append(cases, pix)
	}
	for idx, pix := range cases {
		w, h := len(pix), 1
		body, err := EncodeRLE2(w, h, pix)
		if err != nil {
			t.Fatalf("case %d 编码失败: %v", idx, err)
		}
		gw, gh, got, err := DecodeRLE2(body)
		if err != nil {
			t.Fatalf("case %d 解码失败: %v", idx, err)
		}
		if gw != w || gh != h {
			t.Fatalf("case %d 尺寸不一致", idx)
		}
		if !bytes.Equal(got, pix) {
			t.Fatalf("case %d 解码与原图不逐像素一致", idx)
		}
	}
}

func TestRLE2Rejects(t *testing.T) {
	good, err := EncodeRLE2(4, 1, []uint8{3, 1, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := DecodeRLE2(good[:len(good)-1]); err == nil {
		t.Fatal("截断应报错")
	}
	bad := append([]byte(nil), good...)
	bad[0] = 'X'
	if _, _, _, err := DecodeRLE2(bad); err == nil {
		t.Fatal("错误魔数应报错")
	}
	bad2 := append([]byte(nil), good...)
	bad2[len(bad2)-1] ^= 0xff // 破坏置换流
	if _, _, _, err := DecodeRLE2(bad2); err == nil {
		t.Fatal("置换损坏应报错")
	}
	if _, err := EncodeRLE2(0, 1, nil); err == nil {
		t.Fatal("画幅为零应报错")
	}
}

// TestBatchWindowBound 验证近似算法的误差界 R <= ⌈n/W⌉ * R*。
func TestBatchWindowBound(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for trial := 0; trial < 30; trial++ {
		n := 1 + rng.Intn(3000)
		pix := make([]uint8, n)
		alpha := 1 + rng.Intn(20)
		for i := range pix {
			pix[i] = uint8(rng.Intn(alpha))
		}
		w := 1 + rng.Intn(64)
		got := BatchWindowRuns(pix, w)
		windows := (n + w - 1) / w
		if got > windows*alpha {
			t.Fatalf("n=%d W=%d: 游程数 %d 超出上界 %d", n, w, got, windows*alpha)
		}
		// 不分窗时退化为精确算法，应达到 R* = k（不同值个数）
		var cnt [256]int
		for _, v := range pix {
			cnt[v]++
		}
		k := 0
		for _, c := range cnt {
			if c > 0 {
				k++
			}
		}
		if exact := BatchWindowRuns(pix, 0); exact != k {
			t.Fatalf("不分窗应精确达到 k=%d，得 %d", k, exact)
		}
	}
}
