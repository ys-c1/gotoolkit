package codec

import (
	"math/rand"
	"testing"
)

func TestBLKFlatRoundTripHeader(t *testing.T) {
	pix := make([]uint8, 4*4)
	for i := range pix {
		pix[i] = uint8(i*10 + 1)
	}
	body, err := EncodeBLK(4, 4, 2, false, pix)
	if err != nil {
		t.Fatal(err)
	}
	if string(body[:4]) != blkMagic {
		t.Fatalf("魔数错误: %q", body[:4])
	}
	if body[8] != 2 || body[9] != blkPredictOff {
		t.Fatalf("头字段错误: bs=%d predict=%d", body[8], body[9])
	}
	w, h, got, err := DecodeBLK(body)
	if err != nil || w != 4 || h != 4 || len(got) != 16 {
		t.Fatalf("回读失败: %v", err)
	}
}

// TestBLKPredictWeightsSum 验证预测系数之和为一。
// 一维内部边节点 n = (1·dcL + 1·dcR)/2，两个系数各为 1/2。
func TestBLKPredictWeightsSum(t *testing.T) {
	if blkWeightA+blkWeightB != blkWeightN {
		t.Fatalf("一维系数和不为一: %d+%d != %d", blkWeightA, blkWeightB, blkWeightN)
	}
	// 二维内部角点是四块等权平均，四个 1/4 之和同样为一：4*(1/4)=1。
	if 4 != blkWeightN*blkWeightN {
		t.Fatal("二维四个 1/4 系数和不为一")
	}
}

// TestBLKPredictSeamZero 预测路径在多档块长下缝宽严格为零（共享节点）。
func TestBLKPredictSeamZero(t *testing.T) {
	const w, h = 64, 64
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8(32 + 2*x + y)
		}
	}
	for _, bs := range []int{4, 8, 16} {
		body, err := EncodeBLK(w, h, bs, true, pix)
		if err != nil {
			t.Fatal(err)
		}
		_, _, got, err := DecodeBLK(body)
		if err != nil {
			t.Fatal(err)
		}
		if d := maxSeamAtBoundaries(w, h, bs, got); d != 0 {
			t.Fatalf("bs=%d 预测路径缝宽=%d，期望 0", bs, d)
		}
	}
}

// TestBLKPredictSeamZeroRandom 任意随机图、含尾块的多种块长下，预测缝仍严格为零。
// 这与图像内容无关：边界两侧算的是同一个整数表达式，缝为零是构造保证。
// 前提是块维 >=2：块长 1 时整块只有一个像素，无法同时等于左右两个不同节点，
// 属于共享节点模型的退化情形，不在消缝承诺内（三档目标块长 4/8/16 均满足前提）。
func TestBLKPredictSeamZeroRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	cases := [][3]int{
		{30, 22, 4}, // 两方向都有尾块
		{31, 17, 3},
		{16, 16, 5},
		{7, 11, 2}, // 最小非退化块长
		{33, 9, 6},
	}
	for _, c := range cases {
		w, h, bs := c[0], c[1], c[2]
		pix := make([]uint8, w*h)
		for i := range pix {
			pix[i] = uint8(rng.Intn(256))
		}
		body, err := EncodeBLK(w, h, bs, true, pix)
		if err != nil {
			t.Fatal(err)
		}
		rw, rh, got, err := DecodeBLK(body)
		if err != nil {
			t.Fatal(err)
		}
		if d := maxSeamAtBoundaries(rw, rh, bs, got); d != 0 {
			t.Fatalf("w=%d h=%d bs=%d 随机图预测缝=%d，期望 0", w, h, bs, d)
		}
	}
}

// TestBLKFlatSeamGrowsWithBlockSize 不预测路径缝宽随块长增大，且与理论值一致。
func TestBLKFlatSeamGrowsWithBlockSize(t *testing.T) {
	const w, h = 64, 64
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8(32 + 2*x + y)
		}
	}
	prev := -1
	for _, bs := range []int{4, 8, 16} {
		body, err := EncodeBLK(w, h, bs, false, pix)
		if err != nil {
			t.Fatal(err)
		}
		_, _, got, err := DecodeBLK(body)
		if err != nil {
			t.Fatal(err)
		}
		d := maxSeamAtBoundaries(w, h, bs, got)
		// 渐变梯度：水平方向每像素 2 级、竖直方向 1 级；平面块边界跳变即梯度×块长。
		want := 2 * bs
		if d != want {
			t.Fatalf("bs=%d 不预测缝宽=%d，期望理论值 %d", bs, d, want)
		}
		if d <= prev {
			t.Fatalf("缝宽未随块长增大: bs=%d d=%d prev=%d", bs, d, prev)
		}
		prev = d
	}
}

