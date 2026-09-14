# 已接受学习问答历史：RTW 权威存储验收

状态：**RTW 权威 Commit / Get / List 局部实现；WS02-D / H02 / H07 整体仍为 PARTIAL**。本记录只描述隔离开发分支和本地 PostgreSQL 16 的结果，不代表公开学习流、正式主体映射或生产部署。

## 归属与提交点

- 权威入口是 `api/knowledge.api` 的内部 worker `POST /internal/v1/knowledge/accepted-answers`。请求包含稳定 `answer_id`、`search_id`、完整 `subject={authority_id,tenant_id,subject_id}`、逻辑 `session_id` 和 BTW `AcceptedRootTurn` 的完整 `turn_json` 字符串。RTW 不导入 BTW 的 internal 包；按当前跨仓 JSON 合同解析并检查外层字段、固定快照、终态和引用集合。
- `knowledge_answer_sessions` 只为同主体、同逻辑会话串行分配 `accepted_ordinal`；`knowledge_accepted_answers` 保存产品 turn 的不可变原文与 hash；`knowledge_answer_citations` 保存答案实际引用的 evidence ID 和不可变定位元数据。BTW 的私有 tRPC-Agent-Go Session 事件不是这张产品表的可续跑 Agent 历史。
- `succeeded` 必须有非空答案、已被本轮 EvidencePack 包含的无重复引用，以及 `search_id / pack_hash / durable_ref` 三项完全一致的已提交 `knowledge_search_citations` 收据。RTW 在**同一个 PostgreSQL 事务**里锁定引用行、比较精确 pack JSON、写答案和引用、推进接受顺序；任何失败回滚整个事务。
- `insufficient` 是合法的空证据产品终态，必须没有回答文本、引用 ID 或引用收据；它写入答案顺序，但不伪造 `knowledge_search_citations` 或 `knowledge_answer_citations`。
- 相同 `answer_id`、相同 scope、相同完整 turn 重投返回既有接受时间和顺序，不重复写入；相同 `answer_id` 的其他 turn 或主体冲突。`GET /internal/v1/knowledge/accepted-answers/:answer_id` 按完整 scope 回查未知提交回执；`GET /internal/v1/knowledge/accepted-answers` 按 `after_ordinal` 和 1–100 的 `limit` 正序分页。

## 工作区域与交接

`[W0:ROOT]` 隔离 RTW 工作树；`[W1:WRITE]` `api/knowledge.api`、`service/knowledge/api`、`service/knowledge/generated`、本文；`[R1:READ_ONLY]` BTW `internal/search/root_session.go`、Sea-Docs 知识平台与任务包；`[D1:DEPENDENCY]` 锁定 go-zero/pgx 模块与 goctl 1.9.2；`[G1:GENERATED]` `service/knowledge/api/internal/types`、路由与 TypeScript/OpenAPI 只能由 `service/knowledge/scripts/generate.sh` 生成；`[X1:EXTERNAL]` 本任务自启停的隔离 PostgreSQL 16，其余外部系统未写；`[N1:OUT_OF_SCOPE]` usercenter、前端、BTW 正式入口、原始脏工作树与生产；`[T1:TEMP]` 生成器可执行文件及本地验收临时目录。

主职责为 `[C4:PERSISTENCE]`；跨区 `[C7:CONTRACT]` 的 `AcceptedRootTurn` 与 worker JSON，`[C1:TRANSPORT]` 的 worker token HTTP 路由，`[C8:VERIFY]` 的隔离 PostgreSQL / 真 HTTP / race / 观测。RTW 只接受 BTW 已完成私有 Runner/Graph 和结果验证后交出的产品 turn。BTW 接线方应使用当前 API adapter，并在 Commit 回执不明时先按 AnswerID + scope 回查，再决定重试。

## 本地验收与剩余缺口

代码提交：`e44bed6`。验收命令：`KNOWLEDGE_PG_BIN=/opt/homebrew/opt/postgresql@16/bin KNOWLEDGE_KEEP_EVIDENCE=1 service/knowledge/scripts/acceptance.sh`；该脚本只创建并停止临时 PG16，运行 `go test -race ./service/knowledge/... -count=1 -v`、`go vet ./service/knowledge/...`、`git diff --check`。最终复跑退出码 0。模型测试覆盖缺失引用、三项收据不一致、已引用答案/空证据终态、同键幂等、跨主体读取、有界有序分页、并发同键重投和独立 turn 的接受顺序；`TestAcceptedAnswerDurableHistoryAndCitationGate`、`TestAcceptedAnswerConcurrentReplayOneOrdinal`、`TestAcceptedAnswerConcurrentOrderAndCrossScopeCollision` 均 PASS。真实 HTTP 子进程 `TestRealHTTPKnowledgeWorkflow` PASS，覆盖 worker token、写入、回查、列表和冲突。

最终本地证据目录：`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-acceptance.ExHc6S/`（临时目录，由验收脚本保留）。`test.log`、`observability/knowledge-http.jsonl`、`observability/knowledge-metrics.prom` 的 SHA256 分别是 `afa06782aa96b220cc071bb106b1f07b34421472cdd145f5986b3e2fd540ff79`、`29aa6b81750d7c508ec517675dbe465c500d9bf8a081ebf3fed2c9bbc35bd57c`、`1a1c5502fcc40624f101806015266f9576195b9cdf38df42f0e720aded65615f`。运行日志中 `knowledge.answer.accept` 的成功、重放、拒绝以及 `get/list` 成功记录均含 trace/span/request/operation ID；指标只对唯一答案提交计 `sea_knowledge_commits_total{operation="knowledge.answer.accept"} 1`，读取与重投没有制造新提交。

仍需接线：BTW 的 `AcceptedRootHistory` HTTP adapter、答复流提交前门禁、按已接受产品 turn 组织真正可续跑的 Agent 历史、RTW 用户 JWT 到完整 SubjectRef 的权威解析、前端历史页面与引用状态读取。这里没有把后续工作写成已验收。
