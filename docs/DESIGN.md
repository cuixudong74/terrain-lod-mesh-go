# 设计文档

## 1. 噪声模型

### 基元（package `noise`）

所有基元实现 `Primitive2D.At(x, z float64) float64`，是
**(seed, 参数, 全局坐标)** 的纯函数：无查找表、无 map、无随机数生成器
状态、无 goroutine。因此"从哪个区块取样"在概念上不存在——高度只由全局
坐标决定，跨块边界天然一致。

| 基元 | 说明 | 输出范围 |
|---|---|---|
| `ValueNoise` | 哈希值噪声，五次曲线插值 | [-1, 1) |
| `PerlinNoise` | 经典梯度噪声，8 方向哈希梯度 | 约 [-1.5, 1.5] |
| `WorleyNoise` | 细胞噪声（最近特征点距离 F1） | [0, 1] |

哈希：`splitmix64` 混合器作用于 `(seed, 整数格点 x, 整数格点 z)`，
完全由整数运算构成，跨平台逐位一致。

### 分形叠加（`Fractal`）

- `Octaves`：倍频程数（>= 1）；`Lacunarity`：每倍频程频率倍率；
  `Gain`：每倍频程振幅倍率。
- 求值顺序固定：倍频程 0..N-1 顺序累加，求和顺序是契约的一部分。
- 域扭曲（`WarpConfig`）：在倍频程循环之前，用两个独立种子的值噪声场
  对输入坐标做位移（先 x 后 z，顺序固定）。`Amplitude == 0` 时禁用。

### 局部修改器（`Modifier`）

`Params.Modifiers` 是区域化局部编辑（当前为高度偏移），按切片顺序在
基础高度场之后叠加。修改器只影响与其 `Region` 相交的区块，这是区域化
增量重算的基础。

## 2. 精度与可复现性策略

**固定策略：全部中间计算使用 float64（IEEE-754 双精度），导出时一次性
舍入到 float32。**

- Go 规范允许编译器把 `a*b+c` 融合为 FMA（不同架构结果可能不同）。
  代码中所有可能被融合的中间乘积都经过显式 `float64(...)` 转换强制
  舍入，禁止跨架构结果漂移。该约定适用于 `noise` 与 `terrain` 包全部
  浮点热点路径。
- 所有跨块共享的量用**整数格点索引**计算：顶点的全局位置为
  `G * BaseSpacing`，其中 `G` 是 int64 全局格点索引
  （`G = chunkCoord * quadsPerSide(LOD0) + i * 2^lod`）。相邻块、相邻
  层级对同一共享顶点算出的 `G` 相同，因此世界坐标与高度逐位相等，
  缝合不依赖任何近似比较。
- 浮点求值顺序固定：分形按倍频程顺序累加，修改器按切片顺序叠加，
  Worley 按固定 3x3 邻域顺序扫描。
- 世界边界：`|x|, |z| < 2^40`（`MaxWorldCoord`）。界内格点运算在
  float64/int64 下精确；越界区块以 `ErrKindOutOfBounds` 拒绝。
- 并发只影响"谁先算完"，不影响任何字节：每个区块网格是
  `(Params, ChunkCoord, 邻居LOD)` 的纯函数，导出前按坐标排序。
- 黄金哈希测试（`TestGoldenHash`）锁定全管线输出的 SHA-256，
  任何意外变更都会失败。

## 3. 分块与 LOD

- 块 `(X, Z)` 覆盖 `[X*S, X*S+S)`，`S = ChunkWorldSize`。
- LOD `l` 的顶点间距为 `BaseSpacing * 2^l`；每边方块数
  `n = (S/BaseSpacing) >> l`。约束：`S/BaseSpacing` 必须为 2 的幂，
  且 `n >= 1`（在 `MaxLOD` 下仍至少一个方块）。
- `LODSelector` 把块映射到层级。内置：
  - `DistanceLODSelector(fx, fz)`：距焦点 `LOD0Radius` 内为 LOD0，
    每级半径翻倍，加 `LODBias` 后截断到 `[0, MaxLOD]`。
  - `UniformLODSelector(l)`：全部同层。
