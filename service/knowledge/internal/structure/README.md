# structure

冻结修订的 DocStructureTree 派生与 locator quote 锚定校验（M1，C42 整篇不切割的证据定位基座）。

## 职责

- `Derive(revisionID, source)`：Markdown → 结构树，纯函数、确定性（同输入同输出）；标题 ATX 1–6 级、段落=7 级、全局段落序、字节区间 `[start,end)`；`node_id = hex(sha256(revisionID‖0‖seq))[:16]`。
- `Anchor(tree, source, locator)`：按 SectionPath（逐级严格加深）+ ParaIndex（全局序，须落在解析出的 section 内）解析段落，quote 必须是该段文本子串、非空、≤200 rune；返回验证过的字符区间。标题节点永不可作为证据锚。
- `RenderModuleIndex` / `FormatLogEvent`：模块 index.md 与 append-only log.md 的确定性渲染（前缀封闭集合 ingest/compile/release/lint）。

## 边界（不能做什么）

- 不做检索：结构树只是 locator 元数据，不是检索单元（C42 红线）。
- 不做语法级 Markdown 解析（表格/引用块按普通段落文本处理； fenced code 也是段落内容）。
- 不接触存储/网络：纯函数包，无外部依赖。

## 确定性声明

同 `(revisionID, source)` 永远产出同一棵树——冻结修订可复算、可审计（验收记录需附树 SHA）。

## 验收

`go test ./service/knowledge/internal/structure/...`（含 300+ 节点唯一性/确定性/锚定拒绝用例）。接线（C-14 冻结触发、worker 化）在 A3/A4 装配里程碑完成，本包不依赖它们。
