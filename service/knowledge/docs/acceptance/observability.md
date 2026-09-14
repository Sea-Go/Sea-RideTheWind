# 知识服务统一观测局部验收（2026-09-14）

固定代码提交：`5ed416bf3ba3526edb243ba819cd0e41af6f43ab`，开发分支 `feat/knowledge-service-20260914`。验收只使用隔离 PostgreSQL 16、实际 go-zero HTTP 子进程、本地 OTLP gRPC 测试接收端与合成知识数据；未部署生产、未接真实 DataCenter Collector 或查询后端。`observability_status=LOCAL_VERIFIED`，整任务不得标为 `ACCEPTED`。

## 实现与计数边界

- `api/knowledge.go` 唯一装配 go-zero `logx` JSON Writer、OTel Trace Provider 与 Prometheus Registry。框架普通、Context、错误日志进同一 stdout Writer；启动配置解析失败也输出结构化失败事件。服务使用带实例标识的 `/metrics` 自探测，实际监听可响应后才记录 `knowledge.service.started`。停止时先等 Outbox worker，再关 Trace Provider、预刷写日志、记录终态并有界关闭 Writer。
- HTTP 外层记录模板路由、实际有界 method、status、耗时和 request/trace/span；领域阶段记录 `operation_id`、知识对象/版本和提交后终态。HTTP 仅记录响应分类，错误原因由领域边界写一次。go-zero 授权日志可能附完整请求转储，Writer 保留错误原因及关联并标记 `request_dump_omitted`，不把请求体写进应用日志。
- Outbox 在数据库 `correlation` 列保存原始 `traceparent`、`tracestate` 和 request_id，不改变 H04 业务事件信封；异步投递建立新 Trace 根 Span 与原 Span Link，并传递 W3C trace header。空轮询不产生日志；轮询错误有固定事件，投递回执确认后才计成功。领域命令重放记 `replayed`，不重复计提交。
- `/metrics` 的 operation、outcome、error_code、route、method、status_class 标签有界；请求 ID、任务 ID、Trace ID 不作标签。日志写入、排队、丢弃和 OTLP 导出故障各有计数，阻塞日志 sink 不阻塞业务请求。

## 固定提交复验

执行 `KNOWLEDGE_PG_BIN=/opt/homebrew/opt/postgresql@16/bin KNOWLEDGE_KEEP_EVIDENCE=1 service/knowledge/scripts/acceptance.sh`，退出码 0。脚本包含 `go test -race ./service/knowledge/... -count=1 -v`、`go vet ./service/knowledge/...`、`git diff --check`。真实子进程完成 JWT 拒收、模块/原始资料/Wiki/候选/构建/claim/READY/发布/回滚、幂等回放与 409 冲突；数据库测试覆盖过期租约、旧 attempt/CAS、工件损坏、事务回滚和 Outbox 丢回执重投。

实际子进程日志共 **296 行**，逐行 JSON 解析通过，全部 `service_version=5ed416bf3ba3526edb243ba819cd0e41af6f43ab`；存在 `service.starting`、实际自探测后的 `service.started`、`service.stopped`，领域成功/回放/拒绝及 HTTP 请求终态。关联事件含真实 request/trace/span；`POST` 未命中路由的 404 仍保留 `method=POST`。10 条 go-zero 授权错误保留原因但省略请求转储。

实际 `/metrics` 显示 `knowledge.release.activate` 的提交数 **2**，与数据库两次发布/回滚记录一致；同一操作另有 **1 次 replayed**、**1 次 IDEMPOTENCY_CONFLICT 拒绝**，均未增加提交数；`sea_knowledge_log_records_dropped_total=0`。本地 OTLP gRPC 测试接收端实际收到来源 Span 和独立的新根投递 Span，后者带指向来源 Span 的 Link；日志 sink 阻塞测试证明队列有界、关闭受期限约束，写入失败测试证明预刷写会返回错误。

证据位于任务交付目录（不提交合成运行日志到源码仓）：

| 文件 | SHA-256 |
| --- | --- |
| `/Users/edy/Sea/Deliverables/2026-09-14/RTW观测证据/knowledge-http.jsonl` | `760a8e5c012159992d783a0b0a14223c3dd9b343335b8f00615c147cf9c5375b` |
| `/Users/edy/Sea/Deliverables/2026-09-14/RTW观测证据/knowledge-metrics.prom` | `ce4592cbf73b964b69769092680ef739036ae368c564fde01c4d93edb3516900` |
| `/Users/edy/Sea/Deliverables/2026-09-14/RTW观测验收原始.log` | `49ba118aa7d333443f35dea283a9d23ed48335805c62d2535a1d645c81620913` |

## OBS 门禁边界

OBS-01/02/03 的进程入口、真实 JSON、go-zero 框架适配已在本地证实。OBS-04 的数据库关联与本地 OTLP Span Link 已证实，**RTW→DC 跨服务采集/下钻未证实**。OBS-05 仅实际 HTTP 冲突及无观测注入的其他领域反例分别通过，尚未逐项核对租约/工件失败的日志和 Span。OBS-06 的真实 `/metrics` 计数、日志丢弃和队列故障已证实，生产用量/水位未验。OBS-07 未接真实 Collector、日志后端及 DC 查询，因此不能升为 `INTEGRATED` 或 `ACCEPTED`。OBS-08 本地 race、终态预刷写、sink 故障和正常退出已通过；实际部署退出流程仍需在目标环境复验。
