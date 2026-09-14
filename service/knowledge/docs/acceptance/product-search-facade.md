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

下一交接由 BTW 私有 HTTP 入口连接同代三路真实索引及模型，在现有真 Runner 上消费 RTW 签发的 scope。网页与 WhaleHall 桌宠只需传产品请求字段，处理 202/503 同键重试、展示 200 已验证答案与固定引用，并验证两端 EOF、会话历史一致。SSE、Tool 形式及在线观测下钻属于后续切片。

## 真实 BTW 进程的有证据子链验收

在隔离 PostgreSQL、真实 RTW go-zero HTTP 与独立 BTW HTTP 子进程中，RTW 用已人工发布的 release/index manifest 固定快照；父测试只把其中 chunk 的 `revision_id/chunk_id/quote_hash` 交给 BTW 子进程，不传答案或伪造引用。BTW 使用 RTW Worker API 重新读取当前快照、同版原文，在现有 `Delivery` 中再次核对定位和哈希、持久接纳引用，收到 RTW receipt 后通过原生 tRPC-Agent-Go Graph/LLMAgent/Runner 运行固定模型替身并提交产品轮次。RTW façade 从自己的 PG 验证 `knowledge_search_citations`、`knowledge_answer_citations`、`knowledge_accepted_answers` 后返回含真实 quote、evidence ID 与 receipt 的 200 `succeeded`；同键 POST、GET 完全一致，他人 GET 为 404。该子链也保留原来的 `insufficient` 空证据分支。

命令：`KNOWLEDGE_KEEP_EVIDENCE=1 KNOWLEDGE_REAL_USER_GATE=1 SEA_BTW_PRODUCT_SEARCH_ROOT=<BTW 独立 worktree 绝对路径> bash service/knowledge/scripts/acceptance.sh`，包含知识与 User Center 模块 race 全测、vet，实测退出码 0；`TestRealHTTPKnowledgeWorkflowWithUserCenter` 与 gRPC 替身版本均 PASS。此候选由**隔离测试中真实发布的 chunk 确定性注入**，并未测试三路真实检索的召回；模型是固定响应替身，未验证线上模型质量。可将“RTW 签发 → BTW 有证据成功 → RTW 接纳后公开”的子链记为 `INTEGRATED`，H02/H07 全量仍为 `PARTIAL`。
