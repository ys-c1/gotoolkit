package codec

import (
	"bytes"
	"math"
	"testing"
)

// lcg 生成确定性的伪噪声，避免测试依赖外部数据。
func lcgPix(n int) []uint8 {
	pix := make([]uint8, n)
	seed := uint32(0x12345678)
	for i := range pix {
		seed = seed*1103515245 + 12345
		pix[i] = uint8(seed >> 16)
	}
	return pix
}

func TestRLE2RoundTrip(t *testing.T) {
	cases := map[string][]uint8{
		"flat": bytes.Repeat([]uint8{42}, 256),
		"ramp": func() []uint8 {
			p := make([]uint8, 256)
			for i := range p {
				p[i] = uint8(i)
			}
			return p
		}(),
		"noise":   lcgPix(1000),
		"partial": lcgPix(300), // 不能被 256 整除，覆盖尾块
	}
	for tier := 1; tier <= 3; tier++ {
		for name, pix := range cases {
			body, err := EncodeRLE2(1, len(pix), pix, tier, nil)
			if err != nil {
				t.Fatalf("tier%d/%s 编码失败: %v", tier, name, err)
			}
			w, h, got, err := DecodeRLE2(body)
			if err != nil {
				t.Fatalf("tier%d/%s 解码失败: %v", tier, name, err)
			}
			if w != 1 || h != len(pix) || !bytes.Equal(got, pix) {
				t.Fatalf("tier%d/%s 回读不一致", tier, name)
			}
		}
	}
}

// TestRLE2CapInverse 验证反解系数：对未钳位的 cap，给定 cap 反算 H=K/cap，
// 再用 rle2Cap 正算必须回到同一个 cap。
func TestRLE2CapInverse(t *testing.T) {
	checks := []struct {
		k       int
		capFrom int
		capTo   int
	}{
		{2040, 10, 255},
		{512, 2, 255},
		{128, 1, 128},
	}
	for _, c := range checks {
		for want := c.capFrom; want <= c.capTo; want++ {
			h := float64(c.k) / float64(want) // 由期望 cap 反解熵
			got := rle2Cap(c.k, h)
			if got != want {
				t.Fatalf("K=%d 反算失败: cap=%d -> H=%.6f -> cap=%d", c.k, want, h, got)
			}
		}
	}
	// 钳位边界：H=0 给最大上限；极小熵也钳在 255；极大熵钳在 1。
	if rle2Cap(128, 0) != 255 || rle2Cap(128, 0.0001) != 255 {
		t.Fatal("低熵钳位错误")
	}
	if rle2Cap(128, 9999) != 1 {
		t.Fatal("高熵钳位错误")
	}
}

// TestRLE2KnownEntropy 用已知熵的块核对 cap 与编码行为。
func TestRLE2KnownEntropy(t *testing.T) {
	// 127 个 0 接 128 个 1（共 255，块长上限）：两个等概符号，H=1.0，
	// 且只有两个游程，不转义。
	block := make([]uint8, 255)
	for i := 127; i < 255; i++ {
		block[i] = 1
	}
	// 255 为奇数，两符号各 127/128 个，近似等概，熵极接近 1.0。
	if got := shannonEntropy(block); math.Abs(got-1.0) > 0.001 {
		t.Fatalf("两等概符号熵应接近 1.0，实际 %.6f", got)
	}
	if rle2ShouldEscape(block, 1.0) {
		t.Fatal("H=1 且仅两个游程的块不应转义")
	}
	wantCaps := [3]int{255, 255, 128} // round(K/1.0)
	for tier := 1; tier <= 3; tier++ {
		if got := rle2Cap(rle2TierK[tier-1], 1.0); got != wantCaps[tier-1] {
			t.Fatalf("tier%d H=1 时 cap=%d，期望 %d", tier, got, wantCaps[tier-1])
		}
		enc := encodeRLE2Block(block, tier)
		if enc[0] == 0 {
			t.Fatalf("tier%d 不应转义", tier)
		}
		if int(enc[0]) != wantCaps[tier-1] {
			t.Fatalf("tier%d 块头 cap 字段错误", tier)
		}
	}
}

