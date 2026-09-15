# Wiki 人工事实评阅 v1：RTW 源头交接

状态：**独立开发候选，本机隔离 PG16 与真实 go-zero 管理/Worker HTTP 局部验收，质量产品全项尚未签**。基知识开发集成 `dac0bb29706c2150623f428125749befb9f452cc` 开隔离分支。RTW 只记录管理员对一条固定来源事实在**一个不可变 Wiki 修订**中的人工判断；不生成候选、不代替 BTW 评测、不改 Wiki 正文编辑 head 或已发布 Release。

## 领域与产品等级

`fact_id` 由 RTW 对 `SourceRevisionID + paragraph:N + source_quote_sha256` 计算，客户端不得自行赋值。同一来源事实可在 AI 接纳修订和人手改稿中分别评阅，独立主键为 `(wiki_revision_id,fact_id)`。人手修订标“缺失”的来源事实仍须属于该页当前修订的 SourceRefs 或其不可变同页 base 链/AI祖先获准SourceIDs（v1最多64层），不能从同模块任意不相关资料虚构缺口；事实全集另待独立FactSet收据。每次重判须给当前`base_judge_revision_id`，只推进该主键的评阅 head、追加`judge_revision_id`和规范十进制字符串`judge_revision`；它与 Wiki 正文编辑 revision、活动 Release 指针无关。AI 目标从 `created_by=btw.compile/<id>` 回读 RTW `ACCEPTED` Compile 及获准 SourceIDs；人工目标 `origin_compile_id` 必须为空，核同模块/页面的 Wiki 来源、SourceRefs、编辑 base 与人手创建 actor，不拿旧 AI CompileID 冒充当前人手修订。

`rubric_version=sea.wiki.fact-coverage.v1` 的 `grade` 只表示**此来源事实在目标 Wiki 修订中的表达与引用质量**，不是模型置信度、检索相关性或 D07 页面通过阈值。`missing|conflict` 必须是 `0`，`covered` 可为 `1`（表达/引用不完整或歧义）、`2`（事实正确且精确引用，限定/上下文仍不足）、`3`（事实完整、限定清楚、引用忠实且便于维护）；`undetermined` 不带数字等级。人工`reason` 必须说明证据及保留问题。Wiki 或 Source 后来撤回时仍保留旧评阅与原对象锚，新复查只许`undetermined`无grade，不把撤回后的来源再次给积极等级。管理员身份目前仅经 RTW 现有 `Auth` JWT `userId` 与 `AdministratorIDs` 名单；**没有实时 UserCenter RPC 撤销核验**，Event 的`judgment_source=human_admin_jwt_allowlist`如实写明资格来源，不添加 tenant/组织字段。

每个 Event 只判断**一条**来源事实，不能从收到 N 条事件推断某页/SourceScope 的应覆盖事实全集已经列全。RTW 后续需另签版本化 FactSet/Scope 完整性收据或真人完整覆盖证明，BTW Dataset/D07 在该收据缺失时仍`not_evaluable`；本切片不填页面达标率和未经真人样本测量的数值阈值。质量判断也不会自动批准 Release；只有管理员在三路同代索引 READY 后执行手动发布。

## 字节、事件与回读合同

产品 POST、单fact GET、按`fact_id`排序的列表 GET 与当前编辑 head GET 均在现有管理员 JWT+名单路由组；列表`limit`默认20、最大100，`cursor`为上页最后FactID。POST 从真实 SourceRevision 原对象校 `source_content_sha256`、`source_quote_sha256`和合法`paragraph:N`，使用 RTW `citationParagraph`选段，再在该段**原 UTF-8 字节**内选引文首次精确出现的位置。`source_byte_start/end`是引文自身的绝对原字节范围，不是整段边界；重复同引文固定首次匹配。WikiClaimText 如有须是目标 Wiki 修订原正文的精确子串，其摘要独立于 SourceQuote 摘要。Source/Wiki 原对象 SHA、RTW Compile业务hash、DC Job整个Submit hash、质量Event raw hash及JCS hash分别在各自域内核，不相互代入。

原 Outbox Knowledge EventSpec 九键与 Schema1/Producer `ridethewind.knowledge`不改。新 `event_type=knowledge.wiki.quality.judged.v1`，payload `schema_version=rtw.wiki.quality-judgment.v1`固定32键，按关系分为：

- 评阅身份/版本：`judgment_source,judgment_id,fact_id,judge_revision_id,judge_revision,base_judge_revision_id,module_id,page_id,wiki_revision_id,base_wiki_revision_id,wiki_origin_kind,origin_compile_id,actor_id,judged_at`。
- 原事实与目标：`source_revision_id,source_content_sha256,locator,source_byte_start,source_byte_end,source_quote,source_quote_sha256,wiki_content_sha256,wiki_claim_text,wiki_claim_sha256,citation_present`。
- 人工判断：`assessment,grade,rubric_version,reason,source_withdrawn,wiki_withdrawn`，另有`schema_version`。

