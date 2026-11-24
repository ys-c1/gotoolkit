package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gotoolkit/api"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "encode":
		os.Exit(runEncode(os.Args[2:]))
	case "decode":
		os.Exit(runDecode(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtk encode|decode --format pgm|bmp|rle --in F --out F [--gain N]")
}

// run 是 encode/decode 共用的管线：读入 -> 分段直通 -> 写盘。
// 两条子命令与库调用经由同一个 api.PipeStages，保证环节产物可对拍。
func run(mode string, format string, gain int, inPath, outPath string) int {
	raw, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取失败:", err)
		return 1
	}
	stages, err := api.PipeStages(raw, mode, format, gain)
	if err != nil {
		// 编码阶段失败时 stages 里仍可能保留前置环节，尽量转储以便定位。
		dumpStages(stages)
		fmt.Fprintln(os.Stderr, pipelineVerb(mode)+"失败:", err)
		return 1
	}
	dumpStages(stages)
	body := stages[len(stages)-1].Body
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		return 1
	}
	first, last := stages[0], stages[len(stages)-1]
	if mode == "encode" {
		fmt.Printf("encoded %dx%d -> %s (%d bytes)\n", first.W, first.H, format, len(last.Body))
	} else {
		fmt.Printf("decoded %s -> %dx%d pgm (%d bytes)\n", format, first.W, first.H, len(last.Body))
	}
	return 0
}

func pipelineVerb(mode string) string {
	if mode == "encode" {
		return "编码"
	}
	return "解码"
}

// dumpStages 仅在设置了 GTK_DUMP_DIR 时把各环节产物落盘，供对拍测试做分段直通。
// 不设该环境变量时什么都不做，对正常运行零影响。
func dumpStages(stages []api.Stage) {
	dir := os.Getenv("GTK_DUMP_DIR")
	if dir == "" {
		return
	}
	for _, st := range stages {
		var payload []byte
		if st.Body != nil {
			payload = st.Body
		} else {
			payload = st.Pix
		}
		path := filepath.Join(dir, st.Name+".bin")
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "环节转储失败:", err)
			return
		}
	}
}

func runEncode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输出格式")
	in := fs.String("in", "", "输入 PGM")
	out := fs.String("out", "", "输出文件")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	return run("encode", *format, *gain, *in, *out)
}

func runDecode(args []string) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	format := fs.String("format", "pgm", "输入格式")
	in := fs.String("in", "", "输入文件")
	out := fs.String("out", "", "输出 PGM")
	gain := fs.Int("gain", 0, "亮度增益百分比")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in 或 --out")
		return 2
	}
	return run("decode", *format, *gain, *in, *out)
}
