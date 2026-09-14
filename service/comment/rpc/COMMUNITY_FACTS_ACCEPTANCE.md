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

先在评论、点赞各自数据库执行 `internal/model/migrations/001_*.sql`，再启动对应 RPC/消费者；迁移重复执行幂等。新评论或点赞事实若无法写 outbox，业务 PG 事务回滚；不可手工删 outbox 来“修复”卡住的消息。消费者失败应通过 Kafka 重试或隔离死信进行恢复，不能凭日志冒充事实；当前未证明 Kafka broker 的实际重投配置。

历史 `like_record.state=2` 可能是旧消费者写入的“取消赞”操作码，也可能被旧查询解释为点踩；旧 3/4 更不符合新状态表。`last_operation_id=0` 的歧义旧行与非法旧状态会拒绝新事实事务，需按原始消息/Redis 证据单独辨析并做受控迁移。本变更不伪造历史明细或回填 H09.a 事实。Redis 写成功但 Kafka 推送失败仍可能造成暂时或永久不一致；本次只修复**已到达消费者**的 PG 确认边界。DC 正式 EventSpec/投递确认、BTW `TrustedFactBinder` 接纳、数仓 ODS/DWD、端到端对账尚未完成，因此 WS02-B/H09.a 整体仍是 `PARTIAL`。

## 验收证据

`service/comment/rpc/internal/model/test-community-postgres.sh` 在隔离 PostgreSQL **16.14** 上运行三个包的 `go test -mod=readonly -race -count=1`，退出 0；同一次 PG 会话中两份 SQL 迁移各执行两遍且退出 0。测试覆盖评论创建/回复/待审核、重复及冲突 ID、跨目标与无权删除、重复删除不重扣父回复、并发点赞只产生一次事实、状态正反操作、outbox 失败回滚；点赞覆盖同消息/冲突消息、旧消息、同批有序反转、并发重复投递、失败回滚以及实际消费者的失败返回和重试。

`go test -mod=readonly -race -count=1 ./service/comment/rpc/... ./service/like/rpc/...`、对应 `go vet` 和 `go mod verify` 均退出 0。此验收是 RTW 源事务与消费端的隔离 PG 子链，未声称真实 Kafka/Redis、DC 交付、BTW 接纳或线上运行。
