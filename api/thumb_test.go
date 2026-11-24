package api

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// gridImg 构造 6×6 灰度图，尺寸同时被 2、3 整除，缩略比例 2 与 3 都可整块平均。
func gridImg() *Image {
	const n = 6
	img := &Image{W: n, H: n, Pix: make([]uint8, n*n)}
	for i := range img.Pix {
		img.Pix[i] = uint8((i*13 + 7) % 256)
	}
	return img
}

// TestTwelveGroups 跑满十二组：3 种缩略状态（不缩略/除数2/除数3）× 调色板开/关 × 抖动开/关。
// 缺任何一组本测试都会失败。
func TestTwelveGroups(t *testing.T) {
	const buckets = 8
	type group struct {
		name    string
		thumb   int
		palette bool
		dither  bool
	}
	var groups []group
	for _, thumb := range []int{0, 2, 3} { // 缩略比例 2 与 3 两种都测
		for _, palette := range []bool{false, true} {
			for _, dither := range []bool{false, true} {
				name := ""
				switch thumb {
				case 0:
					name = "thumb-off"
				case 2:
					name = "thumb-2"
				case 3:
					name = "thumb-3"
				}
				if palette {
					name += "/palette"
				} else {
					name += "/no-palette"
				}
				if dither {
					name += "/dither"
				} else {
					name += "/no-dither"
				}
				groups = append(groups, group{name, thumb, palette, dither})
			}
		}
	}
	if len(groups) != 12 {
		t.Fatalf("必须跑满十二组，实际 %d 组", len(groups))
	}

	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.name] = true
		t.Run(g.name, func(t *testing.T) {
			img := gridImg()
			opt := Options{
				Format:   "idx",
				Palette:  g.palette,
				Dither:   g.dither,
				ThumbDiv: g.thumb,
				Buckets:  buckets,
			}
			if !g.palette {
				opt.Format = "pgm" // idx 必须开调色板；关闭组用 pgm 验证常规链路
			}
			body, rep, err := EncodeWithReport(img, opt)
			if err != nil {
				t.Fatalf("编码失败: %v", err)
			}
			if len(body) == 0 {
				t.Fatal("产物为空")
			}

			// 缩略状态与画幅。
			wantDiv := g.thumb
			if g.thumb <= 1 {
				if rep.ThumbApplied {
					t.Fatal("除数为 0 不应缩略")
				}
				wantDiv = 1
			} else if !rep.ThumbApplied || rep.ThumbDiv != g.thumb {
				t.Fatalf("缩略标记错误: %+v", rep)
			}
			wantW := (img.W + wantDiv - 1) / wantDiv
			wantH := (img.H + wantDiv - 1) / wantDiv
			if rep.ThumbW != wantW || rep.ThumbH != wantH {
				t.Fatalf("缩略后画幅 = %dx%d, 期望 %dx%d", rep.ThumbW, rep.ThumbH, wantW, wantH)
			}

			// 索引数量：缩略前/后两列，差值可复算（索引不去重 => 数量即像素数）。
			if g.palette {
				if !rep.PaletteApplied || rep.Buckets != buckets {
					t.Fatal("调色板报告错误")
				}
				wantBefore := img.W * img.H
				wantAfter := wantW * wantH
				if rep.IndexCountBefore != wantBefore || rep.IndexCountAfter != wantAfter {
					t.Fatalf("索引数量 = 前%d/后%d, 期望 前%d/后%d",
						rep.IndexCountBefore, rep.IndexCountAfter, wantBefore, wantAfter)
				}
				if rep.Delta != rep.IndexCountAfter-rep.IndexCountBefore {
					t.Fatal("差值无法由两列复算")
				}
				if rep.Delta != wantAfter-wantBefore {
					t.Fatalf("差值 = %d, 期望 %d", rep.Delta, wantAfter-wantBefore)
				}
				// idx 头部 9 字节 + 调色板 + 每像素一个索引（不去重）。
				if len(body) != 9+buckets+wantAfter {
					t.Fatalf("idx 字节数 = %d, 期望 %d", len(body), 9+buckets+wantAfter)
				}
				// 抖动强度 = 1/缩略除数（未缩略时分母为 1）。
				if rep.DitherStrengthNum != 1 || rep.DitherStrengthDen != wantDiv {
					t.Fatalf("抖动强度 = %d/%d, 期望 1/%d",
						rep.DitherStrengthNum, rep.DitherStrengthDen, wantDiv)
				}
				if rep.DitherApplied != g.dither {
					t.Fatal("抖动标记错误")
				}
				// 回读必须还原成缩略后画幅。
				got, err := Decode(body, Options{Format: "idx"})
				if err != nil {
					t.Fatalf("idx 回读失败: %v", err)
				}
				if got.W != wantW || got.H != wantH || len(got.Pix) != wantAfter {
					t.Fatal("回读画幅与缩略结果不符")
				}
			} else {
				if rep.PaletteApplied || rep.IndexCountBefore != 0 || rep.IndexCountAfter != 0 || rep.Delta != 0 {
					t.Fatal("调色板关闭时不应有索引数量")
				}
				if rep.DitherApplied {
					t.Fatal("调色板关闭时抖动必须关闭")
				}
			}
		})
	}
	if len(seen) != 12 {
		t.Fatalf("十二组不完整: %d", len(seen))
	}
}

