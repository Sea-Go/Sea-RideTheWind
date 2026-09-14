# RTW 人工搜索相关性判定来源

RTW 只负责捕获管理员对一个**已经完成并被 RTW 接纳的产品搜索**与一个**该搜索固定发布版本中的片段**所作的判定。`SearchJudgments.Enabled` 默认 `false`。此接口没有导入历史标签、推断点击负例、调用模型打分或把测试夹具声明为人工判定。BTW evaluation 仍负责冻结正式 qrels、候选池覆盖、裁判与基准版本；数仓负责把 RTW 事件加工为可消费数据集。

上线前先在 RTW Knowledge PostgreSQL 执行 `service/knowledge/scripts/migrate-search-judgments.sql`，再显式设 `SearchJudgments.Enabled: true`。启动时检查三张表的必要列；旧库不能悄悄启用。管理员 JWT 与现有 Knowledge `Administrator` 中间件决定 `actor_id`，没有新的身份体系。

## 判定规则与接口

`sea.search.relevance.v1` 的等级定义：`0` 与查询无关或不能支持所问；`1` 主题有关但没有所问的具体信息；`2` 能支持部分答案或关键条件；`3` 直接、充分支持所问。判定依据由管理员填写非空 `reason`，该文字作为审计说明而非模型推断。输入 `grade` 是字符串 `"0"` 至 `"3"`，避免缺失数字被 Go 零值误认成真正的 0；事件中的 `grade` 是数值，撤回事件中是 JSON `null`。

- `POST /v1/knowledge/modules/:module_id/search-judgments`：传 `search_id,content_revision_id,chunk_id,grade,rubric_version,reason,base_revision_id,idempotency_key`；首判 `base_revision_id` 为空，重判必须等于当前头修订。
- `POST /v1/knowledge/modules/:module_id/search-judgments/withdrawals`：传 `search_id,chunk_id,base_revision_id,reason,idempotency_key`；只能撤回当前已判状态，追加 tombstone，不删除旧判定。
- `GET /internal/v1/knowledge/search-judgments/events/:event_id`：现有 Worker token 鉴权，返回写入 Outbox 时保留的 `event_json` 原始字节字符串及其 SHA-256。此接口只回查两种 qrel 事件；其他 Knowledge 事件仍按完整 producer offset 技术跳过。

源查询来自 `knowledge_product_search_operations.request_json` 的原文与 `created_at`，要求 `status=committed` 且有匹配的 `knowledge_accepted_answers`。片段由固定 `snapshot` 的发布记录、READY build、不可变 index/chunk manifest 和 `ReadSearchSource` 验证；管理员不能提交自造文本、修订或发布时间。`content_available_at` 取 `knowledge_publications.created_at`，即该 release 对产品搜索首次可用的时间。查询原文和片段原文均按 UTF-8 原字节计算 SHA-256。

同一 `(search_id,chunk_id)` 只有一个可变头；每次判定、重判或撤回追加 `knowledge_search_judgment_revisions`，与模块序号化的 `knowledge_outbox` 事件及 `knowledge_search_judgment_events` 的原始字节/hash 同事务提交。相同 idempotency key 与完全相同输入返回原回执；同 key 异输入、旧基线、未完成搜索或非发布片段均拒绝。Outbox 写入失败时修订、头与幂等回执一并回滚。Outbox 投递确认只说明技术接收，不能表示 qrel 已入仓或被评测采纳。

## 数仓交接

事件 `producer=ridethewind.knowledge`、`aggregate_id=module_id`、`aggregate_version=module event_sequence`；类型为 `knowledge.search.judgment.revised.v1` 或 `knowledge.search.judgment.withdrawn.v1`。payload 固定 `judgment_id/judgment_revision/judgment_revision_id/base_revision_id`、`search_id`、查询原文/hash/时点、发布 release/build/index/chunk manifest ref/hash、内容/片段身份与原文/hash、等级/rubric/actor/判定时点/状态/理由。消费方先按 `event_id` 从私有接口回查原始事件/hash，并可用现有 `POST /internal/v1/knowledge/search-sources/read` 按固定发布身份独立核对片段。历史 event 回查在判定或内容撤回后仍保留；source read 对已撤回内容会拒绝新读取，不能用当前正文补造历史。

可确定映射：`query_id=search_id`，`document_id=content_id`，`document_revision=content_revision_id`，`relevance_grade=grade`，`judgment_revision=payload.judgment_revision`，`judgment_source=human_judgment`，`judgment_source_ref=RTW event_id`，`judgment_source_hash=event_sha256`。BTW 数仓消费时才赋予 `available_at`（入仓可用时点）、`source_partition/source_sequence`（完整 producer 流 offset）、`batch_id`、`query_family_id/near_duplicate_cluster_id`（显式版本化分组）与数据集切分；不得由 RTW 猜造。撤回事件生成新的可见性修订，不改旧 Parquet 或旧 manifest。

一条人工判定只证明该 `query × chunk` 已判断；从已有搜索结果或引用录入的等级也不证明当时全候选或 TopK 均被判断。此接口不发候选池覆盖收据。没有独立冻结的判定范围与覆盖证明，正式 Recall/MRR/nDCG 仍为 `not_evaluable`。

2026-09-15 的验收使用隔离 PostgreSQL 16、合成 HTTP/对象存储夹具与真实 go-zero 子进程；`go test -race -count=1 ./service/knowledge/api/...`、`go vet ./service/knowledge/api/...`、goctl 1.9.2 重复生成一致及生成 TypeScript 严格类型检查通过。测试覆盖管理员/Worker 路由、等级 0、并发重放、旧基线、撤回、历史事件回查、Outbox 失败回滚及迁移脚本重入。这是来源契约和本地 L2 验收，尚无真实管理员录入、数仓观察数据、人工 qrels 覆盖收据或线上启用。
