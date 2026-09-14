# H02 Tools 产品父操作与证据交接

状态：RTW提供端`LOCAL_VERIFIED`；真实BTW HTTP消费的空证据与固定已发布chunk有证据两条子链`INTEGRATED`。H02 Tools整条产品链仍`PARTIAL`。

RTW 是浏览器和 WhaleHall Bun 的唯一产品入口。`POST /v1/knowledge/answer-sessions/{session_id}/tool-runs` 只接受 `module_id,idempotency_key`；User Center JWT 和 User RPC 共同确定完整 `SubjectRef`。新父操作从当前人工发布读取并固定三路 `SearchSnapshot`，返回 `operation_id,scope_ref,snapshot_ref,budget_ref,deadline_at_ms,allow_lower_intelligence,budget`。`tenant_id=platform` 是单平台用户命名空间。父键按完整 `SubjectRef + session_id + idempotency_key` 幂等，同键换模块为 409；旧键不会重选新发布。父生命周期 10 分钟，过期为 410。

子搜索由 `POST /v1/knowledge/answer-sessions/{session_id}/tool-runs/{operation_id}/searches` 创建；请求只有 `query,depth,intelligence,read_calls,quote_runes,idempotency_key`。`continue_search_id` 目前非空返回 400，不能声称已支持跨 HTTP 续搜。RTW 为每个 `(operation_id,idempotency_key)` 生成且持久保留 `search_id`。同键同文重试复用 ID；同键异文 409；GET 固定 ID 只回查，不产生新工作。默认总预算 4 次搜索、24 次来源读、32768 Unicode 码点引文；单次最多 8 次读、8192 码点。每个新子操作在 PG 父行锁下预扣完整上界；失败、超时及重复派发不返预算；仅 RTW 验证成功后按实际用量原子退还未用部分。正在运行返回 202，失败可原键重试并保持子 ID。Bun 的内存预算只负责即时 UX，RTW PG 才是累计额度权威。

## RTW → BTW 搜索线格式

配置 `SearchTools.Endpoint` 必须是私有 HTTP(S) URL，路径严格为 `/v1/search/tools/search`；`ScopeKey` 至少 32 字节，独立于客户端和模型。RTW 发出单值 `X-Sea-Search-Tools-Scope`，其值为无填充 `base64url(payload_json) + "." + base64url(HMAC-SHA256(scope_key,payload_json))`。BTW 必须在 SourceReader/Runner 前验证签名、唯一头、规范编码、受众、时间窗、完整身份和请求 hash；不接受 body 或模型覆写签发范围。

请求 body 是 Go `json.Marshal` 的固定字段顺序、无空白 JSON：

```json
{"module_id":"module_id","query":"...","depth":"fast","intelligence":"low","search_id":"search_<RTW ID>","limits":{"read_calls":8,"quote_runes":8192}}
```

签名 payload 的固定字段顺序是 `aud="btw.search.tools.v1",subject_ref{authority_id,tenant_id,subject_id},session_id,operation_id,budget_ref,search_id,snapshot_ref,snapshot,allow_partial,allow_lower_intelligence,request_hash,issued_at_unix,expires_at_unix`。`request_hash=hex(SHA256(规范 body JSON))`；`snapshot` 含 RTW 当前发布的全部三路引用与有效修订；`snapshot_ref="snapshot_"+hex(SHA256(Go json.Marshal(snapshot)))`。签名 TTL 不超过 120 秒，且不超过父截止时间。BTW 必须使用 body 的 RTW 固定 `search_id` 运行 typed 快搜/详搜、同版 SourceReader 与引用接纳，不得用进程内 ToolSession 序号另造 ID 或把其内存余额当成父累计额度。

BTW HTTP 200 返回**裸 JSON，不是最终答案**：`search_id,status,stop_reason,snapshot_ref,requested_intelligence,effective_intelligence,evidence[],gaps[],conflicts[],pack_hash?,citation_receipt?,usage`。`status=complete|partial|empty`，`stop_reason` 非空；三个数组即使为空也必须显式为 `[]`，不能省略或填 `null`。当前 `conflicts` 只能为空数组，因为耐久 EvidencePack 尚未定义可验证的冲突字段。每个 evidence 是 `{evidence_id,revision_id,locator,quote,quote_hash,source_kind}`，`locator` 是原位置的字符串。`usage={read_calls,quote_runes}` 为本次真实来源尝试和公开引文字数。非空结果必须有 `pack_hash` 和 `{search_id,pack_hash,durable_ref}` 收据，空结果两者均不得出现。RTW 在持有发布行锁的提交点核对本仓 `knowledge_search_citations` 内的原始 `pack_json`、hash、收据、完整同版快照、Profile 深度/智能等级、逐项原文、引用有效状态及预算上界；没有耐久接纳的 BTW 200 不会公开 quote。HTTP 回执丢失时，原键以同一 `search_id` 重新调用，由 BTW/RTW 的引用接纳幂等合同恢复。

