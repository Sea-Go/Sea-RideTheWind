# freeze

M1 接线骨架：把 structure/citation/release 三个纯域包串成
**修订冻结 → 结构树派生 → 发布事件** 的最小可运行链（C-14 骨架）。
存储全部走接口注入，本包自带内存实现；PG 落库在装配里程碑接真实存储。

## 职责

- **`Freeze(ctx, fr) (treeSHA, error)`**：校验修订封套（标识/DocKey/64 位小写 hex
  内容指纹）→ 核对 `ContentSHA256 = hex(sha256(Source))`（不符即拒）→
  `structure.Derive` → 渲染 canonical tree JSON → `treeSHA = hex(sha256(canonical))` →
  存入 StructureStore 与 RevisionStore。同 revID 重放幂等：已存树 SHA 与重算一致
  即直接返回；同 revID 换内容（树 SHA 漂移）按不可变修订报 `ErrConflict`。
- **`Release(ctx, moduleID, releaseID, docs, at) (eventID, error)`**：逐 doc Freeze →
  组 C-1 v2 事件（`structure_ref = treeSHA`，`s3_ref = "sha256:<ContentSHA256>"`
  占位约定，`char_len` 为源字节长度，与结构树字节区间同口径）→ Validate →
  Append 进 Outbox（`release.MemoryStore`）。docs 顺序原样保留——顺序是发布事实。
- **`Accept(ctx, req, revID) (Receipt, error)`**：从 StructureStore 取 canonical
  字节并重核 SHA → 严格解析还原 `structure.Tree`（校验树归属 revID）→ 从
  RevisionStore 取源字节并重核内容指纹 → `citation.Accept` 做 C-4 quote 命中校验。
  回执 `TreeSHA` 与冻结时 treeSHA 强一致校验，不一致即硬错误。

## eventID 确定性派生（本链的约定）

`event_id` 字段本身在 canonical 字节里，不能自引用。约定：**以 `event_id` 为空的
规范形作为派生前像**——`CanonicalJSON(event{EventID:""})` →
`EventIDFromCanonical(前像)` 得到 id → 回填 `EventID` → Validate → Append。
因此同 `(module_id, release_id, docs 及其顺序, published_at 同一时刻)`
永远产出同一 eventID（时区不同、表示不同不算不同输入），重放对 Outbox 是幂等
no-op（Append 按 event_id 去重），符合 C-1 幂等键语义。

## canonical tree 序列化（复用 citation 的思路）

字段固定序、紧凑单行（先 revision 头、再按文档序每节点一行）、`\n` 换行、
最小字符串转义（仅引号/反斜杠/控制字符）、手写不依赖 `encoding/json` 的实现
行为。本包与 citation 包是同一文档格式的两个独立实现，
`Release→Accept` 往返测试钉死二者：freeze 的 treeSHA 必须等于 citation 回执的
`TreeSHA`（字节级一致，漂移即测试失败）。配套**严格解析器**
`parseCanonicalTree`：只接受 writer 产出的精确行文法（键序、最小转义、无尾随
杂质、`\u` 不含代理对），`parse(canonical(t))` 与 `t` 深相等。

## 接缝（存储注入）

| 接口 | 契约要点 | 内存实现 | 生产实现（后续） |
| --- | --- | --- | --- |
| `RevisionStore{Load,Save}` | Save 同值幂等；同 revID 异内容 `ErrConflict` | `MemoryRevisionStore` | PG 修订表 |
| `StructureStore{SaveTree,LoadTree}` | 只收 canonical 字节且 sha 匹配；幂等/冲突 | `MemoryStructureStore` | PG `structure_*`（A4） |
| `release.Store`（Append/Pending/MarkSent） | Append 幂等、Pending 保序 | `release.MemoryStore` | PG outbox（A5） |

内存实现并发安全（mutex），返回防御性拷贝；`ErrNotFound`/`ErrConflict` 为
哨兵错误，PG 实现须以 `errors.Is` 可判别的方式返回。

## 与契约/里程碑的对应

- **C-14（A3→A4 冻结触发 `{revision_id, s3_ref}`）**：`Freeze` 是触发的最小
  处理端——校验内容指纹、派生结构树、以 treeSHA 落 StructureStore。M1 骨架里
  触发者是本地调用（worker/测试）；“派生完成回写 A3 后修订才可进入发布”的
  门禁由 `Release` 内部先 Freeze 全部 docs 近似，真实回写门禁随 PG 装配补齐。
- **C-1（发布事件 v2 唯一写者）**：`Release` 组装 v2 字段并写 Outbox；
  eventID 确定性派生（见上），下游按 event_id 幂等消费。
- **C-2（READY 回执/双缓冲）**：不在本链——本链止于 C-1 事件；事件里的
  `structure_ref`/`content_sha256` 即下游 C3 工件化后回 READY 时可比对的
  指纹，M2 接。
- **C-4（引用接纳）**：`Accept` 从存储还原树与原字节后委托
  `citation.Accept`，回执 TreeSHA 与冻结指纹强一致。

## dev 入口：freeze-worker

```sh
GOCACHE=/tmp/gocache-m1w go run ./service/knowledge/cmd/freeze-worker \
  --input service/knowledge/cmd/freeze-worker/testdata/sample-freeze.jsonl --accept
```

读 JSONL 冻结请求（每行一次发布：`module_id/release_id/published_at(RFC3339，
缺省用当前 UTC 时间，会改变 eventID)/docs[]{revision_id,doc_key,content_sha256
(可省略由 worker 计算),source_b64}`），逐 doc Freeze 并打印 treeSHA，发布后打印
eventID；`--accept` 附一条样例 locator 引用接纳：从冻结文档派生树、取首个段落
的真实前缀作 quote（含 SectionPath/ParaIndex），走 `Accept` 打印回执。

## 边界（不能做什么）

- 不落 PG/S3：`s3_ref` 是 `sha256:<hash>` 占位约定，真实对象引用随对象存储
  装配替换；Outbox 用内存实现，无 Dispatch 重试策略。
- 不重写三个域包的语义：只做编排、指纹与存储边界校验；校验规则以域包为准。
- Freeze 的“重放直接返回”只覆盖“树已存且 SHA 一致”路径；首写中途崩溃留下的
  半状态由重放整个 Freeze 补齐（Save 幂等）。
- 不提供并发写同一新修订的分布式互斥：内存实现以 mutex 串行化，PG 里程碑用
  唯一约束/事务兜底。

## 验收

```sh
GOCACHE=/tmp/gocache-m1w go test -race -count=1 \
  ./service/knowledge/internal/freeze/... ./service/knowledge/cmd/freeze-worker/...
```

含：3 文档 eventID 确定性（跨服务实例/时区表示/重放同 id、顺序与 release_id
敏感）、Outbox 不重复、SHA 不匹配拒绝、重放幂等与分歧冲突、Release→Accept
真 quote 往返（回执 TreeSHA == 冻结 treeSHA、锚定区间回读原字节）、canonical
往返与严格解析拒绝族（转义/代理对/前导零/键序/语义界）、内存存储幂等/冲突/
拷贝隔离/并发。
