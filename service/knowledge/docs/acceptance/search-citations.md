# H07 固定原文与引用接纳：RTW 局部验收

日期：2026-09-14。状态：**RTW LOCAL_VERIFIED；H07 跨工程 PARTIAL，未接纳**。这是 RideTheWind 知识服务的权威片段读取和引用映射提交，不代表 BTW 搜索入口、回答历史或客户端已接通。

工作区域：[W0:ROOT] 本独立 RTW 工作树；[W1:WRITE] `api/knowledge.api`、`service/knowledge/` 内本功能模型、API、生成 SDK、测试与本文；[R1:READ_ONLY] Sea-Docs H07、BTW `internal/search/evidence.go` 与原脏树；[D1:DEPENDENCY] go-zero 1.10.2、pgx 5.10.0、goctl 1.9.2；[G1:GENERATED] routes/types/Swagger/TypeScript，仅由 `service/knowledge/scripts/generate.sh` 重生；[X1:EXTERNAL] 只写脚本创建的隔离 PostgreSQL 16 与合成对象目录；[N1:OUT_OF_SCOPE] usercenter、其他工程、用户工作树与生产；[T1:TEMP] 临时 goctl 二进制、测试集群及测试日志。主职责 [C4:PERSISTENCE]，跨 [C1:TRANSPORT]、[C7:CONTRACT]、[C8:VERIFY]。

## 交接合同

| Worker API | 输入/输出与提交点 |
| --- | --- |
| `POST /internal/v1/knowledge/search-sources/read` | 输入 `module_id, release_id, generation, publication_revision, revision_id, chunk_id`。publication revision 是 RTW 正整数指针版本的十进制字符串；返回与该历史发布的 READY build、H06 chunk manifest、原文对象和原文 byte/rune 定位一致的完整 chunk。索引候选文本不能直接作为原文。 |
| `POST /internal/v1/knowledge/search-citations` | 输入 `search_id, pack_json, pack_hash`；`pack_json` 是 BTW `json.Marshal(EvidencePack)` 的精确字符串，`pack_hash=SHA256(pack_json bytes)`。RTW 验证三路索引同代、有效修订、每条证据 ID、定位、原文 hash 和 quote hash；再锁模块行，复核历史发布/READY/未撤回，在同一事务中写不可变 `knowledge_search_citations`。仅 `Commit` 成功后返回 `search_id,pack_hash,durable_ref`。同 search_id 同 pack 重投同收据，异 pack 为 409。 |
| `GET /internal/v1/knowledge/search-citations/:search_id` | 从已提交的行回查固定快照、收据及每条 evidence 的 source/content/revision/chunk、原对象、定位和 quote hash。撤回后保留 ID 和收据但标 `unavailable`；接口不返回旧 quote 正文。取消或断流后的调用方可先用 search_id 恢复收据，再按自己持有的固定 pack 完成终态判断。 |

三个路由均使用现有 Worker middleware；HTTP 错误经现有统一响应分类，业务阶段使用同一 go-zero `logx` Writer 与 OTel/Prometheus Runtime。日志记录 search/chunk/pack hash 和提交结果，不记录 `pack_json`、quote 或对象正文。RTW 未导入或复制 BTW 的 tRPC-Agent-Go Runner；原生 Agent/Tool Span 仍由 BTW 自身产生，跨服务 W3C Trace 在实际适配器接入后再验。

## 本地证据与限制

- 隔离 PostgreSQL 16 + 真实 HTTP go-zero 子进程验证：未携 Worker token 的读取拒绝；固定源片段返回、错 publication 拒绝、引用提交后 GET 可回查；重投不增加第二次提交；撤回后 GET 标不可用。
- PG 模型反例验证：错 generation/publication/revision/chunk，伪造 quote/locator/index，search_id 异包冲突，两请求并发只有一个提交，原文对象读取期间撤回不能写入引用；收据返回时数据库已有精确 pack hash/bytes。历史撤回后同输入仍可回查收据。独立 race 测试还核对 CRLF 原文字节位置、Unicode 规范化 rune 范围及偏移拒收。
- 测试构建采用合成发布和结构性 H06 三路 probe 工件，**不证明**真实 Dense/Sparse/Multi-vector 同代构建或实际排序；本切片只验 RTW 原文/引用侧。
- 尚无 BTW 正式 HTTP SourceReader/CitationAcceptor 适配器、真实搜索请求/首个公开 SSE 引用门禁、Collector→DC 下钻、客户端恢复、`AcceptedRootHistory.Commit/List`。后者需要以完整 SubjectRef、SessionID、AnswerID、SearchID、收据及校验后公开 turn 另行定义权威持久化和历史投影；不能以本引用表代替回答历史。

复验命令：`service/knowledge/scripts/acceptance.sh`（脚本创建/删除隔离 PG16，执行 `go test -race ./service/knowledge/... -count=1 -v`、`go vet ./service/knowledge/...`、`git diff --check`）；`KNOWLEDGE_GOCTL=<临时目录>/goctl service/knowledge/scripts/generate.sh`（goctl 1.9.2）。正式 H07 真实接纳需 BTW 消费上述生成合同，在同一 Trace 下跑到公开 EOF，再核对 RTW 收据和客户端历史。
