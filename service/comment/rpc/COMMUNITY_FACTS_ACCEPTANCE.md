# WS02-B 评论与点赞领域事实：隔离验收

## 工作区域与责任

- `[W0:ROOT]` 本仓库的独立工作树 `sea-rtw-community-interaction-20260914`，起点为已推送的 `origin/feat/knowledge-service-20260914` `1a8aa0b`。
- `[W1:WRITE]` `service/comment/rpc/internal/{model,logic}`、`service/like/rpc/internal/{model,mq}`，两处精确迁移、相邻测试及本验收记录。
- `[R1:READ_ONLY]` Sea-Docs H09.a/WS02-B、DC 当前事件接收实现、RTW 其他服务和原始业务工作树。
- `[D1:DEPENDENCY]` 当前 `go.mod` 固定的 GORM、go-zero、pgx 版本；`[G1:GENERATED]` RPC `pb` 和 `server` 代码未手改。
- `[X1:EXTERNAL]` 仅写入脚本自建的隔离 PostgreSQL 16；不写共享 DC、Kafka、Redis 或线上数据库。
- `[N1:OUT_OF_SCOPE]` 文章、收藏、正式知识证据索引、DC 投递与 BTW 接纳；`[T1:TEMP]` `mktemp` 创建的隔离 PG 数据目录。
- 主职责 `[C4:PERSISTENCE]`，跨 `[C3:DOMAIN]` 状态迁移、`[C2:APPLICATION]` MQ 确认、`[C7:CONTRACT]` H09.a 载荷、`[C8:VERIFY]` PG 验收。

## 源事实合同与真实提交点

评论的 `CreateComment` RPC 只把请求发入 Kafka。`AuditConsumer` 在 `InsertCommentTx` 内同时写评论、计数与 `comment_domain_fact_outbox`，成功提交才有 `community.comment.created`。审核状态 `visibility_state=1` 表示待审核，不表示公开可见。`DeleteCommentTx` 在同一个 PG 事务里锁住评论，校验目标与操作者，仅首次从非删除状态转为删除时扣除计数并写 `community.comment.deleted`；删除事实的 `subject_ref` 指原评论作者，`operator_ref` 指实际删除者。`LikeCommentTx` 以 `(user_id,comment_id)` 行锁串行化，在 0/1/2 状态真正改变时同时写评论点赞/点踩事实，重复操作不产生新事实。

文章/目标点赞 RPC 目前先改 Redis，再发 Kafka。它的 RPC 成功**不能**当作持久业务事实。`LikeUpdateService.Consume` 现在同步调用 `ProcessLikeMessageBatch`；PG 的 `like_consume_inbox`、`like_record` 与独立 `like_domain_fact_outbox` 同事务提交后才返回成功，PG 失败向 Kafka 消费层返回错误。消费者把操作码 1/2/3/4 归一为持久状态 1/0/2/0，只记录真实 0/1/2 状态变化。`msg_id` 唯一且附消息语义哈希，重复同消息跳过，冲突载荷拒收；`last_operation_id` 防止旧 Snowflake 操作覆盖新状态。原 `like_outbox_event` 仍只负责首赞热度事件，不充当 H09.a 事实或 DC 已交付证据。

两类事实均使用 `rtw.community-fact.v1`、完整 `rtw.identity/platform/<UID>`、`event_id`、`event_type`、`producer`、`aggregate_id`、`operation_id`、`target_type/id`、`operation`、`source_ref`、`event_time/occurred_at/available_at`。无权威内容修订时，`target_revision` 与 `aggregate_version` 为 `null`，`revision_status=unknown`，不得从旧 ID 猜修订。评论载荷有 `comment_id/parent_comment_id`、可见状态和 `search_evidence=false`；不包含评论正文，首期不得进入学习问答 EvidencePack。点赞载荷有前后状态，撤销与切换不能只按正向点赞计数重建。

## 部署、恢复和界限

先在评论、点赞各自数据库依序执行 `internal/model/migrations/001_*.sql`、`002_*_dc_wire.sql` 与 `003_*_fact_delivery.sql`，再启动对应 RPC/消费者、`cmd/fact-dispatch` 和 `cmd/fact-authority`；迁移重复执行幂等。`002` 的 `NOT VALID` 约束保留既有旧行供后续辨析，但禁止旧版消费者在迁移后继续追加无出站 envelope 的事实；`003` 只允许派发状态与技术回执变化，冻结事件身份、业务载荷和 envelope。切换时须先停旧消费者。新评论或点赞事实若无法写 outbox，业务 PG 事务回滚；不可手工删 outbox 来“修复”卡住的消息。消费者失败应通过 Kafka 重试或隔离死信进行恢复，不能凭日志冒充事实；当前未证明 Kafka broker 的实际重投配置。

历史 `like_record.state=2` 可能是旧消费者写入的“取消赞”操作码，也可能被旧查询解释为点踩；旧 3/4 更不符合新状态表。`last_operation_id=0` 的歧义旧行与非法旧状态会拒绝新事实事务，需按原始消息/Redis 证据单独辨析并做受控迁移。本变更不伪造历史明细或回填 H09.a 事实。Redis 写成功但 Kafka 推送失败仍可能造成暂时或永久不一致；本次只修复**已到达消费者**的 PG 确认边界。DC 真实投递与 BTW 权威接纳的本地进程链已验收；真实 Kafka/Redis、数仓 ODS/DWD、线上运行和全量对账尚未完成，因此 WS02-B/H09.a 整体仍是 `PARTIAL`。

## DataCenter 出站 wire 合同与交接

