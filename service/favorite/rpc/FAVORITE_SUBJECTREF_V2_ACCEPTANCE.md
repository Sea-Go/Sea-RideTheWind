# Favorite SubjectRef v2 新事实候选与局部验收

状态：开发分支的 **默认关闭、本机 dev/test 候选**。本页记录 Favorite 事实 owner 的新写 wire；不授权生产配置、历史行改写或在线数据迁移。运行中的 `Mode=pro|pre|rt` 配置只要声明 `SubjectRefV2Facts=true`，RPC 在打开数据库前退出 2。旧配置不声明该字段，继续使用 v1。

## 事实与交接合同

| 入口 | 来源与冻结点 | 出站合同 |
| --- | --- | --- |
| `CreateFavorite` | User RPC 确认 `GetUser(Uid)` 返回同一正整数、活动的 UID；Favorite 业务事务检查文件夹 owner 并保存目标修订 | 同一事务只保存一条 `favorite.<FavoriteId>.v1` Outbox。显式候选写外层与业务 payload 的 `schema_version=2`，`subject_ref={"issuer":"rtw.identity","subject_id":"<UID>"}`。`favorite_id/folder_id` 为无损十进制 JSON 字符串 |
| `DeleteFavorite` / 文件夹级联 | 锁住该 Favorite 建立 Outbox，与业务 owner/目标/文件夹/修订逐项核对；不借当前进程开关猜版本 | 每 Favorite 只保存一条 `favorite.<FavoriteId>.v2` 撤回 Outbox，继承建立事件的 v1 或 v2 schema。撤回只能在同一 Favorite 建立版取得 RTW 保存的 DC 技术回执后派发 |
| `cmd/fact-dispatch` | 业务事务已冻结的 `Payload` + `EventID`，`FOR UPDATE SKIP LOCKED` | 对 `/v1/events` 仅发原字节和 `Idempotency-Key=EventID`；同键未知 HTTP 结果重发原字节。DC 的 `accepted` hash、offset、receipt 和时间核对后写回旧 Outbox 行。旧 v1 超精度 JSON 数字行仍阻断，不改体重投 |
| RTW 私有权威读 | 冻结源 EventSpec、已保存的 DC receipt、源 JCS/SHA-256、活业务 owner 或撤回墓碑、同版前驱 | `/internal/v1/favorite/facts/...` 只返回 v1 三元主体；`/internal/v2/favorite/facts/...` 只返回 v2 二元主体。读口不接收调用方自报主体；两版都拒未知/未交付/改 hash/跨 owner |

v2 的 `issuer` 固定为 `rtw.identity`，`subject_id` 是 User RPC 的 RTW UID，无 `tenant_id`。旧 `platform` 仅是 v1 的固定兼容槽，不代表组织或租户。Favorite 事件沿用同一 `rtw.community.favorite` producer、建立/撤回两个 EventType 和原业务 aggregate version；外层 `schema_version=2` 明确标识新形状。旧 v1 `favoriteOutbox` 生成函数没有修改，已冻结行的 EventID、JSON 体、源 JCS hash 不迁移。

旧数据库可能已有 `favorite_item` 业务行，却没有对应 `favorite_fact_outbox` 建立事实。001 原迁移只建空 Outbox，不提供“这行是合法旧收藏还是新写故障”的证据。004 迁移只创建 `favorite_legacy_fact_marker`，**不自动批准任何旧行**；来源迁移 owner 须以已核旧快照和 `approval_ref` 明确标记 FavoriteID、UID、文件夹、目标及固定修订，并写该锚的 JCS/SHA-256。摘要输入字段固定为十进制字符串 `favorite_id,user_id,folder_id`、`target_type,target_id,target_revision`（`NULL` 写 `null`），不含可变标题/封面。删除时先锁建立事实；严格无建立且有完全匹配标记才在原业务事务只删业务行、**不造无前驱技术撤回**。有建立但坏版本/主体/目标不走 legacy；无标记或标记冲突判 `ErrFavoriteFactMigrationBlocked`，RPC 返回 `FailedPrecondition`、业务码 `5304`，业务行与文件夹保持原样。文件夹混合级联逐行判断且同事务提交/回滚，合法新收藏继续产生继承原版本的撤回，旧标记行不产事件。

