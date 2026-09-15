# Wiki 编制请求到 DataCenter Jobs：技术交付候选

状态：**LOCAL INTEGRATED / 正式 AI 编制仍 PARTIAL**。RTW 知识开发源 `1de00e9` 后的本分支只处理已提交 Wiki Compile 业务请求的技术 Job 提交、原 Job 查询与取消。此候选默认关闭；只有 dev/test、loopback PostgreSQL 与显式 loopback `/v1/jobs` 服务 token 可以开启。`Mode=pro`、远端 Jobs URL 或缺侧表会在服务启动前拒绝。它不调用模型、不接受编制候选、不建立 WikiRevision、Release 或活动发布指针。

## 所有者和固定合同

`CreateCompile` 仍由 RTW 管理员 JWT/allowlist 入口调业务事务：核 module/page 的 `base_revision_id`、已获准的 `source_revision_ids` 与指导，冻结 `compile_id`、`generation`、`input_hash`，同事务写原 `knowledge.wiki.compile.requested.v1` Outbox。Outbox 的 `event_id/event_type/aggregate_id/payload/correlation/created_at` 受 SQL trigger 保护；技术交付只能填 `delivered_at`，不能用同一 EventID 改源体。启动候选时，原 H04 dispatcher 排除 requested/cancelled/superseded 三种编制事件，独立 Jobs dispatcher 才领取它们；开关未启用时原 Eventing 发送行为照旧。已由旧 Eventing 路径标交付但无 Jobs 映射的 Compile 事件阻止候选启动，不会将旧 H04 receipt 当成新 Job receipt。

固定 DC JobType 是 `content.wiki-compile.v1`，本机技术 ResourceProfile 为 `cpu`。Submit 的8个 typed JSON 字段为 `producer=ridethewind.knowledge`、`operation_id=<原requested Event.OperationID，command:64hex>`、`run_ref=wiki-compile/<CompileID>`、`job_type`、`resource_profile`、`input`、`deadline=<原Event.OccurredAt+2h>`、`max_attempts=3`。HTTP `Idempotency-Key=operation_id` 是第9项头部约束，**不参与** Submit JCS hash。deadline 从源事件时间冻结，重试不得把它改成当前时间。该 deadline 过期或已有不同 Job input 时进入明确阻断，不造新 Compile 版本或跳过事件。

`input` 仅是一份 `schema_version=rtw.wiki.compile-ticket.v1` 的 RTW 来源票据，精确包含 `source_event_id,source_event_jcs_sha256,compile_id,module_id,page_id,base_revision_id,source_revision_ids,guidance_sha256,compile_input_hash,generation,cancel_version`。Sources 非空且严格升序；`guidance_sha256=SHA256([]byte(Compile.Guidance))` 是原 UTF-8 字节摘要；generation 是规范正 int64 十进制字符串，初始 cancel_version 必为字符串 `"0"`。票据不含原 Source 正文、指导原字节、模型 key 或模型会话凭据。BTW 必须用 RTW `GetCompile/GetRevision` 再核完整冻结业务字段及实际原文，不能把 DC 票据当来源替身。当前 RTW `GetCompile` 尚不回原 Outbox EventSpec/hash；BTW 可核票据 hash 形状和源业务字段，**不能独立向 RTW 回读 `source_event_jcs_sha256`**，这是继续推进正式 source ticket 读口的交接限制。

RTW `Compile.InputHash` 来自 module/page/base/source IDs/guidance 的业务输入；DC `Job.InputHash` 是整个 Submit8字段 JCS/SHA-256，两者不能互换。DC 首次 Submit 返回201、原键同体重投返回200并保持同 `job_id/input_hash`，异体返回409。DC 目前没有按 `(producer,operation_id)` GET 的公开端点，因此丢 Submit 响应时 RTW 保留原 Outbox pending，再以冻结原键/原体重投取得原 Job UUID，随即 GET `/v1/jobs/{uuid}` 复核完整 Submit 与 DC input hash。RTW 将原 JobID/submit hash/source event hash 写单调技术状态的 `knowledge_compile_jobs` 侧表，与旧 Outbox `delivered_at` 在同一 PG 事务提交；本地 ACK 丢失会一起回滚，DC 已存 Job 在下一次同键回放恢复。