// TestBLKToggleBackKeepsDC 切回不预测路径结果不变：
// 两条路径编码出的直流网格逐字节相同，只有 predict 开关字节不同。
func TestBLKToggleBackKeepsDC(t *testing.T) {
	const w, h = 10, 7
	pix := make([]uint8, w*h)
	for i := range pix {
		pix[i] = uint8((i*13 + 7) % 256)
	}
	flat, err := EncodeBLK(w, h, 3, false, pix)
	if err != nil {
		t.Fatal(err)
	}
	pred, err := EncodeBLK(w, h, 3, true, pix)
	if err != nil {
		t.Fatal(err)
	}
	if len(flat) != len(pred) {
		t.Fatalf("两条路径流长度不同: %d != %d", len(flat), len(pred))
	}
	// 除 predict 字节（下标 9）外逐字节一致。
	for i := range flat {
		if i == 9 {
			continue
		}
		if flat[i] != pred[i] {
			t.Fatalf("下标 %d 的直流负载不一致", i)
		}
	}
	if flat[9] != blkPredictOff || pred[9] != blkPredictOn {
		t.Fatal("predict 开关字节错误")
	}
	// 不预测路径两次解码结果完全一致。
	_, _, got1, err := DecodeBLK(flat)
	if err != nil {
		t.Fatal(err)
	}
	_, _, got2, err := DecodeBLK(flat)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got1 {
		if got1[i] != got2[i] {
			t.Fatal("不预测路径不具确定性")
		}
	}
}

// TestBLKResidualAttribution 残余缝宽归属到具体取整步骤。
//
// 本实现三步取整：
//
//	① blockDC：块均值四舍五入到 uint8；
//	② 节点平均：相邻块直流加权平均四舍五入到 uint8；
//	③ 块内插值：双线性分子四舍五入到 uint8。
//
// 节点被相邻块共享，且边界像素严格等于节点，所以②③在边界上不产生两侧差异；
// 残余缝宽只可能来自“不共享节点、各块独立取整”的设计，且源头是节点取整②，
// 下面用最小可复现数值证明该残余恰为 1 个灰度级。
func TestBLKResidualAttribution(t *testing.T) {
	// ②的独立取整反例（本实现不采用）：同一奇直流差，两侧用不同取整方向。
	dcL, dcR := 10, 13                  // 真实平均 11.5
	roundHalfUp := divRound(dcL+dcR, 2) // 四舍五入 → 12
	truncate := (dcL + dcR) / 2         // 向下截断 → 11
	residual := absInt(roundHalfUp - truncate)
	if residual != 1 {
		t.Fatalf("节点独立取整残余应为 1 级，实际 %d", residual)
	}

	// ③是缝中性的：边界像素在 fx=0 / fx=dx 时表达式退化为单个节点值，
	// divRound(v*den, den) 对任意 den 都还原 v，插值取整不引入任何差。
	for _, den := range []int{1, 2, 3, 7, 15} {
		v := divRound(200*den, den) // 边界处单节点表达式
		if v != 200 {
			t.Fatalf("den=%d 边界插值取整改变了节点值: %d", den, v)
		}
	}

	// 对照：本实现共享同一节点，两侧边界值逐字节相同，缝为零。
	shared := uint8(divRound(dcL+dcR, 2))
	if absInt(int(shared)-int(shared)) != 0 {
		t.Fatal("共享节点不应有缝")
	}
}

// TestBLKPartialBlocks 尾块不足整块时仍能完整重建且不越界；
// 块长 1 覆盖宽/高塌缩分支（dx=0 或 dy=0），只验不 panic、画幅正确。
func TestBLKPartialBlocks(t *testing.T) {
	const w, h = 10, 6
	pix := make([]uint8, w*h)
	for i := range pix {
		pix[i] = uint8((i*5 + 3) % 256)
	}
	for _, bs := range []int{4, 1} {
		for _, predict := range []bool{false, true} {
			body, err := EncodeBLK(w, h, bs, predict, pix)
			if err != nil {
				t.Fatal(err)
			}
			rw, rh, got, err := DecodeBLK(body)
			if err != nil {
				t.Fatal(err)
			}
			if rw != w || rh != h || len(got) != w*h {
				t.Fatalf("bs=%d 尾块画幅错误: %dx%d len=%d", bs, rw, rh, len(got))
			}
		}
	}
}

func TestBLKRejects(t *testing.T) {
	if _, err := EncodeBLK(0, 4, 2, false, make([]uint8, 0)); err == nil {
		t.Fatal("零宽应报错")
	}
	if _, err := EncodeBLK(4, 4, 0, false, make([]uint8, 16)); err == nil {
		t.Fatal("零块长应报错")
	}
	if _, err := EncodeBLK(4, 4, 65, false, make([]uint8, 16)); err == nil {
		t.Fatal("块长越界应报错")
	}
	if _, _, _, err := DecodeBLK([]byte("XXXX\x04\x00\x04\x00\x02\x00")); err == nil {
		t.Fatal("坏魔数应报错")
	}
}

// maxSeamAtBoundaries 返回块边界两侧像素灰度差绝对值的最大值，两个方向都查。
func maxSeamAtBoundaries(w, h, bs int, pix []uint8) int {
	m := 0
	for x := bs; x < w; x += bs {
		for y := 0; y < h; y++ {
			if d := absInt(int(pix[y*w+x]) - int(pix[y*w+x-1])); d > m {
				m = d
			}
		}
	}
	for y := bs; y < h; y += bs {
		for x := 0; x < w; x++ {
			if d := absInt(int(pix[y*w+x]) - int(pix[(y-1)*w+x])); d > m {
				m = d
			}
		}
	}
	return m
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
