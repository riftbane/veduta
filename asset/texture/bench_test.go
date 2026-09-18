package texture

import (
	"os"
	"testing"
)

// BenchmarkExample compiles the 256×256, 8-layer example of spec §8.2 (with mips).
func BenchmarkExample(b *testing.B) {
	data := mustReadB(b, "example")
	opt := Options{FS: os.DirFS(testdataDir(b))}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse("example.vtex", data, opt); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMaxSize compiles the example at the largest size, 4096×4096.
func BenchmarkMaxSize(b *testing.B) {
	data := []byte(`{"veduta": "texture/1", "size": [4096, 4096], "tiling": true, "layers": [
		{ "type": "solid", "color": "#8a5a2b" },
		{ "type": "noise", "seed": 7, "scale": 16, "octaves": 8, "color": "#000000", "opacity": 0.25, "blend": "multiply" },
		{ "type": "stripes", "width": 8, "angle_deg": 30, "colors": ["#00000000", "#00000030"] },
		{ "type": "rect", "xy": [256, 256], "size": [3584, 3584], "color": "#ffffff20", "corner": 96, "outline": 32 },
		{ "type": "circle", "center": [2048, 2048], "radius": 640, "color": "#ff0000" },
		{ "type": "gradient", "from": "#ffffff", "to": "#000000", "angle_deg": 45, "opacity": 0.3 },
		{ "type": "image", "path": "textures/src/logo.png", "fit": "cover", "opacity": 0.5 }
	]}`)
	opt := Options{FS: os.DirFS(testdataDir(b))}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse("big.vtex", data, opt); err != nil {
			b.Fatal(err)
		}
	}
}

func mustReadB(b *testing.B, name string) []byte {
	b.Helper()
	data, err := os.ReadFile(testdataDir(b) + "/textures/" + name + ".vtex")
	if err != nil {
		b.Fatal(err)
	}
	return data
}
