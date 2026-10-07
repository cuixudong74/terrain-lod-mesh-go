package noise

import (
	"math"
	"sync"
	"testing"
)

func testPrimitives() map[string]Primitive2D {
	return map[string]Primitive2D{
		"value":  ValueNoise{Seed: 42, Frequency: 0.5},
		"perlin": PerlinNoise{Seed: 42, Frequency: 0.5},
		"worley": WorleyNoise{Seed: 42, Frequency: 0.5},
	}
}

func TestPrimitiveDeterministic(t *testing.T) {
	for name, p := range testPrimitives() {
		for i := 0; i < 1000; i++ {
			x := float64(i)*0.37 - 200.0
			z := float64(i)*0.91 + 57.0
			a := p.At(x, z)
			b := p.At(x, z)
			if a != b {
				t.Fatalf("%s: repeated eval differs at %d: %v vs %v", name, i, a, b)
			}
			if math.IsNaN(a) || math.IsInf(a, 0) {
				t.Fatalf("%s: non-finite at %d", name, i)
			}
		}
	}
}

func TestPrimitiveConcurrentOrderIndependent(t *testing.T) {
	// Evaluate the same coordinate set in shuffled order across
	// goroutines; results must be bitwise identical to sequential eval.
	pts := make([][2]float64, 512)
	for i := range pts {
		pts[i] = [2]float64{float64(i)*1.7 - 400, float64(i)*0.3 + 11}
	}
	for name, p := range testPrimitives() {
		want := make([]float64, len(pts))
		for i, pt := range pts {
			want[i] = p.At(pt[0], pt[1])
		}
		got := make([]float64, len(pts))
		var wg sync.WaitGroup
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				// Each worker walks the slice in a different stride.
				for k := 0; k < len(pts); k++ {
					i := (k*8 + w) % len(pts)
					got[i] = p.At(pts[i][0], pts[i][1])
				}
			}(w)
		}
		wg.Wait()
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: concurrent eval differs at %d", name, i)
			}
		}
	}
}

func TestFractalDeterministicAndWarped(t *testing.T) {
	f := Fractal{
		Source:     PerlinNoise{Seed: 7, Frequency: 0.25},
		Octaves:    5,
		Lacunarity: 2.0,
		Gain:       0.5,
		Warp:       WarpConfig{Amplitude: 3.0, Frequency: 0.1, Seed: 99},
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		x := float64(i)*0.53 - 100
		z := float64(i)*0.29 + 40
		if f.At(x, z) != f.At(x, z) {
			t.Fatalf("fractal not deterministic at %d", i)
		}
	}
	// Warp must actually change the field.
	fw := f
	fw.Warp = WarpConfig{}
	diff := 0
	for i := 0; i < 100; i++ {
		x := float64(i) * 0.71
		if f.At(x, x) != fw.At(x, x) {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("domain warp had no effect")
	}
}

func TestFractalValidate(t *testing.T) {
	bad := []Fractal{
		{Source: nil, Octaves: 1, Lacunarity: 2},
		{Source: ValueNoise{Seed: 1, Frequency: 1}, Octaves: 0, Lacunarity: 2},
		{Source: ValueNoise{Seed: 1, Frequency: 1}, Octaves: 1, Lacunarity: 0},
		{Source: ValueNoise{Seed: 1, Frequency: 1}, Octaves: 1, Lacunarity: 2, Gain: -1},
		{Source: ValueNoise{Seed: 1, Frequency: 1}, Octaves: 1, Lacunarity: 2,
			Warp: WarpConfig{Amplitude: 1, Frequency: 0}},
	}
	for i, f := range bad {
		if err := f.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestGlobalCoordinateConsistency(t *testing.T) {
	// Sampling the same global coordinate "from different chunks"
	// (i.e., simply evaluating at global coords) must agree exactly;
	// there is no chunk-local state in the noise field.
	p := ValueNoise{Seed: 123, Frequency: 0.125}
	for _, x := range []float64{0, 16, 32, -16, 4096.5, -1024.25} {
		for _, z := range []float64{0, 16, -16, 2048.75} {
			if p.At(x, z) != p.At(x, z) {
				t.Fatalf("inconsistent at (%v,%v)", x, z)
			}
		}
	}
}
