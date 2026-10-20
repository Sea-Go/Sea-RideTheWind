# release

发布域的纯域层：C08 门禁状态机 + C-1 事件唯一写者的事件契约与 Outbox 投递语义（无 DB）。

## 职责

- **事件契约（契约快照 v2）**：`Event`/`EventDoc`，JSON tag 全 snake_case，字段序固定为
  `event_id, module_id, release_id, published_at(RFC3339), docs:[{doc_key, revision_id, s3_ref, structure_ref, content_sha256, char_len}]`；
  `Validate()` 校验标识非空、docs≥1、doc_key 符合 `source|page|summary:<uuid>@<rev>` 正则、
  content_sha256 为 64 位小写 hex、published_at 非零、引用字段非空。
- **确定性派生**：`CanonicalJSON(e)` 固定字段序、无缩进、docs 保持原序（顺序即发布事实），
  published_at 规范化为 UTC；`EventIDFromCanonical(canonical)` = `hex(sha256(canonical))` 全长。
  event_id 本身由调用方生成（C-1 唯一写者），本包只提供跨轨可比对的确定性派生。
- **门禁状态机**：`Transition(r, to, at)` 唯一合法迁移表 draft→confirmed、confirmed→published
  （记 PublishedAt，要求 at 非零）、published→rolled_back（终态）；重复发布、跳级、终态复活一律非法。
  值语义：返回新 Release，非法迁移返回错误且原样返回入参。
- **Outbox**：`Store` 接口（Append/Pending/MarkSent，Append 幂等）+ 内存实现 `MemoryStore`
  （插入序 + 已发送集合，并发安全）+ `Dispatch(st, send)` 按序发送、成功即 MarkSent、
  失败即停返回已发数，下次续传（at-least-once：MarkSent 失败的条目会被重发）。

## 边界（不能做什么）

- 不接触 DB/网络：`Store` 只是契约，生产实现（如 Postgres outbox 表）落在装配层。
- 不持时钟：所有时间（published_at、迁移 at）由调用方注入，便于测试与重放。
- 不生成 event_id：唯一写者在调用侧；本包仅提供从规范字节的确定性指纹。
- 不做投递重试策略/退避：只提供"失败即停 + 续传"的语义骨架。

## 确定性声明

同一 `Event` 值永远产出同一规范字节与同一 `EventIDFromCanonical` 结果；
同一时刻的不同时区表示（+08:00 vs UTC）归一为相同规范字节。
状态机迁移表为封闭常量，可穷举验证（4×4 全组合仅 3 条合法路径）。

## 验收

`GOCACHE=/tmp/gocache-s2 go test -race -count=1 ./service/knowledge/internal/release/...`
（含 Validate 正反例、canonical 字节级快照、16 种迁移组合、Outbox 幂等/续传/并发用例）。
