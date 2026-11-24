package api

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sample() *Image {
	img := &Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	return img
}

// chainSample 是 16×8 的宽分布样本：像素覆盖 0..255 全区间，且分布落在
// 多个掩码阈值两侧，使「掩码 / 伽马 / 抖动」任意两步互换都能改变字节流。
func chainSample() *Image {
	const w, h = 16, 8
	img := &Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[y*w+x] = uint8((x*11 + y*23 + 5) % 256)
		}
	}
	return img
}

// chainOpt 是证据用的三开关同开参数：8 个掩码值、伽马 2.2、抖动幅度 160。
func chainOpt(f string) Options {
	return Options{
		Format:     f,
		MaskValues: []int{0, 32, 64, 96, 128, 160, 192, 224},
		Gamma:      2.2,
		Dither:     160,
	}
}

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if got.W != img.W || got.H != img.H {
			t.Fatalf("%s 尺寸不一致", f)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("%s 像素不一致", f)
		}
		if got.Alpha != nil {
			t.Fatalf("%s 全关时 Alpha 应为 nil", f)
		}
	}
}

// 全关（缺省参数）时锁定样本必须与 golden 逐字节一致，三格式逐一核对。
func TestGoldenBytes(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		for _, opt := range []Options{
			{Format: f},
			DefaultOptions(),
			{Format: f, MaskValues: nil, Gamma: 0, Dither: 0},
		} {
			opt.Format = f
			got, err := Encode(img, opt)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s 全关时与 golden 不逐字节一致 (opt=%+v)", f, opt)
			}
		}
	}
}

// 错误码：掩码重复与伽马非有限必须各返回一个独立、稳定的错误码。
func TestErrorCodes(t *testing.T) {
	img := sample()
	cases := []struct {
		name string
		opt  Options
		want ErrorCode
		sent error
	}{
		{"掩码重复", Options{Format: "pgm", MaskValues: []int{10, 10}}, CodeMaskDuplicate, ErrMaskDuplicate},
		{"掩码重复无序", Options{Format: "pgm", MaskValues: []int{255, 0, 255}}, CodeMaskDuplicate, ErrMaskDuplicate},
		{"伽马NaN", Options{Format: "pgm", Gamma: math.NaN()}, CodeGammaNonFinite, ErrGammaNonFinite},
		{"伽马+Inf", Options{Format: "pgm", Gamma: math.Inf(1)}, CodeGammaNonFinite, ErrGammaNonFinite},
		{"伽马-Inf", Options{Format: "pgm", Gamma: math.Inf(-1)}, CodeGammaNonFinite, ErrGammaNonFinite},
	}
	for _, c := range cases {
		_, err := Encode(img, c.opt)
		if !errors.Is(err, c.sent) {
			t.Fatalf("%s: Encode 错误类型不符: %v", c.name, err)
		}
		if got := CodeOf(err); got != c.want {
			t.Fatalf("%s: CodeOf=%d，期望 %d", c.name, got, c.want)
		}
		// 解码侧同样拦下。
		_, err = Decode([]byte("P5\n1 1\n255\n\x00"), c.opt)
		if got := CodeOf(err); got != c.want {
			t.Fatalf("%s: Decode CodeOf=%d，期望 %d", c.name, got, c.want)
		}
		t.Logf("错误码 %d: %s -> %v", c.want, c.name, err)
	}
	// 两个错误码互不相同，且与既有错误码不冲突。
	if CodeMaskDuplicate == CodeGammaNonFinite {
		t.Fatal("掩码重复与伽马非有限必须使用不同错误码")
	}
}

func TestRejects(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "tga"}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm"}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm"}); err == nil {
		t.Fatal("空数据应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Dither: 256}); err == nil {
		t.Fatal("抖动越界应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", MaskValues: []int{-1}}); err == nil {
		t.Fatal("掩码越界应报错")
	}
}

// 掩码往返：多值掩码、三格式，像素零误差还原，Alpha 逐像素与命中集合一致。
func TestMaskRoundTrip(t *testing.T) {
	img := chainSample()
	mask := []int{64, 128}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := Encode(img, Options{Format: f, MaskValues: mask})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f, MaskValues: mask})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("%s 掩码往返像素未零误差还原", f)
		}
		want := maskAlpha(img.Pix, []uint8{64, 128})
		if !bytes.Equal(got.Alpha, want) {
			t.Fatalf("%s Alpha 与命中集合不符", f)
		}
	}
}

// diffBytes 返回首个不同字节的下标（相同为 -1）与不同字节总数。
func diffBytes(a, b []byte) (first, count int) {
	first = -1
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if first < 0 {
				first = i
			}
			count++
		}
	}
	if len(a) != len(b) {
		if first < 0 {
			first = n
		}
		d := len(a) - len(b)
		if d < 0 {
			d = -d
		}
		count += d
	}
	return first, count
}

func sum16(b []byte) string {
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:8])
}