// TestRLE2EscapeTrigger 验证转义的两条触发条件与统计。
func TestRLE2EscapeTrigger(t *testing.T) {
	// 条件 1：256 个不同取值，H=8 >= 阈值 4.0。
	allVals := make([]uint8, 256)
	for i := range allVals {
		allVals[i] = uint8(i)
	}
	if !rle2ShouldEscape(allVals, 8.0) {
		t.Fatal("H=8 的块必须按熵阈值转义")
	}
	// 条件 2：熵低于阈值但实测游程压不住（runs*2 > len）。
	// 8 个符号（H=3.0 < 4.0）交替排列使每个游程长度为 1，runs*2 远超块长。
	alt := make([]uint8, 255)
	for i := range alt {
		alt[i] = uint8(i % 8)
	}
	h := shannonEntropy(alt)
	if h >= rle2EscapeEntropy {
		t.Fatalf("前置条件错误: H=%.3f 应低于阈值 %.1f", h, rle2EscapeEntropy)
	}
	if !rle2ShouldEscape(alt, h) {
		t.Fatal("游程数*2 超过原始字节数时必须按实测条件转义")
	}
	// 小尾块豁免：1 像素块即使 runs*2=2 > 1 也不转义（同价）。
	if rle2ShouldEscape([]uint8{9}, 0) {
		t.Fatal("1 像素块不应转义")
	}
	// 端到端：高熵图的满块（255）应全部转义；末尾不足 3 像素的尾块按豁免
	// 规则可能走游程（同价），这里只要求所有转义块的负载统计自洽。
	pix := lcgPix(512)
	var st RLE2Stats
	body, err := EncodeRLE2(1, 512, pix, 1, &st)
	if err != nil {
		t.Fatal(err)
	}
	if st.RLEBlocks > 1 {
		t.Fatalf("高熵图至多 1 个小尾块走游程，实际 %d 个: %+v", st.RLEBlocks, st)
	}
	// 负载统计自洽：总负载 = 全部块头 + 转义块原始字节 + 游程块游程对。
	// 直接按 255/尾块 的分块结构独立重算，不依赖编码端的计数。
	wantPayload := 0
	for off := 0; off < len(pix); {
		blen := 255
		if rem := len(pix) - off; rem < blen {
			blen = rem
		}
		seg := pix[off : off+blen]
		h2 := shannonEntropy(seg)
		wantPayload += rle2BlockHead
		if rle2ShouldEscape(seg, h2) {
			wantPayload += blen
		} else {
			wantPayload += 2 * rle2RunCount(seg)
		}
		off += blen
	}
	if st.PayloadBytes != wantPayload {
		t.Fatalf("负载统计错误: got %d want %d (%+v)", st.PayloadBytes, wantPayload, st)
	}
	if _, _, got, err := DecodeRLE2(body); err != nil || !bytes.Equal(got, pix) {
		t.Fatal("转义块回读失败")
	}
}

// TestRLE2RecursiveSplit 构造“不转义但 RLE 压不住”的块，验证递归二分。
// 8 符号等概（H=3.0 < 转义阈值 4.0）、每游程长度 4、块长 254：
// 不转义需 2*runs <= 块长（游程长 >=2 即满足）；一成判据需
// 2*runs > 1.1*H*len/8 = 0.4125*len，即游程长 < 4.85。取游程长 4 同时
// 满足两条，且各级子块（127/64 量级）仍满足，故一路二分到 64 像素下限：
// 254->127+127，再各自二分，共 3 次切分、4 个末块、零转义。
func TestRLE2RecursiveSplit(t *testing.T) {
	block := make([]uint8, 254)
	for i := range block {
		block[i] = uint8((i / 4) % 8) // 每符号连续 4 次，8 符号等概，H=3.0
	}
	h := shannonEntropy(block)
	if math.Abs(h-3.0) > 0.01 {
		t.Fatalf("前置熵错误: %.6f", h)
	}
	if rle2ShouldEscape(block, h) {
		t.Fatal("该块不应触发转义，否则无法测试二分")
	}
	var st RLE2Stats
	out := make([]byte, 0)
	rle2EncodeRec(block, 1, &st, &out)
	if st.SplitCount != 3 {
		t.Fatalf("二分次数错误: %d", st.SplitCount)
	}
	if st.BlockCount != 4 {
		t.Fatalf("应切成 4 个末块，实际 %d", st.BlockCount)
	}
	if st.EscapeBlocks != 0 || st.RLEBlocks != 4 {
		t.Fatalf("末块应全部为游程块: %+v", st)
	}
	// 熵下界与分块方式基本无关（含每块独立 ceil 的取整误差，最多差 3 字节）。
	wantBound := int(math.Ceil(h * 254.0 / 8.0))
	if d := st.EntropyBoundRaw - wantBound; d < 0 || d > 3 {
		t.Fatalf("熵下界错误: %d，整体值 %d", st.EntropyBoundRaw, wantBound)
	}
	// 末块达到 64 像素下限后不再切分，WorstRatio 记录该数据能达到的最好水平，
	// 仍可能 >1.10——这是 RLE 对该数据的固有低效，不是分块不够细。
}

