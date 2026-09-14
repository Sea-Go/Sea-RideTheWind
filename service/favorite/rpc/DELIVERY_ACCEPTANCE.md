# 收藏领域事实到 DataCenter 的技术接纳

此切片从 RTW 已提交的 `favorite_fact_outbox` 出发，只交付 H09.a 的 **RTW 来源 → DC 技术接纳**。前置业务事务与收藏正反事实由 WS02-B 提供；DataCenter 不推断收藏含义，BTW 用户事实领域接纳仍需 RTW 权威主体与 EventSpec Binder。

## 部署和交接

先在现有 Favorite PostgreSQL 库执行 `internal/model/001_favorite_fact_outbox.sql`，再执行 `internal/model/002_favorite_fact_delivery.sql`。后者增加 DC 回执列，并拒绝修改已生成的事件身份、版本和 payload。`FavoriteFactOutbox` 的 `status=0` 为待交付，`2` 为失败待重试，`1` 仅表示 DC 已持久技术接纳；`status=1` **不代表** BTW 事实已接纳、数仓已覆盖或某内容已经发布。

独立进程 `cmd/fact-dispatch` 需要 `FAVORITE_DATABASE_URL`、`DC_PLATFORM_EVENT_URL`（完整 `/v1/events` URL）和 `DC_PLATFORM_SERVICE_TOKEN`。`-once` 仅处理至多一条，默认每秒扫描，每批至多 16 条。进程按 `status IN (0,2)` 与 `FOR UPDATE SKIP LOCKED` 领单条；同一收藏的撤回版只在建立版已有 RTW 落库的 DC 技术回执后才可领取，避免并发跳锁让撤回先占 DC offset，阻塞 BTW 的前驱依赖。只发送业务事务内冻结的 JSON 和 `Idempotency-Key=event_id`。DC 返回 201/200 后，还必须核对 `event_id/producer/technical_status=accepted/receipt_id/input_hash/offset/received_at`，才在本地同一事务记 `delivered_at`、DC 回执 ID/hash/offset。HTTP 超时或回执不匹配只增加失败次数，保留原始事件供下轮同键同体重投。技术状态可在 DC 的 `GET /v1/events/{producer}/{event_id}` 权威查询；DC 的 `GET /v1/event-consumers/{consumer}/events` 与 `POST .../ack` 留给 BTW/数仓消费者，不由 RTW 代 ACK。

若 DC 已接纳但 HTTP 响应丢失，RTW 仍保持未交付并重发原事件；DC 根据 `(producer,event_id,input_hash)` 返回同一回执，不增加 offset。相同 `(producer,event_id)` 的不同规范化输入由 DC 409 拒收。RTW 本地冻结 payload 的触发器使实际派发不可能自行修改事件后尝试覆盖。

## 本地验收

从本隔离 RTW checkout 运行：

```bash
SEA_DC_PLATFORM_ROOT=/path/to/isolated/DataCenter service/favorite/rpc/acceptance-dc.sh
```

脚本创建随机端口的独立 PG16，编译并启动真实 DC `cmd/platform -migrate`、真实 RTW `cmd/fact-dispatch -once`，再跑 Favorite 范围的 Go race、vet 与 diff 检查。`TestFavoriteDeliveryRealDataCenterTechnicalReceipt` 验证收藏 assert 从本地 Outbox 到 DC offset 1；DC 接收后代理丢掉 HTTP 成功回执时 RTW 状态仍为失败；按 `GET receipt` 证实 DC 已接纳；相同 event ID 的错 payload 409 且回执不变；原事件重投只取得原 offset 1；撤回事件在 DC 获 offset 2，两条本地记录各保存独立回执。另有并发派发、锁住 assert 时 retract 不得跳锁抢 offset、错误回执、业务事务失败回滚测试。

2026-09-14 实跑上述脚本退出 0；完整记录位于脚本最后打印的临时 Evidence directory。此验收不使用线上 RTW/DC，不含真实服务凭据，不开启生产调度。BTW `TrustedFactBinder`、正式 H09.a 领域验收、DC producer 级签发及 Collector 查询仍未完成，完整 H09/OBS 状态保持 `PARTIAL`。
