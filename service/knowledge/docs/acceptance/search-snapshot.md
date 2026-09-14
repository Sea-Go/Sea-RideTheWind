# H03/H07 当前搜索快照：RTW 提供者局部验收

日期：2026-09-14。状态：**RTW LOCAL_VERIFIED；H03/H07 跨工程 PARTIAL**。此接口是 RideTheWind 人工发布状态的只读投影，不是 BreakTheWaves 完整搜索链或三路真实模型效果验收。

工作区域：[W0:ROOT] 独立 RTW 工作树；[W1:WRITE] `api/knowledge.api`、`service/knowledge/` 内本接口、生成 DTO、窄测试与本文；[R1:READ_ONLY] Sea-Docs H03/H07 与 BTW `internal/search.Snapshot`；[D1:DEPENDENCY] 仓内 go-zero、pgx 和 goctl 1.9.2；[G1:GENERATED] routes/types/OpenAPI/TypeScript 仅由 `service/knowledge/scripts/generate.sh` 重生；[X1:EXTERNAL] 验收脚本自启停的隔离 PostgreSQL 16、本地对象及真实 HTTP 子进程；[N1:OUT_OF_SCOPE] usercenter、RTW 回答历史、BTW 消费者、用户原业务工作树和生产；[T1:TEMP] 临时 goctl 与测试库。主职责 [C4:PERSISTENCE]，跨 [C1:TRANSPORT]、[C7:CONTRACT]、[C8:VERIFY]。

`GET /internal/v1/knowledge/modules/:module_id/search-snapshot` 使用现有 Worker middleware。请求仅允许 `module_id`；服务端从当前人工发布指针与 `knowledge_publications` 审计行确定 `release_id`、READY build、`generation` 和正整数十进制 `publication_revision`。`indexes` 中 dense/sparse/multivector 三路各自返回不可变 `{key,sha256}`，`valid_revision_ids` 是该发布的全部有效 source/wiki 修订。JSON 字段与 BTW `internal/search.Snapshot` 一致；候选 READY build、客户端自报发布版本或索引均不能生成当前快照。

读取时校验发布清单精确对象 hash、修订对象和撤回状态、IndexManifest 的固定 build/release/generation、三个 profile 的 encoder/tokenizer/space/dimensions/mask/aggregation、chunk 数量、分片、probe 和三个真实可读的 lane 引用。对象 I/O 后重新读取当前指针和修订有效性；期间发生新发布、回滚或撤回时拒绝旧读取。管理员回滚到旧 release 时，旧索引可在**新的** `publication_revision` 下成为当前快照；已存引用的历史固定版本读取仍走现有 `ReadSearchSource`，不追新指针。

隔离 PG16 模型用例通过：未发布 READY 拒绝、后续 READY 候选不影响当前、发布/回滚同索引新指针、缺 lane/错 space、原对象损坏、对象 I/O 中指针变化和撤回。真实 go-zero HTTP 子进程通过生成 OpenAPI 响应校验、Worker 未授权拒绝、未发布拒绝、发布后三路精确引用、切换新 release、只撤回旧历史修订不影响新当前、模块撤回拒绝；新增阶段沿用同一 go-zero Writer/OTel/Prometheus Runtime，日志不记录正文或对象字节。

复验：`KNOWLEDGE_GOCTL=<临时目录>/goctl service/knowledge/scripts/generate.sh`；`bash service/knowledge/scripts/acceptance.sh`（隔离 PostgreSQL 16、`go test -race ./service/knowledge/... -count=1 -v`、`go vet ./service/knowledge/...`、`git diff --check`）。RTW 的测试索引工件是结构性 fixture，**不证明** BTW 真实同代 Dense/Sparse/Multi-vector 构建或检索数值。此固定提交时 BTW 客户端尚未调用本接口；后续两仓同进程联验已接通，见下段。Collector→DataCenter 下钻和完整 H07 产品验收仍未完成。

后续BTW从本仓`7519ecc`生成 `SearchSnapshot` DTO，`RTWSearchSnapshotProvider`向真实RTW进程只传module_id，拿当前发布快照再沿**这份返回值**完成原文→引用→答案产品turn。第一次失败揭示原手工跨仓fixture少列一个已发布wiki修订，而RTW当前快照正确包含source+wiki；修正测试预期为完整有效集合后，`SEA_BTW_CITATION_CONSUMER_ROOT=<BTW集成树> bash service/knowledge/scripts/acceptance.sh`的隔离PG16/真实HTTP/race/vet全部通过。BTW源码范围见`internal/app/RTW_SEARCH_SNAPSHOT_ACCEPTANCE.md`。这只把当前快照**子链**上推到`INTEGRATED`，不改变H03/H07整体`PARTIAL`。

进一步复验把同一快照交给BTW typed `search_fast/read_evidence`及真实tRPC-Agent-Go Runner/LLMAgent的`search_fast` Tool调用。初次请求因BTW `search_id=operation:1`不符合本仓持久引用ID语法而400；BTW改为`search_`加固定operation/序号SHA256后重新跑全服务PG16/race/vet通过。当前本仓引用提交指标在跨仓模式下精确为4：本仓原fixture、BTW普通Delivery、BTW直接Tool、BTW Agent Tool，均为不同search_id；重复提交不增指标。本地Tool-call模型fixture不代表实际DC生产模型或H07公开入口。
