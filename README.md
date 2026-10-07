# terrain-lod-mesh-go

程序化地形的高度场生成与多细节层次（LOD）网格导出库。纯 Go 实现，
无外部服务依赖；同一种子与参数下，任意坐标的高度只由全局坐标决定，
输出逐位可复现。

## 包结构

| 包 | 职责 |
|---|---|
| `noise` | 确定性噪声基元（Value / Gradient）、分形叠加（fBm）、域扭曲 |
| `terrain` | 高度场配置、区块网格、并发生成、缓存、增量重算、预算与错误模型 |
| `mesh` | 高度场网格化（含裙边）、共享边连续性校验、确定性 OBJ 导出 |

## 噪声模型

高度场由若干**层（Layer）**叠加而成，每层可独立配置：

- **基元**：`ValueNoise`（晶格值噪声）或 `GradientNoise`（Perlin 梯度噪声）。
  所有晶格随机性来自整数哈希（SplitMix64 混合 `seed, ix, iy`），不经过
  浮点种子，因此与平台无关。
- **分形叠加（fBm）**：`Octaves`（倍频数）、`Lacunarity`（倍频，频率逐层
  相乘）、`Gain`（增益，振幅逐层相乘）。输出按振幅和归一化到 `[-1, 1]`。
- **域扭曲**：`WarpAmp` / `WarpFreq`，先用两个独立派生种子的噪声场偏移
  采样坐标，再求值分形噪声。
- 层按切片顺序求和（顺序固定 ⇒ 确定）；`BaseHeight` 为整体偏移。
- **Modifier**：局部修改器（如山丘），贡献在半径之外**恰好为 0**，
  这是增量重算只影响局部区块的数学基础。

## 精度与确定性策略（固定，不靠运气）

1. 全部求值使用 `float64`，操作顺序在源码中固定（层按切片序、倍频按
   升序、插值先 x 后 y），不依赖 map 迭代序或 goroutine 调度。
2. 噪声求值只使用 `+ - * /`、`math.Floor`（精确）与整数哈希；
   `math.Pow`/`math.Sqrt` 在 Go 中为纯软件实现，跨平台结果一致。
3. 每个区块的网格是 `(config, grid, chunkID, skirtDepth)` 的**纯函数**：
   顶点位置由全局坐标 `Origin + i*Step` 直接算出，与区块遍历顺序、
   并发度、进程重启均无关。相邻区块共享边上的顶点由相同全局坐标算出，
   因此逐位相等。
4. 最终结果把 `-0.0` 归一化为 `+0.0`，保证导出字节稳定。
5. OBJ 导出用 `strconv.FormatFloat(f, 'g', -1, 64)`，是浮点位模式的纯
   函数，重复运行字节完全一致。

## 区块与 LOD

- `Grid{ChunkSize, BaseQuads, MaxLOD}`：区块世界边长恒定；LOD `l` 每边
  四边形数为 `BaseQuads >> l`（`BaseQuads` 必须为 2 的幂）。粗层顶点恰好
  落在细层顶点的子集上。
- **同 LOD 接缝**：共享边顶点逐位一致，无裂缝（`mesh.CheckSharedEdge`
  可校验：两边顶点集合完全相等）。
- **跨 LOD 接缝**：粗层边的每个顶点在细层边上逐位存在（可校验）；细层
  多出的中间顶点不在粗层边上，可能产生细小缝隙，由**裙边（skirt）**
  兜底——每个区块边缘向下延伸 `SkirtDepth` 的垂直裙边，保证拼接后的
  曲面无孔洞。裙边深度按请求配置（`ChunkRequest.SkirtDepth`），缺省用
  `Limits.DefaultSkirtDepth`。

## 并发、取消与完整性

- `Manager.Generate(ctx, reqs)` 用固定大小的 worker 池（`Limits.Workers`，
  `<=0` 表示串行）并行生成；**结果按请求顺序返回**，与完成顺序无关，
  因此输出不随并发度变化。
- 任一时刻只交付**完整区块**：取消/超时/失败时，已完成的区块照常返回，
  并附带 `ErrCanceled` 或定位到具体区块的 `ErrChunkFailed`；绝不出现
  半写入的区块。单个区块构建 panic 会被捕获并转换为该区块的错误，
  不会拖垮整次生成。
- 慢块不阻塞其他块：各区块独立构建，worker 空闲即取下一个任务。

## 增量重算

`Manager.UpdateConfig(newCfg)`：

- 对比新旧配置。仅 Modifier 变化时，受影响区块 = 缓存中与变化
  Modifier 新旧 footprint（圆域）相交的区块，仅这些区块被逐出缓存、
  下次访问时重算；**未受影响区块的缓存对象保持原样，绝不改写**。
- 全局参数（种子、层、BaseHeight）变化时，全部缓存区块受影响。
- 由于区块网格是全量配置的纯函数，增量重算结果与从零全量重建**逐位
  一致**；相同最终配置无论分几步提交、重复提交多少次，收敛到同一状态
  （测试 `TestUpdateIdempotentAndOrderIndependent` 覆盖）。

## 预算与边界行为（确定且有文档）

| 配置 | 行为 |
|---|---|
| `Limits.MaxVertices` / `MaxTriangles` | 单块网格超出预算 ⇒ 该请求以 `ErrBudgetExceeded` 失败（0 = 不限） |
| `Limits.MaxCachedChunks` | LRU 缓存上限，超出逐出最久未用块；内存占用有界（0 = 不缓存） |
| `Limits.Workers` | 并发上限；结果与并发度无关 |
| `Limits.WorldMin/MaxX/Z` | 请求区块越界 ⇒ `ErrOutOfBounds`，错误携带具体 `ChunkID` |
| `ChunkID.LOD > Grid.MaxLOD` | 退化 LOD 请求 ⇒ `ErrOutOfBounds` |
| 非法配置（NaN/Inf、负频率、重复 Modifier ID 等） | `NewManager`/`UpdateConfig` 返回 `ErrInvalidConfig` |

所有错误都是 `*terrain.Error`，含 `Code`（`ErrorCode`）与可选的
`Chunk` 字段，可区分失败原因并定位到具体区块与请求。

## 最小示例

```go
cfg := &terrain.Config{
    Seed: 42, BaseHeight: 2,
    Layers: []terrain.LayerConfig{{
        Primitive: noise.GradientNoise, Frequency: 0.05, Amplitude: 10,
        Fractal: noise.FractalConfig{Octaves: 4, Lacunarity: 2, Gain: 0.5, WarpAmp: 3, WarpFreq: 0.02},
    }},
}
grid := &terrain.Grid{ChunkSize: 16, BaseQuads: 16, MaxLOD: 4}
limits := &terrain.Limits{Workers: 8, MaxCachedChunks: 256,
    WorldMinX: -64, WorldMaxX: 64, WorldMinZ: -64, WorldMaxZ: 64,
    DefaultSkirtDepth: 1.0}
m, err := terrain.NewManager(cfg, grid, limits)
res, err := m.Generate(ctx, []terrain.ChunkRequest{{ID: terrain.ChunkID{X: 0, Z: 0, LOD: 0}}})
// res[0].Mesh 可直接 mesh.WriteOBJ(w, res[0].Mesh) 导出。
```

## 测试

`go test ./...` 覆盖：确定性与并发无关性、跨块/跨 LOD 接缝、增量与
全量逐位一致、更新幂等与顺序无关、取消/超时无残缺区块、预算/越界/
非法配置错误、缓存有界性、OBJ 导出确定性。
