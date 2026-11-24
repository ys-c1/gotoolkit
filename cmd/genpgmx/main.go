// genpgmx 生成头结构支路的验收样例（含字段冲突样例），写入 testdata/pgmx。
// 独立程序，不与 internal/codec 共享代码：扩展块按固定偏移手工拼接，
// 保证样例字节布局可审计。
package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
)

// 与 internal/codec 扩展块布局保持一致的固定偏移表。
const (
	extSize       = 12
	offBitDepth   = 6
	offFlags      = 7
	offChecksum   = 8
	flagFlipped   = 0x01
	validBitDepth = 8
)

func samplePix() (int, int, []byte) {
	// 4x3，与 golden 同源像素，便于对照。
	w, h := 4, 3
	pix := make([]byte, 12)
	for i := range pix {
		pix[i] = byte(i * 7)
	}
	return w, h, pix
}

func stdHeader(w, h int) []byte {
	return []byte(fmt.Sprintf("P5\n%d %d\n255\n", w, h))
}

// extBlock 手工拼一个合法扩展块。
func extBlock(flags byte, pix []byte) []byte {
	b := make([]byte, extSize)
	copy(b[0:], "#GTKX\n")
	b[offBitDepth] = validBitDepth
	b[offFlags] = flags
	binary.BigEndian.PutUint32(b[offChecksum:], crc32.ChecksumIEEE(pix))
	return b
}

func flipRows(w, h int, pix []byte) []byte {
	out := make([]byte, len(pix))
	for y := 0; y < h; y++ {
		copy(out[(h-1-y)*w:(h-y)*w], pix[y*w:(y+1)*w])
	}
	return out
}

func writeFile(dir, name string, body []byte) error {
	return os.WriteFile(filepath.Join(dir, name), body, 0o644)
}

func main() {
	dir, err := repoRoot("testdata", "pgmx")
	if err != nil {
		panic(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	w, h, pix := samplePix()
	flipped := flipRows(w, h, pix)
	head := stdHeader(w, h)

	files := map[string][]byte{
		// 四个开关组合的合法样例。
		"valid_off_off.bin": concat(head, pix),
		"valid_on_off.bin":  concat(head, extBlock(0, pix), pix),
		"valid_off_on.bin":  concat(head, []byte("#GTKR\n"), flipped),
		"valid_on_on.bin":   concat(head, extBlock(flagFlipped, flipped), flipped),
	}

	// 冲突样例 1：位深字段被改坏（8 -> 16），字段越界。
	badDepth := extBlock(0, pix)
	badDepth[offBitDepth] = 16
	files["bad_bitdepth.bin"] = concat(head, badDepth, pix)

	// 冲突样例 2：标志字节保留位非零（0x80），标志冲突。
	badFlags := extBlock(0, pix)
	badFlags[offFlags] = 0x80
	files["bad_flags.bin"] = concat(head, badFlags, pix)

	// 冲突样例 3：校验和字段被改坏（最后一字节取反），校验和不匹配。
	badSum := extBlock(0, pix)
	badSum[offChecksum+3] ^= 0xFF
	files["bad_checksum.bin"] = concat(head, badSum, pix)

	// 扩展块被截断：只给 8 字节。
	files["truncated_block.bin"] = concat(head, extBlock(0, pix)[:8])

	for name, body := range files {
		if err := writeFile(dir, name, body); err != nil {
			panic(err)
		}
		fmt.Println(name, len(body), "bytes")
	}
}

func concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// repoRoot 从当前工作目录向上找 go.mod，拼出仓库内目标目录，
// 避免依赖 `go run` 时的工作目录位置。
func repoRoot(elem ...string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(append([]string{dir}, elem...)...), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("找不到 go.mod")
		}
		dir = parent
	}
}