上线前必须冻结本来源新写并对生产原库作**全量旧行枚举/标记/例外裁决**：已核旧快照的无建立行按004明确批准，无法证明的行留阻断；既有无前驱撤回单列裁决，不篡改冻结 Outbox。然后以只读账号运行 `FAVORITE_DATABASE_URL=<production-readonly-DSN> go run ./service/favorite/rpc/cmd/fact-legacy-preflight`，要求退出0，报告 `missing_assert=approved_legacy`、`blocked=orphan_retracts=0`，保存同一冻结水位/报告，再决定是否发布。`exit=2` 表示来源仍未裁决；本机清零报告不代表生产已裁决。发布后若来源写入改变水位，须重跑预检，不能拿旧报告放行。

## 开发分支验收

固定来源：RTW `68276bb` → 首次双版链路 `a42ab05` → 当前 legacy 标记/分类/预检 `a1e2af8`；与 BTW 双版消费候选 `0aa5f96` 同链。`SEA_DC_PLATFORM_ROOT` 和 `SEA_RTW_FAVORITE_ROOT` 指向各自**隔离开发检出**。这些脚本自行创建 PostgreSQL 16 临时实例与随机本机服务令牌；进程关闭后可独立检查 `pg_ctl status` 为 3。

```bash
SEA_DC_PLATFORM_ROOT=<isolated-DC-checkout> bash service/favorite/rpc/acceptance-dc.sh
```

2026-09-15 真隔离 PG16 + 实际 DC `cmd/platform`，从当前业务头 `a1e2af8` 运行脚本退出 0；race、`go vet`、`git diff --check` 通过。新 `TestFavoriteSubjectRefV2RealDataCenterMixedProducer` 证明 UID 1001 v2 建立和 UID 1002 原 v1 建立各得连续而不同的 DC offset/receipt；1001 在进程开关回到 v1 后撤回仍是 v2、前驱更早；v1/v2 私有读口互不冒充。改变同一个 `(producer,event_id)` 的 v2 业务体，DC 返回 409，原输入 hash/offset 不变。原版高位 Snowflake ID/旧冻结 row/hash/blocked 后续派发用例继续 PASS。真 PG16 又验证无标记阻断/业务行不动、标记旧行无事件删除、混合级联原子、错误 UID 标记拒绝、有建立坏体不得走标记、既有孤儿撤回不能用标记绕过、只读 CLI `exit2→0`。当前 `go-test.log` SHA-256 `f5c00e2ee074b39e18125f27b61751eb969613870204360221646748f5846131`；PG 停机日志 SHA-256 `ca19178a35ab4153b75b494963b66ce8243e1b94c173107c87b44d09db23652d`，停机后 `pg_ctl status` 退出3。首次链路旧头日志 SHA `83e9e1ee796b91112dfc2f7e543abb915e34436d3a5d04d155951f6fbb958e90` 只作为历史证据。

跨 BTW 联验由 BTW 仓 `cmd/worker/favorite_subjectref_v2_acceptance.sh` 创建另一个真 PG16、DC 和 RTW 服务，使用 `TestFavoriteDeliveryTwoUsersSharedAuthorityFixture` 的显式 `v2-mixed` 模式：同一 producer offset 1–3 固定为 UID 1001 v2 建立、UID 1002 v1 建立、UID 1001 v2 撤回。RTW 建立与撤回及原权威 hash/receipt PASS，RTW `rtw-test.log` SHA-256：`ab4d9d4bc54725524e7a89f27e0895a3fbfefc9497f4e3e5d9dad5264ba708d1`。对应 BTW Graph/domain/ACK 明细由 BTW 自己的验收页签收，不能把 DC 的技术接收写成 BTW 领域接收。

## 尚未签收

- v2 具体路径在本机采用活动 User RPC 同 UID 检查；本次 v2 混流固定源的 UID 用隔离 RTW 事实夹具，不是上线 UserCenter/JWT 登录、生产 UID 数据水位或部署证明。
- Producer 服务令牌属于既有 DC 技术入口；Favorite 领域事件不使用 Knowledge Search 的 HMAC `Scope` audience。搜索 v2 签发是独立合同，不等于 Favorite 事件已签名。
- BTW 收藏数仓当前 v1 `ods_event` 消费固定 `schema_version=1`，v2 新事件的持续入仓、CH/dbt/Dataset 工件新版本和生产水位仍需单独签收。客户端 H01 DC UUID↔RTW UID 的同人关联也不由 Favorite owner 猜测。
- 004 表与显式批准机制目前只有本机夹具；生产旧收藏的来源快照、完整批准清单、空/未空历史水位、既有孤儿撤回裁决、上线前只读预检报告和真实流量切换均未发生。这个 P1 门禁未解除，**不能宣称 Stage4 Favorite 源在生产具备准入**。
