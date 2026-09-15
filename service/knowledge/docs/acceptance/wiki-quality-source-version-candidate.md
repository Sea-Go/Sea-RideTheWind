# Wiki质量RTW源版本候选与私有交接

本合同只交付RTW一个读时源版本`V`的候选。它没有`as_of_dc_offset`、真人事实完整性或D07质量结论。DataCenter与BTW须另以同一已ACK完整前缀证明EventID→DC offset、所有`<=V`的相关事件在截止`C`内、该module的`>V`事件不在`<=C`、目录/逐Fact最后运输判断与读时head一致，以及撤回Event重建的资格状态一致。

工作区域：`[W0:ROOT]`本独立RTW知识worktree；`[W1:WRITE]``api/knowledge.api`、`service/knowledge/api/internal/model/wiki_quality_source_version_witness*`、`schema.sql`、Worker叶子逻辑、`service/knowledge/scripts/migrate-wiki-quality-source-version-witness.sql`、本交接及验收脚本；`[R1:READ_ONLY]`BTW SourceProof/DC ACK与Sea-Docs；`[D1:DEPENDENCY]`go-zero v1.10.2、pgx v5和锁定goctl1.9.2；`[G1:GENERATED]`Go types/routes/handler scaffold与OpenAPI/TS只由`scripts/generate.sh`产生；`[X1:EXTERNAL]`仅自启停的loopback隔离PG16，生产与共享实例未写；`[N1:OUT_OF_SCOPE]`RTW开发集成、DC、BTW、网页、Docs及已有用户改动；`[T1:TEMP]`本任务验收目录，仅测试生成物，保留脱敏日志。主职责`[C4:PERSISTENCE]`，跨`[C1:TRANSPORT]`Worker Token与HTTP、`[C7:CONTRACT]`私有wire、`[C8:VERIFY]`PG/race与旧行为。

## 私有Worker合同

`POST /internal/v1/knowledge/wiki-quality/source-version-witness/read`只接受已有Worker service token。请求必须显式给`module_id,page_id,fact_set_revision_id,wiki_revision_id,source_scope_revision,source_version`。没有Wiki FactSet与质量功能装配时返回不可用；错误、旧V、坏修订、缺Outbox或超限都不得发成功候选。API DSL是权威源，生成由固定`goctl 1.9.2`脚本维护，路由和SDK不手改。

响应`schema_version=rtw.wiki-quality-source-version-candidate.v1`，含`source_version=event_sequence_at_read=V`、固定历史Catalog EventID及原raw/JCS/payload JCS SHA、完整SourceRevisionID/SHA/撤回位、required事实当前JudgeRevisionID/EventID及原raw/JCS SHA或明确`present=false`、`module_lifecycle_at_read`、Wiki撤回位、`all_required_have_head`与页组。每页`from_version..to_version`至多128个事件，单响应总至多4096个事件、16MiB原Outbox JSONB；超限拒绝，不返回截短前缀。超大模块的可持久固定Ref与跨请求分页仍需另签，当前4096不能作为最终规模资格。

每Event固定`aggregate_version,event_id,event_type,event_jcs_sha256,jcs_source,delivered_at,target_kind,target_id`。非撤回Event的target字段为空；`knowledge.content.withdrawn.v1`从其完整Event payload严解码`module|revision`目标。目录/质量Event另与各自原字节sidecar的JCS SHA独立核，标`original_fact_set_event_sidecar`/`original_quality_event_sidecar`；其他模块Event标`outbox_jsonb_canonical_at_read`。Outbox的JSONB不能还原原raw SHA，也不能证明迁移保护生效前旧事件从未被修改，不能把这个JCS当原raw字节。既有不可变Event侧表仍保留原raw SHA，DC的whole Event input hash必须和这里的**JCS SHA**对照。

