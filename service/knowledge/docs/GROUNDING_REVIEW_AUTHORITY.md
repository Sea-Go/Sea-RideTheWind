# RTW 答案依据审阅权威

RTW 管理员可登记 Ed25519 审阅公钥、撤销公钥，并提交对一个已接纳答案的签名 claim 审阅。`GroundingReviews.Enabled` 默认 `false`；启用前先执行 `service/knowledge/scripts/migrate-grounding-reviews.sql`，启动时检查三张权威表。私钥只由审阅人持有，RTW 只保存公钥、签名审阅与不可变回执；本仓测试密钥每次在进程中临时生成，测试记录明确为 `synthetic_fixture`。

管理员接口：`POST /v1/knowledge/reviewer-keys` 登记 `key_id,data_kind,public_key_ed25519_hex,idempotency_key`，JWT 管理员 `userId` 成为 `reviewer_id`；`POST /v1/knowledge/reviewer-keys/:key_id/revoke` 提交 `reason,idempotency_key`；`POST /v1/knowledge/answer-grounding/reviews` 提交 `case_json,review_json,key_id,idempotency_key`。登记、撤销和审阅各生成同事务 Outbox 事件，使用 `ridethewind.knowledge` producer。命令重放返回原回执；同 case 的不同审阅不覆盖旧字节，必须另建有明确身份的新 case。撤销不删除旧审阅，但当前公钥状态为 `revoked`，离线再次评估不得继续把旧签名当作当前有效人审权威。

Worker 私有读取：`GET /internal/v1/knowledge/reviewer-keys/:key_id` 返回 `sea.rtw.reviewer-key.v1`，含 `key_id,reviewer_authority=ridethewind.knowledge.admin,reviewer_id,data_kind,public_key_ed25519_hex,registered_at,revoked_at,status,registry_revision,registration_event_id,revocation_event_id`；`GET /internal/v1/knowledge/answer-grounding/reviews/:case_sha256` 返回 `sea.rtw.answer-grounding-review-receipt.v1`，含原始 `case_json/review_json` 与 SHA-256、key/审阅人/提交时 registry revision、answer/search ID、RTW 已接纳答案/turn/pack 哈希与 pack ref、`trace_authority_status=external_case_unverified`、审阅时间及 Outbox event ID/hash。读取旧回执与读取当前 key 状态必须同时做；回执不是可绕过撤销状态的 trust 文件。

注意 `citation_pack_ref=search-citations/sha256/<SHA256(search_id)>` 是 RTW 的稳定逻辑引用，`citation_pack_sha256` 才是已接纳 EvidencePack 原始字节哈希；两者不相等，也不能用 ref 后缀冒充 pack hash。

提交核验沿用 BTW `sea.search.answer-grounding-case.v1` 的 `json.MarshalIndent+\n` 固定字节及 `sea.search.answer-grounding-review.v1` 的 `encoding/json` payload 签名规则。RTW 核 case SHA/CaseID，核 Ed25519、reviewer 与注册 key 的身份/状态/时间，核每段 UTF-8 字节跨度、文本、label、理由、全部引用 ID 及 `coverage_complete`。RTW 再从 PostgreSQL 的 `knowledge_accepted_answers`、`knowledge_answer_citations`、`knowledge_search_citations` 独立比对答案原文、全部引用顺序、quote/hash 和固定 pack ref/hash；错答案、错 quote、错 pack、错 search/answer、伪签名、错 reviewer、旧 key 和重复 case 都在写入前拒绝。

RTW 的 PostgreSQL **没有** BTW case 所引 RTW 结构化日志、DC 模型响应或 usage 报告的原始工件。因此 RTW 只验证这些外部字段的格式并在回执明示 `external_case_unverified`；BTW 必须先用自己冻结的 usage、RTW report、原始结构化日志重新生成完全相同的 case，再取 RTW Worker 回执与当前 key 核验。签名只证明注册公钥对固定 review payload 签过名，不证明标签判断正确，也不证明外部 trace/DC 工件已经由 RTW 自证。当前没有真人审阅输入、正式支持率或线上启用。

## 2026-09-15 本地验收

- `KNOWLEDGE_PG_BIN=/opt/homebrew/opt/postgresql@16/bin KNOWLEDGE_KEEP_EVIDENCE=1 service/knowledge/scripts/acceptance.sh` 退出 0，证据目录为 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T//sea-knowledge-acceptance.tuUviy`。它运行全部 Knowledge 包的 race 测试、真实 go-zero HTTP 子进程和隔离 PostgreSQL 16，并在随后执行 `go vet ./service/knowledge/...`。`TestReviewerRegistrySignedGroundingAndRevocation`、两个 registry 身份碰撞用例、两个 active key 同 case 的单赢家用例、`TestGroundingReviewRejectsForgedSignatureAndSource`、迁移连续执行两次及 Outbox 回滚均通过。
- HTTP 用例逐接口验证：未认证管理员写为 401、非管理员为 403；错误签名/来源为 400；无 Worker token 或普通用户 token 的私有读为 401；不存在的 key/review 为 404。启用时注册、提交、同键重放、撤销与撤销后历史回执读取均为真实 HTTP→PG 行为。默认配置和无开关的应用逻辑仍返回 unavailable，不接纳任何写入。
- 使用 goctl 1.9.2 连续生成后，DSL、Go routes/types、Swagger 与 TypeScript 汇总 SHA-256 均保持 `6792eccadd4bbf3123ca7abb3f6913f09fa1a83c2735c16daaa4b2f9666ffa90`。`go vet -mod=readonly ./service/knowledge/...`、`go test -mod=readonly ./service/knowledge/... -count=1` 与 `go mod verify` 均退出 0。
- 跨仓 v4 在同一 RTW 隔离 PG/HTTP 生命周期中完成 active key 读取、签名/答案/引用核验、撤销及当前 key 再读取。BTW 产出的 `/private/tmp/sea-grounding-review-live-proof-v4.json` SHA-256 为 `333fef27d745105aed0b910da773ea585ce086477833a5321c3e3a7e38694d45`，记录 `pack_identity_distinct=true`、`trace_authority_status=external_case_unverified`、registry revision `1→2`、active 决策 `synthetic_rejected_unsupported`、撤销后决策 `pending_revoked_authority`、`activation=none`、`runtime_blocking=false`。
- 跨仓 v5 再从同轮原始 usage、RTW answer report 和结构化日志重新冻结 case，然后通过真实 Worker HTTP 执行两次 BTW `evaluate-rtw` CLI。active 输出 `/private/tmp/sea-grounding-evaluate-rtw-active-v5.json` SHA-256 为 `7dcd0c4f3b43a78198cc57a337523ceb9cc504a5264e9cff5fabc16ae71c92cc`；撤销后输出 `/private/tmp/sea-grounding-evaluate-rtw-revoked-v5.json` SHA-256 为 `12abba2505445c024fe5c5236cf98ff37423b189c91ba4b5773b6322463b7b9c`。两次 CLI 都退出 0，只输出结构化 slog JSON。

以上标签全部来自进程内临时 Ed25519 密钥和 `synthetic_fixture`，只证明合同与状态迁移可执行。没有真人标签、可信 RTW 服务身份固定、线上启用、生产部署或运行时阻断验收；本轮 acceptance 未启用 `KNOWLEDGE_REAL_USER_GATE`，因此也不把产品 User Center 独立进程纳入本切片结论。