// TestRLE2Tier1PairsMatchGTR1Aligned 验证“当前参数下游程对逐字节相同”的
// 成立边界：当块边界正好是游程切点时（常值、或游程整除块长），GTR2 tier1
// （cap=255）游程块的 (计数,值) 与 GTR1 对同段像素的输出逐字节一致。
//
// 注意：混合数据上固定的 255 像素块边界不保证落在游程切点，跨界游程会在
// 块边界被多切一对，这是分块格式的固有代价，不属于本测试覆盖的对齐情形。
func TestRLE2Tier1PairsMatchGTR1Aligned(t *testing.T) {
	// 多张“块边界即游程切点”的图，整图逐块核对。
	cases := [][]uint8{
		bytes.Repeat([]uint8{7}, 255), // 单常值块
		bytes.Repeat([]uint8{7}, 510), // 两个 255 常值块
		// 5 个符号各连续 51：255=5*51，块内自洽，且块首尾符号不同，
		// 跨块不会出现同符号相邻游程，块边界严格落在游程切点。
		func() []uint8 {
			p := make([]uint8, 255)
			for i := range p {
				p[i] = uint8(i / 51)
			}
			return p
		}(),
		// 两张 255 块拼接（510 像素），同样满足每块 5 个长 51 的游程。
		func() []uint8 {
			p := make([]uint8, 510)
			for i := range p {
				p[i] = uint8((i % 255) / 51)
			}
			return p
		}(),
	}
	for _, pix := range cases {
		g1, err := EncodeRLE(1, len(pix), pix)
		if err != nil {
			t.Fatal(err)
		}
		g2, err := EncodeRLE2(1, len(pix), pix, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		var g2Pairs []byte
		i := rle2HeaderSize
		for i < len(g2) {
			mode, blen := int(g2[i]), int(g2[i+1])+1
			i += rle2BlockHead
			if mode == 0 {
				t.Fatal("对齐场景不应出现转义块")
			}
			consumed := 0
			for consumed < blen {
				g2Pairs = append(g2Pairs, g2[i], g2[i+1])
				consumed += int(g2[i])
				i += 2
			}
		}
		if !bytes.Equal(g2Pairs, g1[8:]) {
			t.Fatalf("对齐场景游程对应与 GTR1 逐字节不同: g1=%v g2=%v", g1[8:], g2Pairs)
		}
	}
}

// TestRLE2RunCountCap 验证按 cap 切分的游程计数：超过 cap 的连续游程拆成多对。
func TestRLE2RunCountCap(t *testing.T) {
	// 200 个 A + 55 个 B：天然 2 个游程。
	block := append(bytes.Repeat([]uint8{10}, 200), bytes.Repeat([]uint8{20}, 55)...)
	if got := rle2RunCountCap(block, 255); got != 2 {
		t.Fatalf("cap=255 应数出 2 个游程，实际 %d", got)
	}
	// cap=100：200 的 A 游程拆成 100+100 两对，B 一对，共 3 对。
	if got := rle2RunCountCap(block, 100); got != 3 {
		t.Fatalf("cap=100 应数出 3 对，实际 %d", got)
	}
	// cap=83：200 = 83+83+34 三对，B 一对，共 4 对。
	if got := rle2RunCountCap(block, 83); got != 4 {
		t.Fatalf("cap=83 应数出 4 对，实际 %d", got)
	}
	// 与编码端实际产出的游程对数一致（取 tier3，其 cap 收紧）。
	h := shannonEntropy(block)
	capv := rle2Cap(rle2TierK[2], h)
	enc := encodeRLE2Block(block, 3)
	wantPairs := rle2RunCountCap(block, capv)
	if got := (len(enc) - rle2BlockHead) / 2; got != wantPairs {
		t.Fatalf("编码游程对数 %d 与预估 %d 不一致", got, wantPairs)
	}
}

// TestRLE2TierCapsDifferOnLongRun 锁定三档语义：同一含超长游程的块，
// 各档按 cap=round(K/H) 反解出的上限不同，超过 cap 的游程被切成多对，
// 游程对数随档位收紧而增加。
func TestRLE2TierCapsDifferOnLongRun(t *testing.T) {
	// 255 块：200 个符号 A 接 55 个符号 B。
	block := append(bytes.Repeat([]uint8{10}, 200), bytes.Repeat([]uint8{20}, 55)...)
	h := shannonEntropy(block)
	pairs := [3]int{}
	caps := [3]int{}
	for tier := 1; tier <= 3; tier++ {
		enc := encodeRLE2Block(block, tier)
		if enc[0] == 0 {
			t.Fatalf("tier%d 低熵块不应转义", tier)
		}
		caps[tier-1] = int(enc[0])
		pairs[tier-1] = (len(enc) - rle2BlockHead) / 2
		if got := rle2Cap(rle2TierK[tier-1], h); got != caps[tier-1] {
			t.Fatalf("tier%d 块头 cap 与反解不一致", tier)
		}
	}
	// tier1 cap=255、tier2 该熵下也顶到 255：200 游程不切，各 2 对。
	// tier3 cap 收紧到 255 以下，200 游程被切成 2 段，共 3 对。
	if pairs[0] != 2 || pairs[1] != 2 {
		t.Fatalf("宽松档不应切断 200 游程: %v", pairs)
	}
	if pairs[2] <= pairs[1] {
		t.Fatalf("收紧档应产生更多游程对: caps=%v pairs=%v", caps, pairs)
	}
}

// TestRLE2Rejects 覆盖输入校验。
func TestRLE2Rejects(t *testing.T) {
	if _, err := EncodeRLE2(2, 2, []uint8{1, 2, 3}, 1, nil); err == nil {
		t.Fatal("像素数不匹配应报错")
	}
	if _, err := EncodeRLE2(2, 2, []uint8{1, 2, 3, 4}, 4, nil); err == nil {
		t.Fatal("档位越界应报错")
	}
	if _, _, _, err := DecodeRLE2([]byte("GTR1xxxx")); err == nil {
		t.Fatal("错误魔数应报错")
	}
}

// TestRLE2FlatMatchesGTR1Pairs 常值数据上，GTR2 tier1 游程块发出的 (计数,值)
// 序列必须与 GTR1 逐字节一致（差异只在容器头与块头）。
func TestRLE2FlatMatchesGTR1Pairs(t *testing.T) {
	pix := bytes.Repeat([]uint8{7}, 255)
	g1, err := EncodeRLE(1, 255, pix)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := EncodeRLE2(1, 255, pix, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	var g2Pairs []byte
	i := rle2HeaderSize
	for i < len(g2) {
		blen := int(g2[i+1]) + 1
		if g2[i] == 0 {
			t.Fatal("常值块不应转义")
		}
		body := i + rle2BlockHead
		g2Pairs = append(g2Pairs, g2[body:body+2]...)
		if int(g2[body]) != blen {
			t.Fatalf("游程计数 %d 与块长 %d 不一致", g2[body], blen)
		}
		i = body + 2
	}
	if !bytes.Equal(g2Pairs, g1[8:]) {
		t.Fatalf("游程对与 GTR1 不一致:\n g1=%v\n g2=%v", g1[8:], g2Pairs)
	}
}

// TestRLE2MixedPairsMatchGTR1 在单个 255 块上验证：tier1（cap=255）游程块
// 发出的 (计数,值) 序列与 GTR1 对同一段像素的输出逐字节相同。
// 数据用 5 个长游程（各 51 像素），块边界正好是游程切点，不存在跨界多切。
func TestRLE2MixedPairsMatchGTR1(t *testing.T) {
	pix := make([]uint8, 255)
	for i := range pix {
		pix[i] = uint8(i / 51) // 5 个符号各连续 51 次
	}
	g1, err := EncodeRLE(1, 255, pix)
	if err != nil {
		t.Fatal(err)
	}
	blockEnc := encodeRLE2Block(pix, 1)
	if blockEnc[0] == 0 {
		t.Fatal("该数据不应转义")
	}
	if got := rle2Cap(rle2TierK[0], shannonEntropy(pix)); got != 255 {
		t.Fatalf("tier1 低熵块 cap 应为 255，实际 %d", got)
	}
	g2Pairs := blockEnc[rle2BlockHead:]
	if !bytes.Equal(g2Pairs, g1[8:]) {
		t.Fatalf("单块游程对与 GTR1 不一致（g1 %d 对, g2 %d 对）",
			(len(g1)-8)/2, len(g2Pairs)/2)
	}
	// 整图解码仍须无损。
	g2, err := EncodeRLE2(1, 255, pix, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, got, err := DecodeRLE2(g2); err != nil || !bytes.Equal(got, pix) {
		t.Fatal("整图回读失败")
	}
}
