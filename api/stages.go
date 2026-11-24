package api

// 本文件把命令行与库调用共用的管线显式拆成四个可观测环节，
// 供对拍测试做“分段直通”：两条路径在每个环节上的产物应当逐字节相同。
// 环节划分与 api.Encode / api.Decode 的内部步骤严格一致，不引入任何新行为。

// StageName 是管线中的一个环节。
const (
	StagePGMDecode = "s1_pgm_decode" // 环节1：格式解码出的原始灰度图（encode 时输入固定为 PGM）
	StageGain      = "s2_gain"       // 环节2：增益后的灰度图（gain=0 时与环节1相同）
	StageFormatEnc = "s3_format_enc" // 环节3：目标格式编码后的字节流
	StageFileBytes = "s4_file_bytes" // 环节4：最终写盘的字节（当前实现与环节3相同）
)

// Stage 是一个环节的直通产物。
type Stage struct {
	Name string
	W, H int
	Pix  []uint8 // 图像环节使用；字节环节为 nil
	Body []byte  // 字节环节使用；图像环节为 nil
}

// PipeStages 跑一遍 gtk 命令行内部的完整管线，并返回每个环节的产物。
//
// encode 子命令的管线为：
//
//	Decode(pgm) -> Encode(format, gain)
//
// decode 子命令的管线为：
//
//	Decode(format, gain) -> Encode(pgm)
//
// 返回的最后一个 Stage 即最终落盘字节。命令行和库调用都必须经由本函数，
// 这样两条路径的差异可以被定位到具体环节，而不是只能看到最终文件不同。
func PipeStages(raw []byte, mode string, format string, gain int) ([]Stage, error) {
	switch mode {
	case "encode":
		return pipeEncode(raw, format, gain)
	case "decode":
		return pipeDecode(raw, format, gain)
	default:
		return nil, ErrUnknownFormat
	}
}

func pipeEncode(raw []byte, format string, gain int) ([]Stage, error) {
	// 环节1：输入固定按 PGM 解码，与 cmd/gtk 保持一致。
	img, err := Decode(raw, Options{Format: "pgm"})
	if err != nil {
		return nil, err
	}
	stages := []Stage{{Name: StagePGMDecode, W: img.W, H: img.H, Pix: append([]uint8(nil), img.Pix...)}}

	// 环节2：增益在 api.Encode 内部作用于副本，这里显式做出该副本。
	gained := img
	if gain != 0 {
		gained = applyGain(img, gain)
	}
	stages = append(stages, Stage{Name: StageGain, W: gained.W, H: gained.H, Pix: append([]uint8(nil), gained.Pix...)})

	// 环节3/4：格式编码，编码结果即写盘字节。
	body, err := Encode(img, Options{Format: format, Gain: gain})
	if err != nil {
		return stages, err
	}
	stages = append(stages,
		Stage{Name: StageFormatEnc, Body: append([]byte(nil), body...)},
		Stage{Name: StageFileBytes, Body: append([]byte(nil), body...)},
	)
	return stages, nil
}

func pipeDecode(raw []byte, format string, gain int) ([]Stage, error) {
	// 环节1：目标格式解码（不带增益），对应 codec.DecodeXxx 得到的原始灰度图。
	plain, err := Decode(raw, Options{Format: format})
	if err != nil {
		return nil, err
	}
	stages := []Stage{{Name: StagePGMDecode, W: plain.W, H: plain.H, Pix: append([]uint8(nil), plain.Pix...)}}

	// 环节2：再按 decode 语义施加反向增益（api.Decode 内部即 applyGain(img, -gain)）。
	img := plain
	if gain != 0 {
		img = applyGain(plain, -gain)
	}
	stages = append(stages, Stage{Name: StageGain, W: img.W, H: img.H, Pix: append([]uint8(nil), img.Pix...)})

	// 环节3/4：输出固定回 PGM，编码结果即写盘字节。
	// img 已用与 api.Decode 相同的 applyGain(-gain) 得到，无需再解一次。
	body, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		return stages, err
	}
	stages = append(stages,
		Stage{Name: StageFormatEnc, Body: append([]byte(nil), body...)},
		Stage{Name: StageFileBytes, Body: append([]byte(nil), body...)},
	)
	return stages, nil
}
