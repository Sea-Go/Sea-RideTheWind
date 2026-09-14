# H02 RTW 产品搜索入口交接

状态：**LOCAL_VERIFIED（2026-09-14）**。隔离 PG、真实 User Center RPC 与 BTW 协议 fixture 已通过；这不是三路真实索引与模型的端到端验收。

## 产品接口与所有者

网页和桌宠只调用 JWT 保护的 `POST /v1/knowledge/answer-sessions/:session_id/searches`。JSON 正文只接受 `module_id,query,depth,intelligence,idempotency_key`。`depth` 为 `fast|detailed`，`intelligence` 为 `low|medium|high`；`idempotency_key` 为 8–128 字节 ASCII 字母、数字、`_`、`.`、`-`。RTW 经 User Center JWT 与实时 User RPC 得到 `rtw.identity/platform/<UID>`，从人工当前发布取完整三路 `SearchSnapshot`。客户端传入主体、快照、搜索 ID 或答案 ID 会在路由解析阶段被拒绝。

RTW 用固定 Go JSON 字段顺序 `module_id,query,depth,intelligence` 的 SHA256 小写十六进制值做 `request_hash`。它与完整 SubjectRef、逻辑会话、幂等键在 `knowledge_product_search_operations` 中一次绑定固定快照、`search_id` 和 `answer_id`。同键同正文复用原行；同键异正文 HTTP 409。RTW 发往 BTW 的私有 `/v1/search/summary` 正文只有前四字段，另带单值 `X-Sea-Search-Scope`。签名 payload 及 HMAC 线格式按 Sea-Docs H02 合同；签发 Unix 秒，TTL 120 秒，服务端配置密钥至少 32 字节，HTTP 禁止重定向转发。

产品 POST 返回 HTTP 200 仅限 RTW 自己的 `AcceptedAnswer` 已提交，且完整主体/会话/search/answer/请求/快照、已接纳引用收据与 BTW 返回的终态结构逐字段一致。`insufficient` 可以没有引用；伪 BTW 200 或只有框架 Session 事件不能发布。HTTP 202 表示别的请求持有操作 lease；503 返回稳定 `search_id/answer_id` 与 `retryable_failure`，以同键 POST 重试。`GET /v1/knowledge/answer-sessions/:session_id/searches/:search_id` 经同一 User RPC 校验，只查询本人的操作与已接纳终态；它显示运行中、可重试失败或最终答案。客户端无需自行构造搜索 ID。

## 迁移、预算与恢复

正式服务 `Postgres.Migrate=false`，上线前在目标知识库执行仓内 [migrate-product-search.sql](../../scripts/migrate-product-search.sql)。该 DDL 可重复执行；升级前按部署流程备份并确认现有表无同名异构定义。启用 `SearchSummary.Endpoint` 后，服务启动会探测表及必需列；缺迁移时拒绝启动。回滚先停新入口并清空正在处理的请求，可保留操作表以保留重试与审计；只有确认表中无记录、无客户端依赖重试且已备份时才可 `DROP TABLE knowledge_product_search_operations`。

`SearchSummary.Endpoint` 必须是明确的 `/v1/search/summary` URL，`ScopeKey` 不入日志。`FastTimeoutMillis` 默认 30000，`DetailedTimeoutMillis` 默认 90000，范围分别 1–60 秒与快搜预算至 180 秒；RTW `Timeout` 必须至少大于详搜预算 5 秒，示例配置为 120 秒。单次上游调用继承客户端 Context 并按深度设置超时，随后给权威答案回查最多 3 秒。

取消或崩溃不删除固定操作：2 分钟 lease 到期可用原键重新 claim；新尝试首先按 AnswerID 与完整 scope 回查 RTW 已接纳答案。BTW 已提交但 HTTP 回执丢失时直接恢复已验证答案，不重新选择当前发布指针。未提交的 200/非 200 都保留可重试失败状态，不能进入历史。稳定 search/answer ID 使 BTW 与 RTW 的提交幂等；未决业务不能用 200 中的模型文本代替权威答案。

## 本切片验收与下一交接