Store只在一个PostgreSQL `REPEATABLE READ READ ONLY`事务中先核固定历史FactSet与Source/Wiki/required当前Judge，随后读取同module当前`event_sequence=V`和Outbox全部`1..V`：行数/V、每条表列与Event内ID/type/module/producer/版本、无洞重版、全部`delivered_at`、目录及每个当前Judge head都在该列表中且JCS同原侧表。它对每条撤回目标核同module不可变Revision当前withdrawn位，再对Source/Wiki/module读时位做Event存在性对账；直接改撤回位而缺事件拒绝。事务Commit失败不返回候选。后续业务写若在事务快照**之后**提交，结果只代表这次读时状态，消费方不能把它借作更晚DC截止。

新增统一Outbox trigger的显式可重入迁移冻结当前与未来事件的EventID/type/aggregate、whole JSONB payload、correlation与创建时间，禁止删行；技术`delivered_at`只允许空→非空一次，旧行不回填也不重编码。现有Compile/FactSet/Quality专用保护仍保留。迁移前必须审已有旧行/送达状态，正式启用及生产水位未由本地PG测试签收。

## 当前本机验收

首次代码轮`bash service/knowledge/scripts/wiki-quality-source-version-acceptance.sh`在新PG16、`-race -count=1`实际通过8个顶层测试，含旧FactSet/质量源、两个RR并发快照与新增Outbox重排/缺版本/重版/撤回/原侧表/Worker 401→200；无SKIP/FAIL。测试日志SHA-256=`17a1d8711969525d6288f0ccadfdf273257752003571df7e7cd4ba6d6ff3e526`，vet/diff均为空SHA=`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`。PG `stop.log`SHA=`ca19178a35ab4153b75b494963b66ce8243e1b94c173107c87b44d09db23652d`，`pg_ctl status=3`，证据保留`sea-wiki-source-version.fXFuQv`。这是未提交WIP的第一轮代码测试字节，不借它签后改脚本。

最终**固定代码头**`2ec691ab80aae64df2ce710d7f52d26ac71d0a8a`另启新PG16，`bash service/knowledge/scripts/wiki-quality-source-version-acceptance.sh`顶层退出0。model聚焦`-race -count=1`实跑11顶层PASS/0SKIP/0FAIL：旧Build Outbox版本/旧FactSet和质量原Event与失败恢复、两个RR并发快照，以及新源V乱序部分送达拒/全版确认、后到重判旧V拒/新V头变、Source/Wiki/module撤回Event和读时位对账、迁移重放两次不改旧Event、whole payload/correlation/已送时间/删行均拒、伪造版本洞/重版与目录/质量sidecar Outbox漂移拒、Worker service-token401/200与旧V拒。model测试日志SHA-256=`7a309e26c553b57008b385a8d3f2b8a20faceb2dd19a241b97b228f14c7ffea6`。同PG另以`KNOWLEDGE_FACT_SET_REAL_HTTP=1`跑真实go-zero `TestRealHTTPKnowledgeWorkflow`，原Admin当前/历史FactSet和Worker原目录/质量Event/旧发布路由1PASS，HTTP日志SHA-256=`80fe5b5f3130a8e9e427917e9b2e5ecd94e8c5669caeb537ec9fc06b9dbe7ef1`；它并未调用新SourceVersion route，新路由的Worker 401/200是独立handler包装真PG测试。vet/diff日志均为空SHA=`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`，生成器1.9.2连续两次DSL/types/routes/OpenAPI/TS字节SHA一致，`go mod verify`所有模块通过。PG `stop.log`SHA=`ca19178a35ab4153b75b494963b66ce8243e1b94c173107c87b44d09db23652d`，停机前状态0、停机后`pg_ctl status=3`且主任务另复核3；Evidence`sea-wiki-source-version.5DRTVi`保留原日志及停止的测试PG数据。该固定头无生产/共享数据写入、无BTW/DC同父；旧HTTP流程不冒新路由真实子进程验收。

验收边界：源候选L2，未接BTW/DC同父、未对生产旧Outbox迁移，未签实际DC截止`C`、FactCatalog真人漏项/D07、Dataset/Serving或线上实例。