// 顺序链：唯一记录顺序为 mask>gamma>dither。
// 任意调换（其余全部 5 种排列）都必须与记录顺序的字节流不同，
// 且给出精确到第几个字节的差异；两次调换顺序的结果之间也做字节差分。
func TestChainOrderSensitivity(t *testing.T) {
	img := chainSample()
	opt := chainOpt("pgm")

	if strings.Join(CanonicalOrder, ">") != "mask>gamma>dither" {
		t.Fatalf("记录顺序被改动: %v", CanonicalOrder)
	}
	canon, err := Encode(img, opt)
	if err != nil {
		t.Fatal(err)
	}

	perms := [][]string{
		{"mask", "gamma", "dither"}, // 记录顺序
		{"mask", "dither", "gamma"}, // 伽马与抖动调换
		{"gamma", "mask", "dither"}, // 掩码与伽马调换
		{"gamma", "dither", "mask"}, // 轮换
		{"dither", "mask", "gamma"}, // 轮换
		{"dither", "gamma", "mask"}, // 掩码与抖动调换
	}
	type run struct {
		order []string
		body  []byte
		sum   string
	}
	runs := make([]run, 0, len(perms))
	for _, p := range perms {
		b, err := EncodeWithOrder(img, opt, p)
		if err != nil {
			t.Fatalf("%v 编码失败: %v", p, err)
		}
		runs = append(runs, run{order: p, body: b, sum: sum16(b)})
	}
	// runs[0] 必须是记录顺序，且 Encode 与 EncodeWithOrder(CanonicalOrder) 一致。
	if !bytes.Equal(runs[0].body, canon) {
		t.Fatal("Encode 与 EncodeWithOrder(CanonicalOrder) 结果不一致")
	}

	// 两行对照：记录顺序一行，调换顺序一行。
	t.Logf("对照行1 order=%s bytes=%d sha256_8=%s", strings.Join(perms[0], ">"), len(runs[0].body), runs[0].sum)
	t.Logf("对照行2 order=%s bytes=%d sha256_8=%s", strings.Join(perms[2], ">"), len(runs[2].body), runs[2].sum)

	for _, r := range runs[1:] {
		first, count := diffBytes(canon, r.body)
		if first < 0 {
			t.Fatalf("调换为 %s 后与记录顺序逐字节相同，顺序不敏感", strings.Join(r.order, ">"))
		}
		t.Logf("调换 %-22s vs 记录顺序: 首差偏移=%d 差异字节=%d sha256_8=%s",
			strings.Join(r.order, ">"), first, count, r.sum)
		if len(r.body) != len(canon) {
			t.Fatalf("%v 长度 %d 与记录顺序 %d 不一致", r.order, len(r.body), len(canon))
		}
	}

	// 两次调换顺序的结果互相做字节差分（精确到第几个字节）。
	firstAB, countAB := diffBytes(runs[2].body, runs[5].body)
	if firstAB < 0 {
		t.Fatal("两次调换顺序的结果不应相同")
	}
	t.Logf("两次调换顺序对照: orderA=%s orderB=%s 首差偏移=%d 差异字节=%d",
		strings.Join(runs[2].order, ">"), strings.Join(runs[5].order, ">"), firstAB, countAB)

	// 两次顺序的结果都落盘存档，供 cmp 逐字节复核；差分报告一并落盘。
	dir := filepath.Join("..", "artifacts", "chain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	saveArtifact(t, filepath.Join(dir, "order_mask_gamma_dither.pgm"), runs[0].body)
	saveArtifact(t, filepath.Join(dir, "order_gamma_mask_dither.pgm"), runs[2].body)
	saveArtifact(t, filepath.Join(dir, "order_dither_gamma_mask.pgm"), runs[5].body)
	// 同一份 16×8 输入也落盘，供命令行复跑后与上面三份逐字节 cmp。
	inBody, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	saveArtifact(t, filepath.Join(dir, "input.pgm"), inBody)

	var rep strings.Builder
	fmt.Fprintf(&rep, "# 顺序对照与字节差分（由 go test ./api -run TestChainOrderSensitivity 生成）\n\n")
	fmt.Fprintf(&rep, "记录顺序: %s\n", strings.Join(runs[0].order, ">"))
	fmt.Fprintf(&rep, "调换顺序: %s\n\n", strings.Join(runs[2].order, ">"))
	fmt.Fprintf(&rep, "两行对照:\n")
	fmt.Fprintf(&rep, "  行1 order=%-20s bytes=%d sha256_8=%s\n", strings.Join(runs[0].order, ">"), len(runs[0].body), runs[0].sum)
	fmt.Fprintf(&rep, "  行2 order=%-20s bytes=%d sha256_8=%s\n\n", strings.Join(runs[2].order, ">"), len(runs[2].body), runs[2].sum)
	fmt.Fprintf(&rep, "每种调换相对记录顺序的字节差分（偏移从 0 起）:\n")
	for _, r := range runs[1:] {
		first, count := diffBytes(canon, r.body)
		fmt.Fprintf(&rep, "  %-20s 首差偏移=%d 差异字节=%d sha256_8=%s\n",
			strings.Join(r.order, ">"), first, count, r.sum)
	}
	fmt.Fprintf(&rep, "\n记录顺序与 %s 逐字节明细（前 8 处）:\n", strings.Join(runs[2].order, ">"))
	shown := 0
	for i := 0; i < len(canon) && shown < 8; i++ {
		if canon[i] != runs[2].body[i] {
			fmt.Fprintf(&rep, "  偏移 %d: 记录=%02x 调换=%02x\n", i, canon[i], runs[2].body[i])
			shown++
		}
	}

	// 两次调换顺序之间的差分：精确到第几个字节，并逐处列出。
	fmt.Fprintf(&rep, "\n两次调换顺序的字节差分:\n")
	fmt.Fprintf(&rep, "  A=%-18s bytes=%d sha256_8=%s\n", strings.Join(runs[2].order, ">"), len(runs[2].body), runs[2].sum)
	fmt.Fprintf(&rep, "  B=%-18s bytes=%d sha256_8=%s\n", strings.Join(runs[5].order, ">"), len(runs[5].body), runs[5].sum)
	firstAB, countAB = diffBytes(runs[2].body, runs[5].body)
	fmt.Fprintf(&rep, "  首差字节偏移=%d（从 0 起计，即第 %d 个字节）差异字节总数=%d\n", firstAB, firstAB+1, countAB)
	shown = 0
	for i := 0; i < len(runs[2].body) && shown < 8; i++ {
		if runs[2].body[i] != runs[5].body[i] {
			fmt.Fprintf(&rep, "  偏移 %d: A=%02x B=%02x\n", i, runs[2].body[i], runs[5].body[i])
			shown++
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "diff.txt"), []byte(rep.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func saveArtifact(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// 十二组：3 种格式 × 4 种开关组合，每组都给出数字。
// 组合：C0 全关 / C1 掩码 / C2 掩码+伽马 / C3 掩码+伽马+抖动（记录顺序）。
// 样本用 chainSample + chainOpt：C3 即三条链全开，也是顺序对照所用的同一组参数。
func TestTwelveGroups(t *testing.T) {
	img := chainSample()
	full := chainOpt("pgm")
	mask := full.MaskValues
	combos := []struct {
		name string
		opt  func(f string) Options
	}{
		{"C0全关", func(f string) Options { return Options{Format: f} }},
		{"C1掩码", func(f string) Options { return Options{Format: f, MaskValues: mask} }},
		{"C2掩码+伽马", func(f string) Options { return Options{Format: f, MaskValues: mask, Gamma: full.Gamma} }},
		{"C3掩码+伽马+抖动", func(f string) Options { return chainOpt(f) }},
	}
	formats := []string{"pgm", "bmp", "rle"}

	baseline := map[string][]uint8{}
	for _, f := range formats {
		b, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		baseline[f] = b
	}

	rows := 0
	var rep strings.Builder
	fmt.Fprintf(&rep, "# 十二组指标（3 格式 × 4 开关组合）\n\n")
	fmt.Fprintf(&rep, "样本: 16×8 宽分布灰度；掩码值 %v，伽马 %.1f，抖动幅度 %d。\n", mask, full.Gamma, full.Dither)
	fmt.Fprintf(&rep, "由 `go test ./api -run TestTwelveGroups -v` 生成。\n\n")
	fmt.Fprintf(&rep, "| 组 | 格式 | 组合 | 字节数 | 解码均值 | 透明像素 | 相对全关首差偏移 | 差异字节 | sha256_8 |\n")
	fmt.Fprintf(&rep, "|---:|:---:|:---|---:|---:|---:|---:|---:|:---|\n")
	for _, f := range formats {
		for _, c := range combos {
			body, err := Encode(img, c.opt(f))
			if err != nil {
				t.Fatalf("%s/%s 编码失败: %v", f, c.name, err)
			}
			dec, err := Decode(body, c.opt(f))
			if err != nil {
				t.Fatalf("%s/%s 解码失败: %v", f, c.name, err)
			}
			sum := 0
			for _, v := range dec.Pix {
				sum += int(v)
			}
			mean := float64(sum) / float64(len(dec.Pix))
			transparent := 0
			for _, a := range dec.Alpha {
				if a == 0 {
					transparent++
				}
			}
			first, count := diffBytes(baseline[f], body)
			firstStr := "-"
			if first >= 0 {
				firstStr = fmt.Sprintf("%d", first)
			}
			t.Logf("组%02d fmt=%s 组合=%-16s 字节=%4d 均值=%7.3f 透明=%2d 相对全关首差偏移=%3s 差异字节=%3d sha256_8=%s",
				rows+1, f, c.name, len(body), mean, transparent, firstStr, count, sum16(body))
			fmt.Fprintf(&rep, "| %d | %s | %s | %d | %.3f | %d | %s | %d | %s |\n",
				rows+1, f, c.name, len(body), mean, transparent, firstStr, count, sum16(body))
			rows++
		}
	}
	if rows != 12 {
		t.Fatalf("十二组只跑了 %d 组", rows)
	}
	dir := filepath.Join("..", "artifacts", "chain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "twelve-groups.md"), []byte(rep.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