DC Claim 真正给 `attempt_id,lease_epoch,cancel_version,lease_expires_at`；BTW 必须连同 RTW 业务 `compile_id,generation,Compile.InputHash` 交 `ClaimCompile`，不能把 DC Job hash 代入 RTW InputHash。管理员 `CancelCompile` 在 RTW 业务层把状态设 CANCELLED、cancel_version 加1且写原取消 Outbox；技术侧再按原 JobID发 DC Cancel，核同 Event operation ID、预期 DC版本0与返回版本1。运行中的 DC Job 可暂为 `cancel_requested`，由旧租约持有者带新取消版本 ACK 后才 `cancelled`。侧表的 technical_state 只是上次取消回执状态，DC GET 才是即时 Job 状态；DC 取消 HTTP 回执丢失时 RTW 取消 Outbox 保持 pending，再用原取消 operation ID、原预期版本0同体回放原 receipt。旧 RTW `AcceptCompile` 和旧 DC Complete 都必须被栅栏拒绝；DC 技术 Cancel/Complete 的成功不生成 Wiki 修订。

同页再建 Compile 时，RTW 原模块锁事务收集并锁定旧 BUILDING 候选，先把每个旧候选设 `SUPERSEDED`、`cancel_version+1`，以旧 Compile、replacement CompileID/generation 产生 `knowledge.wiki.compile.superseded.v1`，随后才写新一代 requested Outbox；任一新写失败整事务回滚。两条同事务事件的 `created_at` 相同，受控 Jobs 以来源 module `aggregate_version` 定序；默认 H04 只为Wiki类型按此版本打破同事务并列，其余Eventing类型保留原 `created_at,event_id` 顺序。

默认 H04 的 Sender执行前还核替代/管理员取消的原requested Outbox必须恰好一份且已交付、新requested须等待其replacement旧supersede Outbox交付；多实例跳锁选中后继只记固定pending，不调用DC也不占offset。

受控 Jobs 多实例 `SKIP LOCKED` 即使跳过已被锁的旧 supersede 行而选中新 requested，Submit 仍按 replacement CompileID 查未交付的旧取消事件并在 DC 副作用前返回固定 `PREDECESSOR_CANCEL_PENDING`；旧 Job 原取消 receipt/Outbox 已提交后才允许新 Job。旧 requested 若尚未技术交付，仍先按原票据提交 Job、再取消；BTW 应先做 RTW Claim 栅栏再请求模型，不能让已替代的旧任务消耗模型额度。

```text
旧请求 Outbox -> DC 原 Job receipt -> 旧 Job 被领取
同页新 CreateCompile 事务：旧 Compile SUPERSEDED/CV1 + 替代 Event -> 新 Compile Gen2 + requested Event
旧替代 Event -> DC Cancel 原 Job/CV1 -> 新 requested Event -> DC Submit 新 Job
旧 Attempt 的 RTW Accept / DC Complete：栅栏拒绝；Wiki 修订与人工发布：不变
```

**凭据所有者分离**：Jobs HTTP 仅使用平台授予的 DC service job token；正式 BTW Wiki 模型调用需要另由 DC 平台 owner 授权/计量的 native service-user bearer 与 `knowledge-wiki-compiler` CallPoint。RTW JWT、DC job token、RTW UID 及 DC UUID 都不能替模型会话身份，不能猜两种账号同人。成功 `jobs.ResultRef.URI/Hash/MediaType` 与 RTW `AcceptCompile` 的不可变修订/结果 hash 尚未冻结跨仓协议，BTW 正式 worker 在 RTW 业务 Accept 后不能自造 `rtw://` URI 或用 `ResultHash` 冒充内容 Hash 来技术 Complete/ACK；另由结果合同 owner 签收。

## 同次局部验收

固定来源头 `c58d7a2`（基础票据/技术交付 `8d98496`，旧Job回收 `e7f30a7`，默认H04顺序/跳锁前驱 `a3c5bd1/c58d7a2`）× DC 平台 `161d218`，运行：

```bash
KNOWLEDGE_PG_BIN=<PostgreSQL-16-bin> \
SEA_DC_JOB_PLATFORM_ROOT=<isolated-DataCenter-checkout> \
bash service/knowledge/scripts/wiki_compile_job_acceptance.sh
```

