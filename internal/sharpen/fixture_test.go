package sharpen

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFixtures 生成三档对比度的 PGM 夹具（不参与常规断言）。
func TestWriteFixtures(t *testing.T) {
	if os.Getenv("WRITE_FIXTURES") == "" {
		t.Skip("set WRITE_FIXTURES=1 to regenerate")
	}
	dir := filepath.Join("testdata", "fixtures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range DefaultEdgeCases() {
		body := append([]byte(fmt.Sprintf("P5\n%d %d\n255\n", e.Width, e.Height)), e.Render()...)
		if err := os.WriteFile(filepath.Join(dir, e.Name()+".pgm"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
