package codec

import (
	"reflect"
	"testing"
)

func blkTestImage(w, h int) []uint8 {
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8((x*7 + y*13 + x*y) % 256)
		}
	}
	return pix
}

func TestBLKSizeEqualsActualLength(t *testing.T) {
	cases := []struct{ w, h, b, d int }{
		{8, 8, 8, 1},
		{16, 16, 8, 4},
		{20, 13, 8, 3},  // 非整除，含右边/下边/角块
		{17, 1, 8, 2},   // 单行边块
		{1, 17, 8, 5},   // 单列边块
		{3, 5, 8, 8},    // 整图小于一个块
		{64, 48, 16, 6}, // B=16 整除
	}
	for _, c := range cases {
		pix := blkTestImage(c.w, c.h)
		body, err := EncodeBLK(c.w, c.h, pix, c.b, c.d)
		if err != nil {
			t.Fatalf("EncodeBLK %+v: %v", c, err)
		}
		want := BLKSize(c.w, c.h, c.b, c.d)
		if len(body) != want {
			t.Fatalf("%+v 实际长度 %d != BLKSize %d", c, len(body), want)
		}
	}
}

func TestBLKSizeMonotonicInDepth(t *testing.T) {
	w, h, b := 40, 30, 8
	prev := 0
	for d := 1; d <= 8; d++ {
		n := BLKSize(w, h, b, d)
		if n < prev {
			t.Fatalf("位深 %d 体积 %d 小于前一档 %d，单调性被破坏", d, n, prev)
		}
		prev = n
	}
}

func TestBLKConstantImageLossless(t *testing.T) {
	for _, d := range []int{1, 2, 4, 8} {
		w, h := 20, 20
		pix := make([]uint8, w*h)
		for i := range pix {
			pix[i] = 200
		}
		body, err := EncodeBLK(w, h, pix, 8, d)
		if err != nil {
			t.Fatal(err)
		}
		rw, rh, got, err := DecodeBLK(body)
		if err != nil || rw != w || rh != h {
			t.Fatalf("解码失败 d=%d: %v %dx%d", d, err, rw, rh)
		}
		for i, v := range got {
			if v != 200 {
				t.Fatalf("纯色图 d=%d 必须无损，位置 %d 得 %d", d, i, v)
			}
		}
	}
}

func TestBLKHighDepthNearLosslessAndErrorDecreases(t *testing.T) {
	for _, dims := range [][2]int{{32, 24}, {20, 17}, {13, 7}, {5, 5}} { // 后三组含右边/下边/角块
		w, h := dims[0], dims[1]
		pix := blkTestImage(w, h)
		var prevMAE float64 = 1e9
		for _, d := range []int{1, 2, 4, 8} {
			body, err := EncodeBLK(w, h, pix, 8, d)
			if err != nil {
				t.Fatal(err)
			}
			rw, rh, got, err := DecodeBLK(body)
			if err != nil || rw != w || rh != h || len(got) != w*h {
				t.Fatalf("d=%d dims=%v 解码异常: %v %dx%d len=%d", d, dims, err, rw, rh, len(got))
			}
			var sum, maxAbs int
			for i := range pix {
				e := int(pix[i]) - int(got[i])
				if e < 0 {
					e = -e
				}
				sum += e
				if e > maxAbs {
					maxAbs = e
				}
			}
			mae := float64(sum) / float64(len(pix))
			if mae > prevMAE+1e-9 {
				t.Fatalf("dims=%v 位深 %d 平均误差 %.3f 未随位深下降（前 %.3f）", dims, d, mae, prevMAE)
			}
			prevMAE = mae
			if d == 8 && maxAbs > 2 {
				t.Fatalf("dims=%v d=8 应近无损，最大误差 %d > 2", dims, maxAbs)
			}
		}
	}
}

func TestBlockDecoderReusesBuffer(t *testing.T) {
	w, h, b := 20, 20, 8 // 3x3 = 9 块
	pix := blkTestImage(w, h)
	body, err := EncodeBLK(w, h, pix, b, 4)
	if err != nil {
		t.Fatal(err)
	}
	bd, err := NewBlockDecoder(body)
	if err != nil {
		t.Fatal(err)
	}
	if cap(bd.Buffer()) != b*b {
		t.Fatalf("中间缓冲容量应为 B*B=%d，实际 %d", b*b, cap(bd.Buffer()))
	}
	basePtr := reflect.ValueOf(bd.Buffer()).Pointer()

	blocks := 0
	for {
		_, _, bw, bh, px, done, err := bd.Next()
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		blocks++
		if reflect.ValueOf(bd.Buffer()).Pointer() != basePtr {
			t.Fatalf("第 %d 块后复用缓冲底层数组发生变化（未复用）", blocks)
		}
		if reflect.ValueOf(px).Pointer() != basePtr {
			t.Fatalf("第 %d 块返回切片未别名到复用缓冲", blocks)
		}
		if len(px) != bw*bh {
			t.Fatalf("块像素长度 %d != %d", len(px), bw*bh)
		}
	}
	if blocks != 9 {
		t.Fatalf("应有 9 个块，实际 %d", blocks)
	}

	// 复用缓冲解出的整图仍应正确（与一次性 DecodeBLK 一致）。
	_, _, full, err := DecodeBLK(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != w*h {
		t.Fatalf("整图长度 %d", len(full))
	}
}

func TestBLKRejectsInvalid(t *testing.T) {
	if _, _, _, err := DecodeBLK([]byte("XXXX000000")); err == nil {
		t.Fatal("错误魔数应报错")
	}
	good, err := EncodeBLK(8, 8, blkTestImage(8, 8), 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := DecodeBLK(good[:len(good)-3]); err == nil {
		t.Fatal("截断流应报错")
	}
	if _, err := EncodeBLK(8, 8, make([]byte, 8), 8, 4); err == nil {
		t.Fatal("像素数不匹配应报错")
	}
	if _, err := EncodeBLK(8, 8, make([]byte, 64), 8, 0); err == nil {
		t.Fatal("位深 0 应报错")
	}
}

func TestBudgetDepthGuaranteesFit(t *testing.T) {
	for _, budget := range []int{1000, 5000, 20000, 40000, 67000} {
		w, h, b := 256, 256, 8
		d := BudgetDepth(w, h, b, budget)
		if d == 0 {
			// 连 d=1 都放不下时，预算必须小于 d=1 的精确体积。
			if budget >= BLKSize(w, h, b, 1) {
				t.Fatalf("预算 %d 反解 d=0，但 d=1 实际只需 %d", budget, BLKSize(w, h, b, 1))
			}
			continue
		}
		if got := BLKSize(w, h, b, d); got > budget {
			t.Fatalf("预算 %d 反解 d=%d，但该位深体积 %d 超预算", budget, d, got)
		}
		if d < 8 {
			if got := BLKSize(w, h, b, d+1); got <= budget {
				t.Fatalf("预算 %d 反解 d=%d 非最大：d+1 体积 %d 仍放得下", budget, d, got)
			}
		}
	}
}

func TestBlockSizeForBudget(t *testing.T) {
	// d=4、每块 258 字节载荷时，B²*4/8 <= 256 => B² <= 512 => B<=22。
	b := BlockSizeForBudget(258, 4)
	if b < 1 || b > 64 {
		t.Fatalf("块尺寸越界: %d", b)
	}
	if b*b*4 > 8*(258-2) {
		t.Fatalf("块尺寸 %d 不满足预算不等式", b)
	}
}
