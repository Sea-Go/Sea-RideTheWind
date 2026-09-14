# 已接受学习问答历史：RTW 权威存储验收

状态：**RTW 权威 Commit / Get / List 与已登录用户产品读面局部实现；WS02-D / H02 / H07 整体仍为 PARTIAL**。本记录只描述隔离开发分支和本地 PostgreSQL 16 的结果，不代表公开学习流、真实用户库或生产部署。

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

上述是该固定提交时的提供方证据。后续BTW已生成`AcceptedRootHistory` HTTP adapter，并在同一RTW真实HTTP/隔离PG16进程里提交已接纳引用的`succeeded` turn、幂等重放、按完整scope读回和跨主体拒读；RTW真实PG证明该AnswerID只留一条，答案提交JSON终态与BTW父Trace同ID。RTW用户服务也已在已验JWT与User RPC回执后生成固定单平台`rtw.identity/platform/<UID>`主体三元组。两者的局部证据见本仓`search-citations.md`与`service/user/user/api/identity_contract.md`，不代表实际用户事件或服务间Binder已接通。

仍需接线：正式搜索答复流提交前门禁、按已接受产品turn组织真正可续跑的Agent历史、RTW真实用户到知识搜索入口的主体传递、前端历史页面与引用状态读取、实际Collector→DC下钻。WS02-D/H02/H07整体仍未验收。

## WS02-D 已登录用户读面：本地增量验收

新增产品 GET `/v1/knowledge/answer-sessions/:session_id/accepted-answers`，仅接受 `after_ordinal` 与 `limit`（默认 50，1–100）；GET `/v1/knowledge/answer-sessions/:session_id/accepted-answers/:answer_id` 按完整会话回查。两条路由使用 User Center 的 JWT 密钥 `UserAuth.AccessSecret`，与管理员 `Auth.AccessSecret` 及内部 worker token 隔离。请求没有主体三元组字段；即使额外提交 `authority_id/tenant_id/subject_id`，产品 logic 也完全不用。服务端从 go-zero 已验 JWT 的数字 `userId` 提取 UID，再通过配置的 `UserRpc` 调用原 UserService.GetUser，要求用户存在且返回相同 UID，最后由共享 `service/user/user/identity` 生成 `rtw.identity/platform/<十进制 UID>`。原 User Center 的 `api/internal/identity` 保留兼容入口并转发到同一解析实现。这里 `platform` 只表示现有唯一用户空间，不代表组织或付费租户。

读取仍调用权威 Store 的完整主体+session 条件：列表按 accepted_ordinal 正序，跨主体同名 session 得空列表，跨主体 AnswerID 回查为 404；已删除用户或 RPC 返回 UID 错配为 403，无效 JWT 在触达 User RPC 前为 401，User RPC 不可用为 503。产品读接口返回已接受产品投影，原本内部 worker 的读写路径及提交验证条件不变。部署时 `USER_AUTH_SECRET` 必须匹配 User Center **实际**签发配置，`USER_RPC_ETCD_HOST` 必须指向 User RPC；两者并非从客户端获取。

工作区：`[W0]` 独立 RTW 工作树；`[W1]` `api/knowledge.api`、`service/knowledge/`、共享 `service/user/user/identity/` 和必要的旧内部转发文件；`[R1]` 用户中心 User RPC 实现、BTW/Docs；`[D1]` go-zero v1.10.2、gRPC、pgx；`[G1]` goctl 1.9.2 管理的 routes/types/OpenAPI/TypeScript；`[X1]` 临时 PG16 与本地测试 gRPC（允许测试写入，均自启停）；`[N1]` 原始脏树、其他服务与生产；`[T1]` 本任务 goctl 可执行文件和隔离 PG 目录。主职责 `[C1]` 产品 HTTP 与身份边界；跨 `[C2]` 读用例、`[C4]` 现有 PG 范围查询、`[C7]` User RPC/公开 API、`[C8]` 协议验收。

2026-09-14 复现：`KNOWLEDGE_PG_BIN=/opt/homebrew/opt/postgresql@16/bin bash service/knowledge/scripts/acceptance.sh` 退出 0，运行全部知识包 race 测试、真实 go-zero HTTP 子进程、临时 PostgreSQL 16、真实 gRPC 协议上的受控 GetUser 实现和 `go vet ./service/knowledge/...`。`TestRealHTTPKnowledgeWorkflow` 证明管理员/worker 令牌不进入产品读路由、伪造主体参数无效、其他 UID 拿不到该答案、已删除/UID 错配拒绝、答案列表 1→2 顺序分页、越界 limit/cursor 拒绝、RPC 停止时返回 503。`go test -mod=readonly -race -count=1 ./service/user/user/api/internal/identity ./service/user/user/api/internal/logic/user ./service/user/user/api/internal/handler/user ./service/user/user/identity`、`go vet ./service/user/user/...`、`go test -mod=readonly ./service/user/user/...` 均退出 0。真实用户数据库、跨服务登出令牌撤销、网页/桌宠消费和线上密钥/服务发现配置尚未验证，因此该切片是本地协议/PG 验收，不是 H02 完整通过。

## 产品引用当前可用性读面：后续局部验收

产品新增 `GET /v1/knowledge/answer-sessions/:session_id/accepted-answers/:answer_id/citations`。它复用同一 UserAuth JWT→User RPC同UID→完整SubjectRef，先确认本用户/会话的AnswerID，再从不可变`knowledge_answer_citations`按citation_order取**该答案实际引用的证据ID**，以已提交search_id回查RTW引用表的固定release状态并只返回对应元数据。响应含`available/unavailable`、固定module/release/发布指针版本和quote_hash/定位，**不返回旧quote正文**；`insufficient`答案返回空引用。这个状态是查询时刻的判断，历史`turn_json`仍作为不可变产品记录保存，客户端不可把它当作当前来源。

同一隔离PG16和真实go-zero HTTP测试通过：已登录UID可读当前可用引用，另一UID的同AnswerID为404，未认证401，已删除用户403、RPC停服503；撤回来源后同接口把已引用条目标为`unavailable`。模型测试另证明空证据不伪造引用、跨主体拒读，JSON投影不含quote文本；全知识服务race/vet和goctl 1.9.2二次生成hash一致。它仍没有真实用户数据库、网页/桌宠消费者、刷新通知或生产部署；H02/H07整体继续PARTIAL。
