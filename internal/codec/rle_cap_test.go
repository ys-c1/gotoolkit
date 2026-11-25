package codec

import (
	"bytes"
	"math/rand"
	"testing"
)

// runLengths 返回像素流的极大同值段长度序列（切分前的天然游程）。
func runLengths(pix []uint8) []int {
	var segs []int
	for i := 0; i < len(pix); {
		j := i
		for j < len(pix) && pix[j] == pix[i] {
			j++
		}
		segs = append(segs, j-i)
		i = j
	}
	return segs
}

// optimalTokensDP 是按块穷举的动态规划版：
// 对每个长度为 L 的极大同值段，dp[l] 表示覆盖前 l 个像素的最少游程数，
// 每个游程可覆盖 1..cap 个同值像素，转移 dp[l] = 1 + min dp[l-k]。
// 段与段之间不能合并，最优游程总数为各段最优值之和。
func optimalTokensDP(pix []uint8, maxRun int) int {
	total := 0
	for _, L := range runLengths(pix) {
		dp := make([]int, L+1)
		for l := 1; l <= L; l++ {
			best := l
			for k := 1; k <= maxRun && k <= l; k++ {
				if 1+dp[l-k] < best {
					best = 1 + dp[l-k]
				}
			}
			dp[l] = best
		}
		total += dp[L]
	}
	return total
}

// optimalTokensBrute 是真正的按块穷举：在整条像素流上 DFS 枚举每一个合法
// 游程切分（同值、长度 1..cap），取所有完整切分中游程数的最小值。
// 指数复杂度，只用于小图，作为 DP 的独立交叉校验。
func optimalTokensBrute(pix []uint8, maxRun int) int {
	var dfs func(pos int) int
	dfs = func(pos int) int {
		if pos == len(pix) {
			return 0
		}
		best := len(pix) - pos + 1
		for k := 1; k <= maxRun && pos+k <= len(pix); k++ {
			same := true
			for t := 0; t < k; t++ {
				if pix[pos+t] != pix[pos] {
					same = false
					break
				}
			}
			if !same {
				break // 再长只会继续跨值，提前剪枝
			}
			if r := 1 + dfs(pos+k); r < best {
				best = r
			}
		}
		return best
	}
	return dfs(0)
}

// countRLETokens 去掉 8 字节头后统计游程条数。
func countRLETokens(body []byte) int { return (len(body) - 8) / 2 }

// TestRLECapGreedyMatchesOptimal 核心判据：贪心体积与按块穷举最优值之差 < 1 字节。
func TestRLECapGreedyMatchesOptimal(t *testing.T) {
	rng := rand.New(rand.NewSource(20261126))
	cases := [][]uint8{
		{9, 9, 9, 7},
		{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}, // 单段 L=12
		{1, 1, 2, 2, 2, 3, 3, 3, 3, 1, 1, 1},
		{0, 0, 0, 0, 0, 0, 0, 0},
	}
	// 追加随机图：小字母表保证出现长同值段，能真正触发上限断开。
	for g := 0; g < 40; g++ {
		n := 1 + rng.Intn(24)
		pix := make([]uint8, n)
		for i := range pix {
			pix[i] = uint8(rng.Intn(3))
		}
		cases = append(cases, pix)
	}

	for _, maxRun := range []int{1, 2, 3, 5, 7, 16, 64, 255} {
		for idx, pix := range cases {
			w := len(pix)
			body, err := EncodeRLEWithCap(w, 1, pix, maxRun)
			if err != nil {
				t.Fatalf("cap=%d case=%d 编码失败: %v", maxRun, idx, err)
			}
			greedyTokens := countRLETokens(body)
			optDP := optimalTokensDP(pix, maxRun)
			if greedyTokens != optDP {
				t.Fatalf("cap=%d case=%d 贪心游程 %d != DP最优 %d", maxRun, idx, greedyTokens, optDP)
			}
			// 小图再上全局穷举交叉验证 DP 本身。
			if len(pix) <= 14 {
				if optBrute := optimalTokensBrute(pix, maxRun); optBrute != optDP {
					t.Fatalf("cap=%d case=%d DP=%d 与穷举=%d 不一致", maxRun, idx, optDP, optBrute)
				}
			}
			// 判据写成差值小于一字节：每条游程 2 字节，体积差 = 2*(游程差)。
			greedyBytes, optBytes := len(body), 8+2*optDP
			if diff := greedyBytes - optBytes; !(diff < 1) {
				t.Fatalf("cap=%d case=%d 体积差 %d 字节不满足 <1", maxRun, idx, diff)
			}
		}
	}
}

// TestRLECapRoundTrip 所有上限下编码都能无损回读。
func TestRLECapRoundTrip(t *testing.T) {
	pix := []uint8{7, 7, 7, 7, 7, 3, 3, 8, 8, 8, 8, 8, 8, 8, 8, 1}
	for _, maxRun := range []int{1, 2, 3, 4, 8, 255} {
		body, err := EncodeRLEWithCap(len(pix), 1, pix, maxRun)
		if err != nil {
			t.Fatal(err)
		}
		w, h, got, err := DecodeRLE(body)
		if err != nil || w != len(pix) || h != 1 || !bytes.Equal(got, pix) {
			t.Fatalf("cap=%d 回读失败", maxRun)
		}
	}
}

// TestRLECapTwoDegenerates 上限取 2：每段被切成 ceil(L/2) 条游程，
// 成对打包、奇长度段末尾留一个单像素游程。
func TestRLECapTwoDegenerates(t *testing.T) {
	pix := []uint8{9, 9, 9, 9, 9, 4, 4, 4} // 段长 5 与 3
	body, err := EncodeRLEWithCap(len(pix), 1, pix, 2)
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := []byte{2, 9, 2, 9, 1, 9, 2, 4, 1, 4}
	if !bytes.Equal(body[8:], wantCounts) {
		t.Fatalf("cap=2 退化为成对打包不符: got %v want %v", body[8:], wantCounts)
	}
	if got := countRLETokens(body); got != 3+2 {
		t.Fatalf("cap=2 游程数=%d, 期望 ceil(5/2)+ceil(3/2)=5", got)
	}
}

// TestRLECapRejects 上限越界报错；默认编码器仍固定 255。
func TestRLECapRejects(t *testing.T) {
	pix := []uint8{1, 1}
	for _, bad := range []int{0, -1, 256, 1000} {
		if _, err := EncodeRLEWithCap(2, 1, pix, bad); err == nil {
			t.Fatalf("cap=%d 应报错", bad)
		}
	}
	body, err := EncodeRLE(2, 1, pix)
	if err != nil {
		t.Fatal(err)
	}
	if countRLETokens(body) != 1 {
		t.Fatal("默认 EncodeRLE 行为被改变")
	}
}
