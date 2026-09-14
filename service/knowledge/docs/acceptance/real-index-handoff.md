# H06 BTW 本地三路索引到 RTW 真实接纳：跨仓局部验收

日期：2026-09-14。状态：**隔离环境 H06 构建接纳子链 INTEGRATED；H06 整体 PARTIAL**。本页只证明同一个 BTW 本地READY工件经真实RTW go-zero HTTP与独立PostgreSQL16成为权威build READY，并由DC技术job协议替身核对同Ref；人工发布仍独立。

RTW `TestRealHTTPKnowledgeWorkflow`在自身实际知识服务进程中另建未发布module、一条原文source、固定retrieval_profiles的release与**未claim** build；共享隔离对象目录。只有设置`SEA_BTW_INDEX_CONSUMER_ROOT=<BTW集成工作树>`才把0600 fixture交给BTW `TestRTWRealProviderIndexDispatch`。BTW从RTW真实GetRelease/GetRevision/GetBuild及对象hash读冻结输入，用自身Preparer生成chunk、tRPC-Agent-Go Graph/Runner驱动三路local-exact Build/Probe，经RTW真实ClaimBuild/AcceptBuild/GetBuild；先试过期claim和旧fence结果，都由RTW拒绝且不改build。RTW成功收同一IndexManifest Ref/hash后，BTW的DC typed Job/Represent HTTP**协议fixture**才确认技术ACK，本地outbox最终delivered；同Ref再次调用AcceptBuild不新增第二次领域事件。

RTW父测试从自己的HTTP及PG `knowledge_builds`复核READY/代/Ref/hash，`knowledge_outbox`的`knowledge.index.build.accepted.v1`对该build恰一条；该module `knowledge_publications`为0，当前快照Worker GET仍404。结果文件由BTW只在所有本地与外部检查成功后写出，RTW不以它替代自己的数据库核验。首次联验失败揭示RTW把三个profile按lane规范排序，BTW测试误把请求顺序当回执顺序；消费者改为按lane比较完整字段。第二次父测试对Outbox业务build_id取错层级：表的`payload`列是完整Event信封，实际在`payload.payload.build_id`；改正查询后完整脚本退出0。两次真实失败都保留为合同证据，没有放宽RTW接纳条件。

复验命令：`SEA_BTW_INDEX_CONSUMER_ROOT=<BTW集成树> bash service/knowledge/scripts/acceptance.sh`，脚本创建/停止独立PG16并跑完整知识服务race、真实HTTP与vet；BTW子脚本自启停另一组PG16，跑单个跨仓消费者race、worker vet和diff检查。原同仓`TestRealHTTPKnowledgeWorkflow`、生产知识服务API以及其它原测试仍通过。BTW对应范围见`cmd/worker/RTW_REAL_INDEX_ACCEPTANCE.md`。

**尚未验证**：实际DC BGE-M3而非固定2维表示fixture、真实DC jobs进程、Milvus Dense/Sparse/Multi-vector同代索引、生产对象存储、正式发布指针及搜索消费、Collector下钻、跨DC job的全局build epoch分配和性能/故障恢复容量。因此不得把本次“RTW READY”写成产品已发布或完整H06/J01验收。