本切片在独立分支 `feat/community-dc-event-wire-20260914`，从 RTW 集成头 `c8246fe` 开始，只修改评论与点赞源事实。Outbox 原 `payload` 保持 `rtw.community-fact.v1` 业务事实，`target_revision` 与业务 `aggregate_version` 仍为 `null`，绝不把内容修订猜作数字。新增 `delivery_envelope` 冻结可直接 POST `/v1/events` 的外层：`schema_version: 1`、`aggregate_version: 正整数`、原事件 ID/类型/生产者/操作 ID、RFC3339Nano 时间和嵌套原业务 `payload`。`aggregate_id` 与 `fact_version` 列用于唯一约束、检查与派发读取；发送端必须原样复用这份已提交的 `delivery_envelope`，并以 `event_id` 作为 `Idempotency-Key`，不得重算可变时间或事实字段。

评论外层 `aggregate_id` 是评论 ID，`comment_fact_stream` 在评论创建/删除/互动同一 PG 事务中逐条分配连续版本；点赞外层 `aggregate_id=like-state/<UID>/<target_type>/<target_id>`，`like_fact_stream` 在该用户目标状态真正变化的事务中分配连续版本。点赞业务载荷中的原 `aggregate_id=<target_type>/<target_id>` 仍指互动目标。两种版本都只是各自生产者事实流版本，不是内容修订、客户端雪花操作 ID，也不是 DataCenter 接收 offset。旧 Outbox 若对同一聚合仍有 `delivery_envelope IS NULL` 行，新事实事务拒绝提交，需先依据原始事实次序和消息证据做受控迁移；不可将所有旧行填 `1`、随意删旧行或绕开拒发门禁。新派发器只能 claim 非空 envelope，旧行不得发往 DC。

当前评论与点赞各有独立 `fact-dispatch`，按聚合版本顺序原样发送冻结 envelope，并把匹配的 DataCenter `receipt_id/input_hash/offset/received_at` 落回源 Outbox；失败响应保留同一事件待重试，非法冻结体进入 blocked。独立 `fact-authority` 只读取已落匹配回执且仍能由领域状态证明的事实。对外主体 wire v2 只有 `{issuer:"rtw.identity",subject_id:<UID>}`，没有 realm 或 tenant 字段；业务载荷中的 `rtw.identity/platform/<UID>` 是既有 v1 规范来源字符串。EventID 使用可被 DC 回执路径安全读取的点分键，`source_ref` 仍保留领域路径语义。接收 201/200 仅表示技术接纳，不等于业务事实已经进入 DWD 或可训练。

## 验收证据

`service/comment/rpc/internal/model/test-community-postgres.sh` 在隔离 PostgreSQL **16.14** 上运行三个包的 `go test -mod=readonly -race -count=1`，退出 0；同一次 PG 会话中两份 SQL 迁移各执行两遍且退出 0。测试覆盖评论创建/回复/待审核、重复及冲突 ID、跨目标与无权删除、重复删除不重扣父回复、并发点赞只产生一次事实、状态正反操作、outbox 失败回滚；点赞覆盖同消息/冲突消息、旧消息、同批有序反转、并发重复投递、失败回滚以及实际消费者的失败返回和重试。

`go test -mod=readonly -race -count=1 ./service/comment/rpc/... ./service/like/rpc/...`、对应 `go vet` 和 `go mod verify` 均退出 0。此验收是 RTW 源事务与消费端的隔离 PG 子链，未声称真实 Kafka/Redis、DC 交付、BTW 接纳或线上运行。

设置 `SEA_DC_PLATFORM_ROOT` 为已核对的 DataCenter 独立工作树后，同一脚本额外构建并启动真实 `cmd/platform -migrate`，对隔离 PG 中真实 RTW 评论四版与点赞六版共十条冻结 envelope 执行 HTTP 接纳。2026-09-14 的本地验收退出 0：两个 producer 各自从 offset 1 开始；评论 source watermark 连续到 4；相同 ID 同体重放回原 receipt/hash/offset；同 ID 改体、另一 ID 占用同聚合版本均为 409，字符串 schema 与空聚合版本均为 400。此处是实际 DC HTTP 契约子验收，不是 RTW 派发器/回执落库验收。

2026-09-15 使用 BTW `cmd/worker/community_fact_acceptance.sh` 的同次进程验收退出 0。固定提交为 DataCenter `f59a676a3439f66122e0ec579cd22f030719e058`、RTW 运行代码 `58468c1d4d0fd5708bc0c9726a94ef252cb588eb`、BTW 运行代码 `6050ab8ca84ac8225442571a985c071113336041`；最终报告 SHA-256 为 `269cf7d12c1b01f3181bfb38636c4623f838bedfa8177e74e913876a6002eeb8`。真实 RTW 业务事务产生评论 create、like、unlike、delete 四条和目标 like、unlike 两条；两个派发器落回各自从 1 开始的 DC 全局 producer offset，两个 authority 进程再供 BTW 逐条核验。RTW 源 PG 六行均保持 `target_revision=null/revision_status=unknown`，评论同时保持 `search_evidence=false`。

四个新进程的每行日志已解析并要求 `timestamp/level/service/environment/service_version/instance_id/component/log_source/event/message`；终态要求 `outcome/duration_ms`，authority 从 BTW 注入的 W3C Context 获得 trace/span。go-zero signal 日志经注入 Writer 进入同一 envelope，业务层不依赖 package-level `slog`。本项观测状态是 `LOCAL_VERIFIED`：验收证明结构化日志和 Context 关联，没有证明 RTW OTLP 导出或线上 Collector 接收。