本切片的隔离验收覆盖：真实 RTW go-zero HTTP、隔离 PostgreSQL 16、真实 User Center RPC，BTW `httptest` 对签名头与四字段 JSON 的协议检查；伪 200 无 Commit、Commit 后 502 丢回执、同键冲突/重投、他人跨 scope GET、未发布与无效键反例。需运行 `KNOWLEDGE_REAL_USER_GATE=1 KNOWLEDGE_PG_BIN=/opt/homebrew/opt/postgresql@16/bin bash service/knowledge/scripts/acceptance.sh`，以及 `go vet ./service/knowledge/...`。BTW fixture 的模型和三路索引是结构性的，结果只能证明本产品入口和双仓线协议局部合同，不能标 H02/J02/H07 整体 ACCEPTED。

本分支验收实测：上述完整命令以 `-race` 运行通过；`TestRealHTTPKnowledgeWorkflowWithUserCenter`、普通 go-zero/PG HTTP 流程、`TestProductSearchOperationFixedSnapshotLeaseAndReplay`、`TestProductSearchMigrationProbeAndBadKeys` 与 `TestProductSearchProjectsOnlyMatchingCommittedCitationAnswer` 均 PASS，脚本内 `go vet ./service/knowledge/...` 和 User Center race/vet 也通过。成功、202 与 503 均按真实 HTTP 状态断言；测试在 BTW fixture 返回伪 200 而未 Commit 时得到 503、历史为空，在 BTW 通过真实 RTW 私有 HTTP Commit 后故意返回 502 时仍按 AnswerID 恢复 200。直接 PG 测试另验证 `succeeded` 必须有同一快照原文、引用收据与已接纳答案，异查询/异发布不能复用答案。操作表迁移脚本在旧结构（缺表）隔离 schema 上实跑并通过启动前探测。正式 BTW 进程、真实三路索引、模型总结、网页/桌宠和 Collector 下钻尚未在本分支验收。

下一交接由 BTW 私有 HTTP 入口在真 Runner/同代三路索引中消费 RTW 签发的 scope，并同一隔离 RTW PG 完成原文、引用、答案 Commit；RTW 再用本入口发布。网页与 WhaleHall 桌宠只需传产品请求字段，处理 202/503 同键重试、展示 200 已验证答案与固定引用，并验证两端 EOF、会话历史一致。SSE、Tool 形式及在线观测下钻属于后续切片。

## 真实 BTW 服务的空证据子链

集成树追加 `TestRTWRealProductSearchServer`：RTW 在隔离 PostgreSQL 上使用真实 go-zero HTTP 签发产品范围，产品 POST 的四字段正文与原始签名头经透明中继进入独立 BTW 测试进程。BTW 在真实 HTTP 入口验签，执行 tRPC 根 Graph/Runner，空证据路径向同一 RTW Worker HTTP 提交 `insufficient`；RTW 从自己的 PostgreSQL 验证固定 AnswerID、SearchID、`rtw.identity/platform/<UID>` 与会话，只存在一条已接纳答案且没有引用。相同幂等键重投不再运行 BTW，产品 GET 与 POST 的终态一致。BTW 子进程验证原文读取、引用接纳和模型调用均为零，并检查原生根 Agent Span。对应 BTW 消费者验收见 `internal/transport/http/search/RTW_PRODUCT_SERVER_ACCEPTANCE.md`。

在 RTW 集成树设置 `SEA_BTW_PRODUCT_SEARCH_ROOT=/Users/edy/Sea/.codex-worktrees/sea-btw-runtime-content-20260914`、`SEA_BTW_CITATION_CONSUMER_ROOT`、`SEA_BTW_INDEX_CONSUMER_ROOT` 与 `KNOWLEDGE_REAL_USER_GATE=1` 后运行 `bash service/knowledge/scripts/acceptance.sh`，退出码 0；脚本包含真实 User Center/隔离用户库测试及各子链的 race 与 vet。这证明 H02/J02 的**真实签发与空证据回答子链 `INTEGRATED`**，并不证明有证据的成功回答、同代三路检索、模型总结、正式 BTW 服务部署、公开 SSE/Tools 或两端客户端展示；整体仍为 `PARTIAL`。