2026-09-15 脚本从本独立分支固定头顶层退出0，真隔离 PG16 + 实际 DC `cmd/platform`，新增 Wiki 用例及原 Compile/Outbox 聚焦回归 PASS、`go vet ./service/knowledge/...` 与 `git diff --check` 退出0。实际请求 Job1 在 DC 落库后模拟丢 HTTP 回执，RTW 原 Outbox仍pending/侧表0；重投原 Submit 从 DC200 恢复 Job1。随后模拟 RTW PG Outbox ACK失败，同侧表一并回滚；第三次原体重投仍为 Job1/GET全字段相符。同 operation ID 改票据被 DC409 拒绝。真实 DC Claim 的 AttemptID/Epoch/Expiry/CV0 给 RTW ClaimCompile；管理员取消 CV1 使 DC运行 Job `cancel_requested`；取消 HTTP 回执丢失后 RTW侧表仍CV0、Outbox仍pending，再按原operation/版本重放DC原取消receipt并ACK；旧 RTW Accept/旧 DC Complete 均409，持有者 DC cancel ACK 后 `cancelled`。最终 RTW WikiRevision 为0，原 requested EventSpec JSONB 字节未改，提交后尝试改体被源 trigger 阻断。同页旧 Job 已领取时，新Compile原事务先把旧候选标SUPERSEDED/CV1并发旧替代Event，旧DC Job先cancelRequested，再提交独立新Gen2 Job；旧RTW Accept/旧DC Complete 409、Wiki/人工发布0。另以真PG锁住旧替代Outbox，让第二派发实例跳选新requested，固定前驱pending门禁使DC Job数仍只有旧Job，解锁后按序恢复。Jobs开关关闭时连续同页两代的旧requested<旧superseded<新requested沿原H04 Sender到真实DC Eventing JCS/hash/offset有序；另分别锁旧requested和旧supersede，第二H04派发跳锁选中后继均在Sender前pending，对应DC GET404/零新offset，解锁后恢复原序；Jobs侧表0/WikiRevision0，非Wiki事件路由保持原样；pro/远端配置早拒。

RTW 输出包含 `knowledge.compile.job.submit` failed/succeeded、`knowledge.compile.job.cancel` succeeded 的 JSON 终态与稳定 operation/event、trace/span；RTW 代理观察到 OTel `traceparent` 确实进入 DC HTTP。Prometheus 的固定 operation 错误/成功计数存在，动态 CompileID/EventID 不成为 label；DC Job 技术交付不增 `sea_knowledge_commits_total` 的 Wiki 领域转移数。这是本机 OBS，Collector 查询/告警与正式 AI 模型原生 Span 另验。

`test.log` SHA-256 `482636cb73e701e5117e9b69b58558f787d992d61341145b9bb999de8f87713f`；脱敏真 DC 票据/Submit/双 hash/Lease/Cancel JSONL SHA-256 `09248b639a10330e0a09394d86eaf083fdae485569f58b03f5fa9260966b946a`（脚本 Evidence directory 下 `observability/wiki-compile-job-ticket.jsonl`）；PG 停机日志 SHA-256 `ca19178a35ab4153b75b494963b66ce8243e1b94c173107c87b44d09db23652d`。停机后 `pg_ctl status` 退出3。脱敏 fixture 无 token、指导原文、Source正文或模型 key，可由 BTW owner 直接和严格 decoder 对字面键/JCS。

## 仍待签收

- 本机 CreateCompile 测试使用隔离管理员 actor 夹具，未证明实际 Next 管理员 JWT 的创建/取消浏览器旅程；该业务路由在现源码继续由 Admin JWT 与 allowlist 保护。
- BTW 正式 Wiki 编制 Runner/Graph/LLMAgent/typed Source Tool、模型 native service-user bearer、真实模型成本/结果对象、RTW `AcceptCompile` 成功与 DC 成功 ResultRef/Complete、S3 共享原字节和人工 Release 发布尚未同链运行。
- 同页旧 Job 的源取消与新Job顺序现只在本机真DC/PG16签收；默认H04的新`superseded.v1` EventType在尚未上线的Wiki消费者中需按同producer连续版本显式接纳。生产旧Wiki资料、水位、Collector、真实BTW模型执行、跨仓ResultRef/S3与线上切换均未运行。正式 Wiki AI 编制状态维持 PARTIAL。