RTW `command()`同键串行、同体重放原收据、异文冲突；先查持久回放再读资料，因此撤回后旧命令仍可恢复。新评阅修订、独立 head CAS、`emitWithReceipt` Outbox 及质量 Event 侧表在**同一个 PG 事务**提交。侧表以 text 留 `event_json` 原发射字节、其 `event_raw_sha256`和对 RFC8785 规范化后字节的`event_jcs_sha256`；Outbox 主列是 JSONB，不能重新序列化来冒充原Event字节。`GET /internal/v1/knowledge/wiki-quality/events/:event_id` 仅由既有 Worker token 读，校 sidecar/outbox/修订一致、原Wiki/Source对象字节与所记引文 span，回原 EventJSON 和双SHA。新 Outbox触发器只冻结该质量Event原身份/正文/correlation，技术侧允许第一次填`delivered_at`；原Source/Compile EventSpec、旧EventID/hash没有原地改动。

静态 Synthetic Golden 为 [wiki-quality-event-v1.json](../../testdata/wiki-quality-event-v1.json)：Source 原文含四空格和 CRLF、段内短引文`short fact`固定原字节`21:31`。**原EventJSON不带文件尾 LF**的 rawSHA=`b8a7f76b4abd10555f2b3f450963bbf518b9919491b714cc909ecb9508b3ac5f`，JCS SHA=`90e12c15843c7a1ad848da83edf97d4bce218f95eaa16d90afc2f26af02cd7a6`；fixture文件自身含尾LF SHA=`6b54655324aadfea697e0cfe6fdd3a690be9f396b55563142cc8df428bab1f32`，三者不可混用。BTW/数仓消费者须核原 Event byte/hash、JCS hash、Source/Wiki固定修订与事实引文，再以 DC 连续 offset 前缀去重/ACK，不能只收技术 Job 回执或活动索引文字。

## 默认关闭与验收

`WikiQualityJudgments.Enabled=false`是旧环境默认。旧Wiki/Source/Compile读写与活动Release保持原模式；现有数据库要显式运行可重入 [migrate-wiki-quality.sql](../../scripts/migrate-wiki-quality.sql)后启用，启动会探测独立评阅修订/head/Event表，**不回填任何历史真人标签**。管理员 POST 输入只许有限平坦字符串键，HTTP边界先拒重复/未知/多尾，再由模型核来源、Grade/类别、Origin及CAS。OBS将`knowledge.wiki.quality.judge/get/list/event.read`和`knowledge.wiki.head.get`注册为闭集operation；GET为只读，不计领域提交。另一个小fix把既有SearchJudgment原未登记operation改为真实record/withdraw/event.read，Event读不会误报领域commit；阶段输出同源JSON/OTel/Prom，不用fmt直接吐业务状态。

本机任务专属 PG16 首轮模型/坏输入回归通过，随后真实go-zero HTTP聚焦race通过：管理员无JWT401、非名单403、坏SourceSHA/重复评阅键400、合法POST200/同键重放200、单fact/list/编辑head GET200、Worker原Event私有401→workerToken200；原知识流程同轮继续通过。HTTP/模型 test.log SHA=`974b2b9759c58daa05f476f14d80dd7dfdd6c9182a7be92807498f4371b6fcac`，停止日志SHA=`ca19178a35ab4153b75b494963b66ce8243e1b94c173107c87b44d09db23652d`，证据`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-acceptance.Kk7JVh`。独立再次聚焦模型race通过 default-off/迁移两次不回填、Outbox故障使评阅修订/head/Event/幂等回放全事务回滚，test.log SHA=`ed7ac854da3dd0f8693a906a2de2212be613037c68f8b352a3468ba7a1401066`，PG stopSHA同上，证据`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-acceptance.0VyZXY`。最终同一任务专属PG16聚焦源/HTTP/Golden/坏输入/人工无关来源/迁移/Outbox故障回归再次顶层exit0：`TestRealHTTPKnowledgeWorkflow` PASS3.15s，质量模型七类核心/反例均PASS，test.log SHA=`904cc9acc45d2f9f7bf82ffd95bac2ecddb16dff7bad64684c79845a9f5906e5`、HTTP结构JSONL SHA=`ec55f5a81ea8290d9051b42136e6aa7df8f9ee199e4e6030fbb9118a4c1d00f5`、PG stopSHA=`ca19178a35ab4153b75b494963b66ce8243e1b94c173107c87b44d09db23652d`/server stopped，证据`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-acceptance.ajBL7M`。锁定goctl1.9.2对API/routes/types/OpenAPI/TS再生成后六文件SHA逐个字节不变；`go vet ./service/knowledge/...`、`go mod verify`与diff check退出0。这签本机L1/L2人工来源与权威HTTP，不签真实管理员样本分歧、Web双会话浏览器、RTW→DC→BTW/CH数仓同父L3、FactSet完整性、三路检索qrel或生产身份/对象权限。