`POST /v1/knowledge/answer-sessions/{session_id}/tool-runs/{operation_id}/evidence-reads` 只接受 `{search_id,evidence_id,idempotency_key}`。RTW 必须找到同一父操作已完成子搜索中实际公开的 evidence，再从已接纳 pack 与固定发布的原始 chunk 重读，核对 quote、hash、locator 和收据；在同一父行锁下扣 1 次读和相应引文字数。相同读键回放不再扣费；跨父、未知 evidence、撤回后及父过期均不会重新公开。此产品重读由 RTW 权威存储与同版 Reader 完成，不依赖 BTW 跨进程内存 ToolSession 的搜索映射；BTW 进程内 typed `read_evidence` 仍可用于同次 Agent 调用。

## 首批RTW提供端验收（历史阶段）

本分支包含 RTW schema/API/迁移、独立 PG 幂等与预算测试、已发布引用及同版重读测试、真实 User Center HTTP 身份门禁。提交 `5043c31` 后运行 `KNOWLEDGE_REAL_USER_GATE=1 KNOWLEDGE_KEEP_EVIDENCE=1 bash service/knowledge/scripts/acceptance.sh`，退出码 0：`service/knowledge/...` 和 User Center 相关 Go 测试均以 race 模式通过，两个范围的 `go vet` 通过，`git diff --check` 通过。单独 `go mod verify` 返回 `all modules verified`；goctl 1.9.2 生成脚本复跑后未引入额外生成差异。生成的 TypeScript 契约尚未在 WhaleHall 消费端类型检查。

真实 User Center 用例验证活跃 JWT 创建父操作、同键回放、跨用户 404、停用旧 JWT 403、RPC 缺失/不可用 503；隔离真 PG 用例验证新父固定人工发布、并发同键唯一子 ID、不同键锁下不可超支、失败原键重试、完成时按验证用量只退款一次、伪造引文拒绝、同版原文重读幂等及撤回后不再公开。HTTP 夹具返回伪造带引文 200 时，RTW 仅回 503 且没有证据；夹具返回真实空结构时，RTW 公开空证据但不产生引用收据。这些证明 **RTW 提供端 `LOCAL_VERIFIED`**；夹具不等同于 BTW 生产进程。BTW Tools HTTP 消费者、WhaleHall Bun Port 和真实跨仓父 Agent 运行还需各自 writer 对此冻结线格式实现和联验；此前不能将 H02 Tools 产品链标为 `ACCEPTED`。

## 真实BTW消费者的空证据与有证据子链

RTW集成树设置`SEA_BTW_TOOLS_CONSUMER_ROOT=/Users/edy/Sea/.codex-worktrees/sea-btw-runtime-content-20260914`、`KNOWLEDGE_REAL_USER_GATE=1`、`KNOWLEDGE_KEEP_EVIDENCE=1`并限Go并行度后运行完整`bash service/knowledge/scripts/acceptance.sh`，退出码0；真实User Center版及gRPC替身版`TestRealHTTPKnowledgeWorkflow`分别PASS（48.86秒、23.26秒），知识/User Center race与vet均通过。前一次联验已走完两条子链，只因总引用指标测试仍按旧计数而失败；修正期望为“原有引用+本次有证据Tools的一笔”后全套复验通过，同键回放不新增提交。

父测试先经RTW产品API固定本人/会话/发布/预算，透明转发自身签发的四字段以外的Tools body与原始HMAC头至独立BTW HTTP测试进程。空结果经原生tRPC Graph/Runner返回`empty`，RTW PG无引用行；第二个BTW进程只拿RTW**已发布chunk的修订、chunk ID、quote hash**，通过RTW Worker HTTP重读同版原文，再由真实`RTWSearchCitationAdapter`接纳引用/收据，返回`complete`结构证据而非答案。RTW从自身PG核对exact pack/receipt、公开quote与有效修订后，按1次真实读和实际引文字数退款；同键POST/GET不重跑BTW，跨主体GET404。产品`evidence-reads`再从RTW耐久pack和原文重读，同键只扣一次读与引文预算。BTW子进程退出时核原生根Agent Span、读源/引用接纳次数。

候选仍由隔离测试按真实RTW发布chunk标识**确定性指定**，未由Tools三路真实索引召回；BTW此测试入口不是正式`cmd/api` socket，WhaleHall Bun/Mastra父Agent尚未消费，`continue_search_id`仍不支持。故只标两条跨仓子链`INTEGRATED`，H02 Tools整体不标`ACCEPTED`。
