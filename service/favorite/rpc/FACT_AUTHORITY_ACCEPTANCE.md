# 收藏事实权威读与高位标识验收

工作区域：`[W0:ROOT]` 本 RTW 独立工作树；`[W1:WRITE]` `service/favorite/rpc` 和本分支 `go.mod/go.sum`；`[R1:READ_ONLY]` DC eventing 与 BTW FactWorker 合同、RTW article 服务；`[D1:DEPENDENCY]` 锁定版 JSON Canonicalization 模块；`[G1:GENERATED]` RPC pb 文件不手改；`[X1:EXTERNAL]` 验收脚本自建隔离 PostgreSQL 16、真实 DC `cmd/platform`；`[N1:OUT_OF_SCOPE]` 原业务脏树、其他服务、Docs、生产环境；`[T1:TEMP]` 验收脚本的随机临时目录。主职责 `[C4:PERSISTENCE]` 是冻结 Outbox/回执的权威读取，跨 `[C1:TRANSPORT]` 私有 HTTP、`[C7:CONTRACT]` DC JCS 输入哈希、`[C8:VERIFY]` 真实协议与数据库验收。

## 事实及读口合同

收藏域仍在原业务事务中写入 `favorite_fact_outbox`。新建事件的 `payload.favorite_id` 和 `payload.folder_id` 是无损十进制 **JSON 字符串**；外层 `aggregate_id`、`event_id`、`operation_id`、`source_ref` 不变。DC 使用 JCS 规范化整个 EventSpec 后计算 SHA-256；这修复真实 Snowflake ID 超过 2^53 时 JSON 数字被拒绝的问题。已冻结 Outbox 不重写，已经接纳的小整数 JSON 数字事件仍可按原哈希读取。

私有读进程 `cmd/fact-authority` 仅在三个配置均显式存在时启动：`FAVORITE_DATABASE_URL`、`FAVORITE_AUTHORITY_LISTEN`、长度至少 32 字节的 `FAVORITE_AUTHORITY_TOKEN`。它不挂到公开 favorite API，也不输出文章正文或密钥。调用方仅能使用单个 `Authorization: Bearer <service token>` 发起：

```http
GET /internal/v1/favorite/facts/rtw.community.favorite/favorite.9007199254740995.v2
```

`200` 正文含 `event`（源 Outbox 冻结完整 EventSpec）、`subject_ref`（RTW 构造并核过的 `rtw.identity/platform/<UID>`）、撤回时的 `predecessor_event_id`、`technical_receipt`（RTW 已提交的 DC `accepted` 回执及 `receipt_id/input_hash/offset/received_at`）、`source_event_hash`（对源 EventSpec 同版 JCS/SHA-256）。建立事实无 `predecessor_event_id`。同键只读重投返回同一业务事实和技术回执；调用方不传主体、目标或 hash，因此无法覆盖 RTW 的主体归属。

响应形状示例（hash 与回执 ID 为示意值，实际返回必须和 DC 回执一致）：

```json
{
  "event": {
    "event_id": "favorite.9007199254740995.v2",
    "event_type": "rtw.favorite.retract",
    "schema_version": 1,
    "producer": "rtw.community.favorite",
    "aggregate_id": "9007199254740995",
    "aggregate_version": 2,
    "operation_id": "favorite.9007199254740995.v2",
    "occurred_at": "2026-09-14T16:00:00Z",
    "payload": {
      "schema_version": 1,
      "event_id": "favorite.9007199254740995.v2",
      "subject_ref": {"authority_id": "rtw.identity", "tenant_id": "platform", "subject_id": "1001"},
      "target_type": "article",
      "target_id": "article-snowflake",
      "target_revision": null,
      "operation": "retract",
      "source_ref": "rtw.favorite/9007199254740995",
      "event_time": "2026-09-14T16:00:00Z",
      "available_at": "2026-09-14T16:00:00Z",
      "favorite_id": "9007199254740995",
      "folder_id": "9007199254740993"
    }
  },
  "subject_ref": {"authority_id": "rtw.identity", "tenant_id": "platform", "subject_id": "1001"},
  "predecessor_event_id": "favorite.9007199254740995.v1",
  "technical_receipt": {
    "event_id": "favorite.9007199254740995.v2",
    "producer": "rtw.community.favorite",
    "technical_status": "accepted",
    "receipt_id": "example-receipt",
    "input_hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "offset": 4,
    "received_at": "2026-09-14T16:00:01Z"
  },
  "source_event_hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}
```

