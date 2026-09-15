# Wiki FactSet 跨 RTW／DC／BTW 真实来源 Holder

状态：**test-only 静态交接，待 BTW 同一质量 consumer 的新 FactSet ODS 读面固定后串行真 PG16×DC Eventing×BTW 验收**。基 RTW 知识开发集成 `1510e1e5672b117ad9158579f7446e669d2d5ed7` 建独立测试叶子；不改变 RTW 源模型、旧质量 Event、DataCenter 或 BTW。只有 `SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT` 明示时才增加本夹具，且必须同时给已固定的 `SEA_DC_EVENT_PLATFORM_ROOT`；共享 DC root 单独存在不会启本 Holder、旧质量 Holder或搜索判定 Holder。该 selector 与旧质量／搜索 Holder及单机 `KNOWLEDGE_FACT_SET_REAL_HTTP` 夹具互斥，避免两个消费剧本覆盖同一 producer cursor。两开关缺省时原HTTP工作流、修订分页和Compile列表保持旧断言。

同一临时 RTW 管理员 JWT+名单/PG16/共享 Local 对象里，已有基线人工 Wiki 的一次质量判定 Event 继续保留。显式 Holder另建含四空格与CRLF的第二 Source；RTW `ACCEPTED` AI Compile固定SourceA+SourceB、Wiki引用两份；管理员为该**固定WikiRevisionID**冻结 `facts_complete=true`的FactSet v1，逐项服务核两个必需Fact的`paragraph:2`原句SHA与首次精确byte span，分别为这两个Fact向同一Wiki目标录入独立质量 `judged.v1` 修订/Event。目录 `fact_set_revision_id/source_scope_revision/FactSet payload JCS`与两条判断的目标Wiki/FactID逐项对齐；已有基线人工质量 Event可以落ODS，却**不能**算作目标两个必需事实中的一个。`facts_complete`仅管理员声明，合成Grade3不是客观真人审核，也不给D07页面覆盖率。

```text
RTW Admin HTTP + 原Wiki/Source对象 → FactSet目录Event + 目标必需Fact质量Event×2
原工作流基线人工质量Event×1   ┘
RTW原Outbox → 真DC Eventing ridethewind.knowledge offset 1..N
             → BTW同一 btw-warehouse-wiki-quality consumer
                Catalog原Event三SHA+SourceScope/FactSetRevision PG ODS
                三条质量原Event双SHA/目标2子集 PG ODS
                N-4其他技术Event显式skip → 同事务提交后 ACK N
             → RTW私有原字节与历史目录、两页编辑head、活动Release回读不变
```

一次性 fixture 是父测试目录中绝对路径、`O_EXCL`新建的普通 `0600` JSON文件，字面11键：`rtw_url,rtw_token,dc_url,dc_token,expected_events,catalog_event_id,quality_event_ids,catalog_quality_event_ids,result_path,fact_set_revision_id,source_scope_revision`。`quality_event_ids`列全部3条人工质量 Event（基线1＋Catalog目标2），`catalog_quality_event_ids`只列目标必需Fact的2条，并非3条任意子集。RTW将该路径只以 `SEA_RTW_REAL_WIKI_FACT_SET_FIXTURE`注入外部 `bash internal/warehouse/wikiqualitysource/acceptance.sh`；令牌不进日志、报告或仓内文档。外部结果也须为普通 `0600`，回`committed_offset,acknowledged_offset,catalogs,judgments,technical_skips,catalog_event_id,quality_event_ids,catalog_quality_event_ids`；父测试要求 Catalog1、判断3、技术skip=`N-4`、同producer完整PG offset count/min/max=`N/1/N`和同consumer ACK=`N`。RTW单独回读Catalog原Event Raw/JCS/FactSetPayloadJCS、全部质量原Event Raw/JCS、Catalog固定历史ID和当前scope head、目标两个Wiki/FactID/原对象byte span；DC查四个确切EventID及基线→Catalog→目标判断真实offset；结果无法通过人工声明替代这些来源事实。

现BTW固定旧质量 ODS consumer对 `knowledge.wiki.fact-set.*` 明确 fail closed，所以**先**签同consumer新 Catalog 原Event核验与PG落行/ACK，再启RTW本测试父轮，更不能在生产上先开放 `WikiFactSets.Enabled`让未知类型卡住连续游标。成功的本机轮也只说明原事件数据与技术收据同源，不能证管理员实时UserCenter UID、真人清单无漏、D07、ClickHouse/dbt或生产。RTW质量actor沿现有Auth JWT+名单的`userId`字面值，无租户字段；源旧质量Event/编辑head与人工Release指针不被FactSet技术消费改写。
