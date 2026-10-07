// Command terrain-export generates a rectangular region of chunks and
// writes a deterministic binary mesh file plus its SHA-256 digest, for
// offline validation. Re-running with the same flags always produces
// byte-identical output.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"

	"github.com/cuixudong74/terrain-lod-mesh-go/noise"
	"github.com/cuixudong74/terrain-lod-mesh-go/terrain"
)

func main() {
	var (
		seed    = flag.Uint64("seed", 1234, "world seed")
		chunks  = flag.Int("chunks", 8, "chunks per side")
		out     = flag.String("out", "terrain.tlm", "output file")
		focusX  = flag.Float64("focus-x", 0, "LOD focus x")
		focusZ  = flag.Float64("focus-z", 0, "LOD focus z")
		maxLOD  = flag.Uint("max-lod", 3, "maximum LOD level")
		chunkSz = flag.Float64("chunk-size", 16, "chunk world size")
		spacing = flag.Float64("spacing", 1, "LOD0 vertex spacing")
	)
	flag.Parse()

	p := terrain.Params{
		Seed:        *seed,
		HeightScale: 12,
		Noise: noise.Fractal{
			Source:     noise.PerlinNoise{Seed: *seed, Frequency: 0.05},
			Octaves:    4,
			Lacunarity: 2,
			Gain:       0.5,
			Warp:       noise.WarpConfig{Amplitude: 2.5, Frequency: 0.02, Seed: *seed ^ 77},
		},
		ChunkWorldSize: *chunkSz,
		BaseSpacing:    *spacing,
		MaxLOD:         terrain.LOD(*maxLOD),
		LOD0Radius:     *chunkSz * 1.5,
	}
	w, err := terrain.NewWorld(p, terrain.DistanceLODSelector(*focusX, *focusZ))
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	var coords []terrain.ChunkCoord
	for x := 0; x < *chunks; x++ {
		for z := 0; z < *chunks; z++ {
			coords = append(coords, terrain.ChunkCoord{X: int32(x), Z: int32(z)})
		}
	}
	meshes, errs := w.Generate(context.Background(), coords)
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "chunk error:", e)
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := terrain.WriteBinary(f, meshes); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := f.Seek(0, 0); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if err != nil {
			break
		}
	}
	fmt.Printf("wrote %d meshes to %s\nsha256: %x\n", len(meshes), *out, h.Sum(nil))
	if len(errs) > 0 {
		os.Exit(1)
	}
}