- 缝合器支持**任意**层级差（不限于相邻差 1）：细层块把朝向粗层邻居的
  边上、间距为 `2^(邻居lod - 本lod)` 的整数倍之外的顶点从三角化中吸附
  掉，退化三角形被跳过，未引用顶点被压缩移除。共享边即粗层边折线，
  无裂缝、无孔洞、无 T 型接头。`ValidateLODMap` 可供需要经典
  "相邻差 <= 1" 策略的调用方使用。

## 4. 增量重算契约

- 区块网格是 `(Params, ChunkCoord, 邻居LOD)` 的纯函数，无隐藏状态。
- `UpdateParams(region, update)`：在参数副本上应用 `update` 并校验，
  失败则整体不生效；成功后版本号递增，**与 region 相交**的缓存块被
  丢弃（下次 `Generate` 惰性重算），其余缓存块字节与对象 identity
  原样保留并直接并入新版本。
- 调用方保证 region 覆盖本次变更能影响的所有区块（种子、噪声、
  LOD 策略等全局变更用 `AllRegion()`；局部修改用 `Modifier` 并以其
  `Region` 提交）。区块顶点边界为闭区间（远端边与下一块共享），
  因此与块最小边相切的区域也会使该块失效。
- 幂等与顺序无关：最终状态只由最终参数决定。重复提交同一更新是
  无操作（参数相同则缓存全部命中）；不同顺序到达同一最终参数集时，
  逐位结果相同（有测试覆盖）。
- 增量结果与从零全量重建逐位一致（有测试覆盖）。

## 5. 并行、取消与原子性

- `Generate` 去重后按工作池并行生成，工作数由
  `Budgets.MaxChunksInFlight` 控制（0 = `runtime.NumCPU()`）。
  慢块只占用自己的 worker，不阻塞其他块。
- 导出原子性：网格先在本地完整构建，成功才进入结果集与缓存；
  任一时刻调用方拿到的都是完整区块。
- `ctx` 取消/超时：未开始的块记 `ErrKindCanceled` 错误，进行中的块
  完成后若版本已变则不写缓存；取消的块绝不进入缓存，不留残缺状态。
- 输出顺序按块坐标排序，与完成顺序、并发度无关。

## 6. 预算与有界内存

`Budgets` 各字段为 0 时表示"不限/默认"：

| 字段 | 语义 | 超限行为 |
|---|---|---|
| `MaxVerticesPerChunk` | 单块顶点硬上限 | 该块失败，`ErrKindBudgetExceeded` |
| `MaxTrianglesPerChunk` | 单块三角形硬上限 | 同上 |
| `MaxChunksInFlight` | 并行生成的块数上限 | 其余块排队等待 |
| `MaxCachedChunks` | 缓存容量（默认 4096） | FIFO 逐出；只影响命中率，不影响输出字节 |
| `MaxChunksPerRequest` | 单次 `Generate` 块数上限 | 整个请求失败，`ErrKindBudgetExceeded` |

内存上界 ≈ `MaxCachedChunks * 单块大小 + MaxChunksInFlight * 单块大小`，
与生成范围无关；大范围按需生成时内存有界。

## 7. 错误策略

所有失败为 `*terrain.Error`，携带 `Kind`、`Coord`、`Op`、`Msg`：

| Kind | 触发 |
|---|---|
| `ErrKindInvalidParam` | 参数非法/极端（非正频率、NaN、非 2 幂块比等），在 `NewWorld`/`UpdateParams` 校验期抛出 |
| `ErrKindOutOfBounds` | 区块超出 `±MaxWorldCoord` |
| `ErrKindDegenerateLOD` | 请求层级超过 `MaxLOD` 或导致每边 0 方块 |
| `ErrKindBudgetExceeded` | 顶点/三角形/请求规模超预算 |
| `ErrKindCanceled` | context 取消或超时 |

单块失败只产生对应错误项，不影响同请求其他块，更不会让整次生成崩溃；
错误可定位到具体区块与操作。

## 8. 导出格式

`WriteBinary`：魔数 `TLM1` + 块数，随后每块为坐标、LOD、顶点数、
三角形数、float32 位置（小端）、uint32 索引（小端）。写出前按
`(x, z, lod)` 排序，字节流与生成顺序无关，可直接做 SHA-256 离线校验。
`WriteOBJ` 提供同顺序的文本格式便于人工检查。