// TestAllOffBytesUnchanged 全关时三种格式产物与既有 golden 基准逐字节一致。
func TestAllOffBytesUnchanged(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		// 显式零值 Options：新开关全部处于关闭状态。
		got, rep, err := EncodeWithReport(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 全关产物与原版基准不一致", f)
		}
		if rep.ThumbApplied || rep.PaletteApplied || rep.DitherApplied {
			t.Fatalf("%s 全关却报告链路生效", f)
		}
		// 旧入口 Encode 的行为同样不变。
		got2, err := Encode(img, Options{Format: f})
		if err != nil || !bytes.Equal(got2, want) {
			t.Fatalf("%s Encode 入口产物改变", f)
		}
	}
}

// TestErrorCodes 桶数为零与缩略除数为负各给一个独立错误码。
func TestErrorCodes(t *testing.T) {
	img := gridImg()

	_, err := Encode(img, Options{Format: "pgm", Palette: true, Buckets: 0})
	if !errors.Is(err, ErrZeroBuckets) {
		t.Fatalf("桶数为零应返回 ErrZeroBuckets, 实际 %v", err)
	}
	_, _, err = EncodeWithReport(img, Options{Format: "pgm", Palette: true, Buckets: 0, ThumbDiv: 2})
	if !errors.Is(err, ErrZeroBuckets) {
		t.Fatalf("缩略+桶数为零仍应返回 ErrZeroBuckets, 实际 %v", err)
	}

	_, err = Encode(img, Options{Format: "pgm", ThumbDiv: -1})
	if !errors.Is(err, ErrBadThumbDiv) {
		t.Fatalf("缩略除数为负应返回 ErrBadThumbDiv, 实际 %v", err)
	}
	_, err = Decode(nil, Options{Format: "pgm", ThumbDiv: -3})
	if !errors.Is(err, ErrBadThumbDiv) {
		t.Fatalf("解码路径除数为负也应返回 ErrBadThumbDiv, 实际 %v", err)
	}

	// 两个错误码必须互不相同。
	if errors.Is(ErrZeroBuckets, ErrBadThumbDiv) || ErrZeroBuckets.Error() == ErrBadThumbDiv.Error() {
		t.Fatal("两个错误码必须可区分")
	}
	// 除数为零是合法的“不缩略”，不得报错。
	if _, _, err := EncodeWithReport(img, Options{Format: "pgm", ThumbDiv: 0}); err != nil {
		t.Fatalf("除数 0 应表示不缩略: %v", err)
	}
}

