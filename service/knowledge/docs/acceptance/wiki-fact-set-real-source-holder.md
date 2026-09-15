# Wiki FactSet 跨 RTW／DC／BTW 真实来源 Holder

状态：**test-only 独立开发叶子，固定本机RTW×DC×BTW同父验收通过，待RTW知识开发集成新HEAD再复验**。基 RTW 知识开发集成 `1510e1e5672b117ad9158579f7446e669d2d5ed7` 建独立测试叶子；不改变 RTW 源模型、旧质量 Event、DataCenter 或 BTW。只有 `SEA_BTW_WIKI_FACT_SET_CONSUMER_ROOT` 明示时才增加本夹具，且必须同时给已固定的 `SEA_DC_EVENT_PLATFORM_ROOT`；共享 DC root 单独存在不会启本 Holder、旧质量 Holder或搜索判定 Holder。该 selector 与旧质量／搜索 Holder及单机 `KNOWLEDGE_FACT_SET_REAL_HTTP` 夹具互斥，避免两个消费剧本覆盖同一 producer cursor。两开关缺省时原HTTP工作流、修订分页和Compile列表保持旧断言。

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

旧BTW质量 ODS consumer对 `knowledge.wiki.fact-set.*` 明确 fail closed；本机同父轮先固定BTW内容开发集成 `d53d288` 的**同consumer** Catalog 原Event核验与PG落行/ACK，才显式启RTW测试selector。正式生产仍不得先开放 `WikiFactSets.Enabled`让未知类型卡住连续游标。成功的本机轮只说明原事件数据与技术收据同源，不能证管理员实时UserCenter UID、真人清单无漏、D07、ClickHouse/dbt或生产。RTW质量actor沿现有Auth JWT+名单的`userId`字面值，无租户字段；源旧质量Event/编辑head与人工Release指针不被FactSet技术消费改写。

## 固定叶子同父证据

从本测试源码 `0c2d41da8ed820abb0d42bebfe3b989aa201f20d`，BTW同消费者 `d53d288cd6355f51446fab4866d39e410de8af94`，DC真平台 `0e6f0fe0791807538ce317ca18ecb8747d0f615f`，同轮起任务专属RTW PG16、DC该集群中隔离数据库与独立BTW PG16。顶层`service/knowledge/scripts/acceptance.sh`退出0，实际`TestRealHTTPKnowledgeWorkflow`12.42秒通过：DC原Event producer 14条形成`1..14`完整前缀，FactSet目录offset8、两条目录目标判断offset9/10，另1条基线人工判断；BTW `btw-warehouse-wiki-quality` 写入Catalog1/Judgment3、明确技术skip10、同事务落ODS后ACK14，DC游标14。RTW源Worker私有原Event Raw/JCS/FactSetPayloadJCS、三条质量原Event、历史目录ID、两个Wiki编辑head和人工Release pointer1均在运输/ACK前后独立核同。DC平台在父测试中真实`platform.started/stopped`生命周期检查通过；没有调用模型或ClickHouse。

父脚本完整日志SHA256=`f5e10671ef32016eaec7e3eeb713cd8d8351d6fbe8089135bcb0e982d82579d0`，RTW `test.log` SHA256=`71a6296fbdc02021185ad00ebacd675e4aedd41e674e22f54c5b9bec823c3178`，脱敏 `wiki-fact-set-cross-source.json` SHA256=`39c45ef713d572ff1255e348315f2dbf535ef726628aa60e53fcb420fac3303d`，RTW同源HTTP JSONL SHA256=`bb31b0edf056548e21e1988f0de48a473f8eeaeb0ebea8552265b6c8953f021c`，RTW PG `stop.log` SHA256=`ca19178a35ab4153b75b494963b66ce8243e1b94c173107c87b44d09db23652d`、`pg_ctl status=3`；证据目录`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-acceptance.LxUdMy`。外部 BTW 同轮唯一`sea-wiki-quality-ods.EnCyPG`的 `TestRTWRealWikiFactSetSource`真私有HTTP/PG PASS，`go-test.log` SHA256=`9d19293dbc68b18079674d070ee3717b756822d419993d01e2d6f29606276275`，独立PG `stop.log`同SHA、`pg_ctl status=3`；其其它日志保留。静态selector/no-PG测试SHA256=`9c2f9903a0ad6a0693f3027eb17312ea0ae94d4331576842b6c7a5f66c9c0d24`，默认关与共享DC单独不误启、缺根/共选在PG效应前拒。脱敏报告明确`facts_complete_declared=true`但`human_catalog_verified=false,d07_evaluable=false,production_verified=false`。本结果仍是**独立叶子**本机链路；RTW开发集成新HEAD复验、真人事实全集审核和上线资格分别另签。
