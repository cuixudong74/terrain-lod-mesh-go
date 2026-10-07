# terrain-lod-mesh-go

程序化地形的高度场生成与多细节层次（LOD）网格导出库，纯 Go、零外部依赖、
全流程离线。同一片确定性世界可按区块、按细节层次按需生成，导出的网格可直接
用于离线校验与后续渲染。

## 能力概览

- **确定性噪声场**：值噪声 / Perlin 梯度噪声 / Worley 细胞噪声三种基元，
  分形叠加（倍频 lacunarity、增益 gain、倍频程数）与域扭曲（domain warp）
  全部可配置。噪声是全局坐标的纯函数，跨区块边界采样结果逐位一致。
- **逐位可复现**：固定 float64 精度策略（见
  [docs/DESIGN.md](docs/DESIGN.md)），结果与并发度、调度顺序、区块遍历顺序、
  进程重启无关。黄金哈希测试锁定全管线输出。
- **多细节层次与无缝拼接**：LOD 0 最细，每粗一级顶点间距翻倍。相邻块层级
  不同时，细层块在共享边上吸附到粗层顶点间距（stitching），共享边顶点逐位
  重合，曲面在每条共享边上连续且可被自动化校验。
- **增量重算**：`UpdateParams` 按区域失效缓存，未受影响区块的字节与对象
  identity 保持不变；增量结果与从零全量重建逐位一致；更新顺序与重复提交
  不改变最终结果。
- **并行生成与取消**：工作池并行，任一时刻只导出完整区块；context 取消/
  超时不会留下残缺区块或脏缓存；慢块不阻塞其他块。
- **预算与有界内存**：顶点/三角形/在飞块数/缓存容量/单请求块数均可配置，
  超限行为确定（见设计文档）。
- **可区分错误**：所有失败携带 `ErrorKind`（参数非法 / 越界 / 退化 LOD /
  预算超限 / 取消）与具体区块坐标，单个块失败不影响其他块。

## 快速开始

```go
p := terrain.Params{
    Seed:           1234,
    HeightScale:    12,
    Noise: noise.Fractal{
        Source:     noise.PerlinNoise{Seed: 1234, Frequency: 0.05},
        Octaves:    4, Lacunarity: 2, Gain: 0.5,
        Warp:       noise.WarpConfig{Amplitude: 2.5, Frequency: 0.02, Seed: 77},
    },
    ChunkWorldSize: 16,   // 每块边长（世界单位）
    BaseSpacing:    1,    // LOD0 顶点间距
    MaxLOD:         3,
    LOD0Radius:     24,
}
w, err := terrain.NewWorld(p, terrain.DistanceLODSelector(focusX, focusZ))
meshes, errs := w.Generate(ctx, coords)   // 并行、可取消、结果排序确定
var buf bytes.Buffer
terrain.WriteBinary(&buf, meshes)         // 逐位可复现的二进制导出
```

命令行离线导出（相同参数重跑字节完全一致）：

```sh
go run ./cmd/terrain-export -seed 1234 -chunks 8 -out terrain.tlm
# 输出文件路径与 sha256 摘要
```

## 测试

```sh
go test ./...          # 单元测试
go test -race ./...    # 并发竞态
```

覆盖：噪声与网格确定性、并发度无关性、同层/跨层接缝、增量与全量一致、
更新幂等与顺序无关、取消不残留、缓存有界、错误分类、黄金哈希。

详细设计（噪声模型、精度策略、缝合算法、配置项、错误与预算语义）见
[docs/DESIGN.md](docs/DESIGN.md)。