// TestThumbnailAreaAverage 面积平均可手工复算：2×2 与 3×3 分块。
func TestThumbnailAreaAverage(t *testing.T) {
	img := &Image{W: 4, H: 2, Pix: []uint8{
		10, 20, 30, 40,
		50, 60, 70, 80,
	}}
	// div=2：两块 (10+20+50+60)/4=35，(30+40+70+80)/4=55。
	body, err := Encode(img, Options{Format: "pgm", ThumbDiv: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if got.W != 2 || got.H != 1 || len(got.Pix) != 2 || got.Pix[0] != 35 || got.Pix[1] != 55 {
		t.Fatalf("div=2 面积平均错误: %+v", got.Pix)
	}

	// div=3：宽 4 => 两块；左块 3×2 六像素和 10+20+30+50+60+70=240，平均 40；
	// 右块 1×2 两像素 40+80=120，平均 60。
	body3, err := Encode(img, Options{Format: "pgm", ThumbDiv: 3})
	if err != nil {
		t.Fatal(err)
	}
	got3, err := Decode(body3, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if got3.W != 2 || got3.H != 1 || len(got3.Pix) != 2 {
		t.Fatalf("div=3 边缘块画幅错误: %+v", got3)
	}
	if got3.Pix[0] != 40 || got3.Pix[1] != 60 {
		t.Fatalf("div=3 分块平均 = %+v, 期望 [40 60]", got3.Pix)
	}
}

// TestIndexNoDedup 索引不去重：重复像素也各占一个索引项。
func TestIndexNoDedup(t *testing.T) {
	img := &Image{W: 4, H: 1, Pix: []uint8{10, 10, 10, 200}}
	body, rep, err := EncodeWithReport(img, Options{
		Format: "idx", Palette: true, Buckets: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.IndexCountAfter != 4 {
		t.Fatalf("索引应不去重，数量 = %d", rep.IndexCountAfter)
	}
	// 头部后紧跟 4 个调色板字节与 4 个索引字节。
	if len(body) != 9+4+4 {
		t.Fatalf("idx 长度错误: %d", len(body))
	}
	index := body[13:]
	if index[0] != index[1] || index[1] != index[2] {
		t.Fatal("前三个同值像素应映射到同一调色板项，但仍各占索引")
	}
	if index[3] == index[0] {
		t.Fatal("200 不应与暗部像素同桶")
	}
}

// TestDitherActuallyPerturbs 抖动开启后结果应与纯量化不同，且强度随除数变化。
func TestDitherActuallyPerturbs(t *testing.T) {
	// 6×6 线性渐变，量化到 4 桶时大量像素落在桶间，误差扩散必然改变索引。
	img := &Image{W: 6, H: 6, Pix: make([]uint8, 36)}
	for i := range img.Pix {
		img.Pix[i] = uint8((i * 255) / 35)
	}
	plain, _, err := EncodeWithReport(img, Options{
		Format: "idx", Palette: true, Buckets: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	dithered, rep, err := EncodeWithReport(img, Options{
		Format: "idx", Palette: true, Buckets: 4, Dither: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(plain, dithered) {
		t.Fatal("抖动开启后索引产物应发生变化")
	}
	if rep.DitherStrengthNum != 1 || rep.DitherStrengthDen != 1 {
		t.Fatalf("未缩略时强度应为 1/1, 实际 %d/%d",
			rep.DitherStrengthNum, rep.DitherStrengthDen)
	}

	// 缩略除数 2、3：强度分别为 1/2、1/3，抖动结果与全强度不同且互不相同。
	d2, rep2, err := EncodeWithReport(img, Options{
		Format: "idx", Palette: true, Buckets: 4, ThumbDiv: 2, Dither: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	d3, rep3, err := EncodeWithReport(img, Options{
		Format: "idx", Palette: true, Buckets: 4, ThumbDiv: 3, Dither: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.DitherStrengthNum != 1 || rep2.DitherStrengthDen != 2 {
		t.Fatalf("除数 2 时强度应为 1/2, 实际 %d/%d",
			rep2.DitherStrengthNum, rep2.DitherStrengthDen)
	}
	if rep3.DitherStrengthDen != 3 {
		t.Fatalf("除数 3 时强度分母应为 3, 实际 %d", rep3.DitherStrengthDen)
	}
	if bytes.Equal(d2, d3) {
		t.Fatal("缩略比例 2 与 3 的抖动结果应不同")
	}
}

// TestIndexDeltaRecomputable 索引数量差可由缩略前后两列复算（多尺寸穷举）。
func TestIndexDeltaRecomputable(t *testing.T) {
	for _, dim := range [][2]int{{6, 6}, {5, 7}, {8, 4}, {3, 9}} {
		img := &Image{W: dim[0], H: dim[1], Pix: make([]uint8, dim[0]*dim[1])}
		for i := range img.Pix {
			img.Pix[i] = uint8(i * 3)
		}
		for _, div := range []int{2, 3} {
			_, rep, err := EncodeWithReport(img, Options{
				Format: "idx", Palette: true, Buckets: 16, ThumbDiv: div,
			})
			if err != nil {
				t.Fatal(err)
			}
			nw, nh := (dim[0]+div-1)/div, (dim[1]+div-1)/div
			if rep.IndexCountBefore != dim[0]*dim[1] || rep.IndexCountAfter != nw*nh {
				t.Fatalf("%dx%d div=%d 两列数量错误", dim[0], dim[1], div)
			}
			if rep.Delta != rep.IndexCountAfter-rep.IndexCountBefore {
				t.Fatal("差值不可复算")
			}
		}
	}
}