读口只在源行 `status=1`、DC 回执字段齐全且 `source_event_hash == technical_receipt.input_hash` 时返回；校验事件 ID/producer/版本/操作、完整主体、目标及来源引用。存活的建立事实还要和 `favorite_item` 业务所有者/目标一致；业务行删除后的建立事实必须有同 ID 撤回 Outbox。撤回必须有同 ID、较早 DC offset 的建立前驱，主体、目标、文件夹及来源不变，且业务行已删除。未知/错误 producer、未投递、旧身份来源、主体冲突、哈希篡改统一 `404`；缺令牌或重复授权头 `401`；数据库故障 `503`。响应禁止缓存。

BTW Binder 应用 `producer,event_id` 向此 RTW 读口查源，再逐字段核对 DC batch 中的 EventSpec、producer、event_id、输入 hash、offset、receipt_id、received_at；仅使用 RTW 回的 `subject_ref` 绑定事实。DC 的技术接纳或 RTW 的读口成功 **不是** BTW 事实接纳、DWD 入仓或用户特征生效。撤回按 `predecessor_event_id` 关联已验建立事实。

## 已冻结历史与迁移边界

迁移前先检查 `favorite_fact_outbox` 中 `jsonb_typeof(payload->'payload'->'favorite_id')` 和 `jsonb_typeof(payload->'payload'->'folder_id')`。旧版安全范围内的数字事件按原 EventSpec/hash 继续投递和读取。超过 2^53 的旧版数字事件，派发器在本地校验阶段阻断、不调用 DC、不修改 payload/event_id/版本；它只把技术状态置为 `3=blocked`，记录结构化 `favorite.delivery.blocked`/`INVALID_FROZEN_ENVELOPE`，后续合法行仍可派发。已失败的旧行也不得直接改写为字符串后按同一个事件 ID 重投。需要先查 DC `(producer,event_id)` 回执确认是否有既存输入，再单独制定新的事件版本/迁移合同和 BTW 兼容规则；当前实现不自动修复历史行。原库执行 `001_favorite_fact_outbox.sql` 与 `002_favorite_fact_delivery.sql` 后才可启用读口。

## 本分支验收

- 修复前，`favorite_id=9007199254740995`、`folder_id=9007199254740993` 的真实 RTW→DC `cmd/platform` 请求返回 HTTP `400 INVALID_JSON`；隔离证据目录：`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-favorite-dc.13C9j3`。
- 修复后，`SEA_DC_PLATFORM_ROOT=<隔离 DC 工作树> bash service/favorite/rpc/acceptance-dc.sh` 通过：高位建立与撤回各获不同 DC 回执及连续 offset，固定事件重投不增新事件；独立 RTW HTTP 进程在接纳前 `404`、接纳后 `200` 并与 DC 原 hash/receipt/received_at 对齐，未知/错 producer/无令牌/篡改/跨用户或旧来源夹具均拒绝，缺配置的读进程无法启动。旧高位数字事件被标为 `blocked` 后，同批后继合法事件仍由真实 DC 接纳为 offset 5；最终证据目录：`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-favorite-dc.MTEAX9`。
- `bash service/favorite/rpc/acceptance.sh` 在隔离 PG16 上 `-race -count=1` 与 vet 退出 `0`，覆盖跨用户、旧来源、撤回主体冲突、小整数旧事件兼容、高位旧数字阻断且 Outbox 不改写、后继事件继续派发；最终证据目录：`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-favorite-fact.aEq8Fq`。

文章公开边界仍待交接：当前 `resolveArticleSnapshot` 调 `ArticleRpc.GetArticle(ArticleId, IncrView:false)`，基线 `GetArticleRequest` 没有可证明公开修订的 `PublicOnly` 字段。应在文章域公开读 RPC 新字段集成后给收藏快照加公开门禁，不从 `status` 猜测冻结 r1。本分支未改文章或生成 pb。
