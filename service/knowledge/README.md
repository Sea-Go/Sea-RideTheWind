# 知识资料与发布服务

背景：BG-2026-09-13-r2；任务 WS02-C；交接 H02/H03/H06。API-only 用例沿用 go-zero handler/logic/svc/model；当前没有同仓 RPC 消费者，不创建空 RPC 服务。

## 工作区域与实现边界

- W0：独立 `sea-rtw-knowledge-20260914` 工作树，基线 `08b385e676c0c5861771125e6462b9f585bd7450`。
- W1：`service/knowledge/`、权威 `api/knowledge.api` 及本接口生成物；必要依赖装配。
- R1：Sea-Docs、既有 article 服务、已交付网页及其他仓。
- D1：go-zero v1.10.2、pgx v5.10.0、minio-go v7.2.0 模块缓存，仅核对公开 API。
- G1：goctl v1.9.2 生成的 routes/types；由 API DSL 生成，不手改。
- X1：仅任务专用 PostgreSQL 实例与本地对象目录，允许隔离验收；没有生产写入。
- N1：原始仓未提交改动、既有社区实现与其他任务目录。
- T1：本任务 mktemp 目录保存生成器、数据库和运行日志，不提交数据库文件。
- 主职责 C3/C4：不可变修订、固定清单、接纳与发布 CAS；跨 C1/C2/C7/C8：生成 API、入口、事件与真实存储验收。

RTW 不执行 Agent、编码或索引算法；这些归属 BTW tRPC-Agent-Go。数据库与发布状态由本服务管理，禁止 worker 直接写库。

## H04 事件交接 r1（尚待 DC 接纳）

Outbox 表 `knowledge_outbox` 与知识修订/候选/发布同事务写入。事件 JSON 包含 `event_id,event_type,schema_version=1,producer=ridethewind.knowledge,aggregate_id,occurred_at,payload`；具体 payload 由知识域维护，不使用旧桌面事件接口。

`mqs.HTTPSender` 向配置的通用事件端点 POST 事件，`Idempotency-Key` 为 event_id。期望 HTTP 2xx 与 JSON `{ "event_id": "同一事件ID", "technical_status": "accepted" }`；只有匹配回执才设置 delivered_at。接收者须耐久接纳并按 event_id 去重后返回。重复传输不表示重复领域操作，technical_status 不表示领域接纳或知识发布。

`DispatchOne` 采用 PostgreSQL `FOR UPDATE SKIP LOCKED`，同事务标记确认；网络超时/失败/错误回执回滚，后续保持同 event_id 重投。工作进程每批最多 16 条，每次 10 秒期限。未配置通用 H04 接收者时关闭投递器，Outbox 保留待交付事实。

H01/H03 关联补充：每条事件携带非空 `operation_id` 和 `aggregate_version`。全部知识事件的 aggregate_id 为 module_id；aggregate_version 来自同事务递增的 `knowledge_modules.event_sequence`，每条事件严格递增，即使同一次业务操作产生多个事件也各占一版。它与模块手动发布 `pointer_revision` 分开，不能互换。修订及冻结事实是追加事件，不应因后到达的更高聚合版而丢弃仍有用的历史修订；发布投影则按 payload.pointer_revision 防止指针倒退。

业务命令 operation_id 为 `command:` 加 scope/idempotency_key 的 SHA256，构建或编制接纳为 `result:<build_id|compile_id>`；重试回放保持同一业务操作和事件身份。固定 H04 生产 fixture 为 `testdata/h04-revision-created-v1.json`，其原文 `testdata/book-a.md` 可校验 payload.content_hash；最小技术回执为 `testdata/h04-technical-receipt-v1.json`。支持 DC 追加 receipt_id/input_hash/received_at 字段，Sender 仍核对事件身份与 accepted。

## 接口与接纳规则

权威 `api/knowledge.api` 生成 Go routes/types、`generated/knowledge.json` 和 `generated/typescript/`。生成命令：

```sh
goctl api go -api api/knowledge.api -dir service/knowledge/api -style go_zero
goctl api swagger --api api/knowledge.api --dir service/knowledge/generated --filename knowledge
goctl api ts --api api/knowledge.api --dir service/knowledge/generated/typescript
```

统一入口为 `service/knowledge/scripts/generate.sh`（可通过 KNOWLEDGE_GOCTL 指定生成器路径），包含生成器版本检查、生成文本空白规范化及去除 Swagger 生成时钟元数据。本次生成器 goctl 1.9.2，运行 go-zero v1.10.2。成功 `code=200,msg,data` 已在 DSL 定义 envelope；错误使用真实 HTTP 400/401/403/404/409/410/499/503/504/500，业务未知错误不作为空成功。

- 公开 GET `/v1/knowledge/modules`、`/modules/{id}`、`/modules/{id}/published` 仅展示有效已发布内容；候选/管理员列表为 `/workbench/modules`。
- 管理命令复用已验证 JWT 的 `userId`，并由部署配置 AdministratorIDs 指定首批管理员。统一身份中心的后续业务角色映射仍属 WS02-A；客户端自报身份不作为依据。
- `/modules/{id}/sources` 直接接收 Markdown/UTF-8 纯文本，不提供扫描 PDF、OCR、EPUB。Wiki 使用 `/wiki-pages/{page_id}/revisions`，要求 base_revision_id 与当前页头一致；`paragraph:N` 绑定具体修订，按非空空行分块编号并验证范围。
- `/modules/{id}/compiles` 固定 source revisions、页面 base 和 guidance。BTW 工件只经 `/internal/v1/knowledge/compiles/{id}/results` 接纳，不覆盖人工编辑。
- `/releases/{id}/index-builds` 创建独立 generation。Worker 先 `/internal/v1/knowledge/builds/{id}/claim`，绑定 attempt_id、lease_epoch、未来 RFC3339 `lease_expires_at`、cancel_version 与输入 manifest_hash。续租保持同 attempt/epoch，替代 attempt 必须更高 epoch。过期、旧代、旧 attempt、取消回执不能提交。
- H06 `IndexManifest` 当前由 `api/internal/model/builds.go` 的接纳结构定义 r1，读取不可变对象并校验其 SHA256、固定构建身份、ChunkManifest，以及 dense/sparse/multivector 的 profile、空间、维度、数量、分片和探针引用。`testenv.Index` 明确构建的是结构 fixture；fixture 数值与探针声明不证明真实编码或索引效果。后续 WS06 冻结生产者 schema 后进行双向契约接纳。
- READY 不自动发布。`PUT /modules/{id}/activation` 对 release/build/expected_pointer_revision/reason 验证后，在同事务更新指针、publication 审计和 Outbox。回滚使用同一接口、保持 pointer_revision 递增。现网页没有 idempotency_key 时，以 actor/module/expected_pointer_revision 作为重试身份；同键不同输入返回 409。
- 已取消/被替代构建不影响旧 READY build；已撤回来源不能通过回滚重新启用。公开读取固定同一 PostgreSQL 快照，正文对象和修订仍按 hash 核对。

## 启动与存储

示例配置在 `api/etc/knowledge-api.yaml`。通过环境提供 KNOWLEDGE_AUTH_SECRET、KNOWLEDGE_ADMIN_ID、KNOWLEDGE_WORKER_TOKEN、KNOWLEDGE_POSTGRES_DSN、KNOWLEDGE_OBJECT_DIRECTORY。在**空的隔离数据库**先执行 `api/internal/model/schema.sql`，或把本地测试配置 Postgres.Migrate 显式设为 true；默认不自动迁移。然后在仓根执行：

```sh
go run ./service/knowledge/api -f service/knowledge/api/etc/knowledge-api.yaml
```

PostgreSQL 保存修订、清单、状态、操作回放和 Outbox。数据库 trigger 拒绝修订/清单正文变更，正文对象采用 SHA256 内容寻址；原文不会被新 Wiki 版本覆盖。对象写入先于元数据事务，事务失败可能留下未引用的内容寻址对象，当前不自动清理。

Objects.Backend=local 是开发适配器，pro 模式拒绝使用；S3 配置 Endpoint/Bucket/AccessKey/SecretKey/Secure 使用既有 minio-go 客户端、启动探测已存在的 bucket。S3 适配代码已编译，但本轮尚未完成真实 S3 服务验收。没有生产数据迁移或部署。

Delivery.Enabled 默认 false。DC 接入后配置独立 `/v1/events` 接收地址，不使用旧桌面 events/batch 路径。Outbox、索引 worker 与对象基础设施故障不能自动替换当前正式版本。

## 本地验收结果（2026-09-14）

- L1：UTF-8/格式/来源/定位、内容寻址并发写入与损坏拒收、lease期限；Go 编译、静态检查、生成 TypeScript 严格类型检查。
- L2：隔离 PostgreSQL 的书 A 原文 + Wiki v1/v2 + 加书 B v3 + 回滚；不可变 DB 约束；10 个并发发布恰好 1 成功、9 冲突；Outbox 写入失败时指针及审计全部回滚；重复请求保持同响应。
- L2：9 类无效 READY fixture（旧 attempt、旧 generation、取消版本、输入hash、对象hash、缺lane、错space、数量不一致、probe未通过）均拒收；额外覆盖租约过期、取消、替代 generation，旧已发布版本保持有效。
- L2：编制结果 hash/资料引用/人工 base 冲突检查；成功候选幂等、人工修改不被覆盖；FAILED、取消、旧 attempt、过期和替代编制结果均有明确状态与拒收路径。
- L2：实际 go-zero HTTP 子进程经过生成路由/JWT/handler/logic/svc/PostgreSQL/本地对象，完成资料、Wiki、候选、claim、READY、手动发布、回放和 409 冲突；公开列表在发布后才出现，原文按历史修订回读。
- L2：独立真实 HTTP 接收者先耐久接收但丢回执，Outbox 重投同 event_id；双 dispatcher 不重复领域效果，ack 后仅一次确认。
- L3 尚未验收：真实 BTW 编制模型、Dense/Sparse/Multi-vector 索引构建与独立查询、DC 正式双服务通道、生产 S3，以及浏览器/桌宠实际交互。结构 fixture 的 READY 不能当成三路算法效果通过。

复现：安装匹配仓库 Go toolchain 与本机 PostgreSQL 后，在仓根执行 `KNOWLEDGE_PG_BIN=/path/to/postgresql/bin service/knowledge/scripts/acceptance.sh`。脚本创建随机端口/专用目录的本地 PostgreSQL，逐测试隔离 schema，结束停止实例并清理；设置 `KNOWLEDGE_KEEP_EVIDENCE=1` 可保留日志目录。直接执行 `go test ./service/knowledge/...` 只运行无外部依赖测试，PG/HTTP测试会明确 skip，不能据此声称 L2 通过。

无提交到主分支、无部署。当前交付用于下一步 WS03 网页真实接线、WS05 通用事件接纳以及 WS06 三路结果联调。

### 提交前独立复验补充

独立复验复现了工件读取耗时超过租约后仍提交 READY/ACCEPTED 的缺陷。结果写入现在通过 PostgreSQL `clock_timestamp()` 在最终 UPDATE 再次核对租约；拒收会回滚同事务产生的 Wiki 修订、页头与 Outbox。新增构建和编制两个慢读取反例，覆盖状态与副作用均不提交。2026-09-14 使用隔离 PostgreSQL 16 执行完整 `scripts/acceptance.sh` 通过（含 race、HTTP 与静态检查）。

该基线尚缺管理员与公开历史读面；以下 H02 补充实现补齐接口，最终网页/桌宠集成仍须各消费者独立验收。


### H02 产品读取实现工作区（2026-09-14）

继续使用以上 W0/W1/R1/D1/G1/X1/N1/T1 声明，读面基线为 `627c1c9`。本次主职责 C4 为模块范围查询、稳定分页与历史发布资格；C1/C2/C7 为 go-zero 产品路由、logic 与生成契约，C8 为隔离 PG/HTTP 验收。仅 `api/knowledge.api` 与 `service/knowledge/` 可写；不修改原始工作树、其他服务和共享 Docs。管理员产品接口不转发内部 worker 路由；权限仍用已有 JWT/Administrator 装配，不建立新身份系统。


### H02 产品读取契约 r2

全部路径前缀是 `/v1/knowledge`，成功响应沿用 `{code:200,msg:"success",data:...}`。权限复用现有中间件，不向浏览器传递 worker token。

| 读取面 | GET 路径 | data 类型与语义 |
| --- | --- | --- |
| 管理员 | `/workbench/modules/:module_id` | Module；包括未发布和已撤回模块的管理状态 |
| 管理员 | `/modules/:module_id/revisions` | `{items: Revision[],next_cursor?}`，只列元数据、当前 withdrawn |
| 管理员 | `/modules/:module_id/revisions/:revision_id` | Revision，包括经 hash 验证的 content；已撤回内容不返回正文 |
| 管理员 | `/modules/:module_id/releases` | `{items: Release[],next_cursor?}` |
| 管理员 | `/modules/:module_id/releases/:release_id` | 固定 Release 元数据；不把候选当已发布 |
| 管理员 | `/modules/:module_id/builds`、`/modules/:module_id/builds/:build_id` | Build 列表或详情，包括旧 READY、取消、替代与失败状态 |
| 管理员 | `/modules/:module_id/compiles`、`/modules/:module_id/compiles/:compile_id` | Compile 列表或详情，供刷新恢复及状态轮询 |
| 公开 | `/modules/:module_id/published-releases/:release_id` | 曾实际发布且当前有效的固定 Release |
| 公开 | `/modules/:module_id/releases/:release_id/revisions` | 该固定已发布 Release 的 Revision 元数据分页 |
| 公开 | `/modules/:module_id/releases/:release_id/revisions/:revision_id` | 该 Release 成员的固定正文、hash、来源定位 |

所有列表采用 `limit`（默认 20，允许 1–100）和不透明 `cursor`，`next_cursor` 缺省表示结束。管理员原有 revisions 列表由无界返回改为有界列表；调用者必须跟随 next_cursor。每次不带 cursor 的查询开始新一轮，按持久化 `list_order` 倒序；cursor 固定首轮成员上界及模块/资源种类/公开 release 范围，后续新增记录不挤入旧轮次，任务状态仍读取当前值。游标不得换模块、资源种类或 release 使用；不使用 offset，不把 UUID 字典序当创建顺序。

`schema.sql` 为 revisions/releases/builds/compiles 增加 identity 列及 `(module_id,list_order DESC)` 索引，并为 publication 增加模块/版本索引。创建路径先获得模块事务锁，再插入记录并分配 identity，因此同模块晚提交创建无法落到已经可见的分页上界内。迁移前记录在一次 schema 升级时取得固定序号，后续记录延续该序号；旧数据保证稳定遍历，不把迁移分配序号当作原始业务时间。旧二进制忽略新增列，修订/清单不可变约束继续保护 identity。迁移执行由部署流程明确控制，服务默认不自动迁移。

公开资格依据 `knowledge_publications` 的真实发布审计，READY 和冻结清单不构成公开资格。活动指针切换或回滚后，旧发布仍定位原 release/revision；请求的 module/release/revision 必须一致且 revision 属于清单。缺失、未发布或非成员返回 404；模块撤回，或清单任一修订撤回，使该发布读取返回 410。管理员仍可读取审计元数据解释失效原因，但不读已撤回正文。正文/清单对象读取或 hash 校验失败返回 503，取消与超时保留原有分类，不把暂时存储故障当撤回。

新读面先验证当前数据库资格，再在无数据库事务/锁的情况下读取对象，最后复核当前资格。修订正文始终按指定不可变 hash 验证；读取 Wiki 期间其依赖来源被撤回，也不能返回成功。目录分页只传修订元数据，不为列表读取所有正文；正文可用性在打开该修订时验证。`paragraph:N` 仍是 CRLF 转 LF 后以字面 `\n\n` 分块、忽略空块并从 1 编号，不跟随最新修订重新定位。

### H02 读面验收补充（2026-09-14）

- L1/L2：四类列表范围、limit、坏 cursor、跨模块/种类/公开 release 游标拒收；首轮之后插入记录，遍历旧轮仍不重复、不遗漏、不混入新成员；刷新可见新成员。已有数据迁移并重复执行 schema 后，固定正文/hash 不变，identity 与修订/清单仍受不可变约束保护。
- L2：历史 Wiki v1 发布后切换 v2，旧引用仍返回 v1 的原始正文/hash；READY 尚未发布、非清单成员、跨模块均拒收；撤回历史成员只使包含它的发布失效，当前有效版本仍可读；模块撤回后公开读取均拒收，管理状态仍可查。
- L2：在正文对象 Get 内触发修订/依赖来源/模块撤回，最终读取拒收；对象返回错误 hash 的字节也不能成功。新读面没有对象 I/O 跨数据库锁。
- L2：实际 go-zero HTTP 子进程经生成路由/既有 JWT/Administrator、logic、PostgreSQL 16 和本地对象完成全部新路径。验证管理员草稿、四类历史列表与详情、取消后轮询、公开分页/历史正文、404/410/503、缺身份 401，以及不合法数值 limit 的 400；故意损坏真实本地正文文件产生 503，恢复原字节后再验撤回。
- 生成一致性：固定 goctl 1.9.2 再次运行 `scripts/generate.sh`，DSL、Go types/routes、Swagger 和 TypeScript 的 SHA256 均不改变；消费者同步 `generated/typescript/knowledgeComponents.ts` 与 Swagger。
- 尚未由本提供方证明：网页/桌宠真实用户旅程、BTW 实际三路算法与生产对象存储。上述历史发布测试仍使用显式结构 READY fixture，不能据此宣称三路检索或全工程验收完成。

可复现命令仍为 `KNOWLEDGE_PG_BIN=/path/to/postgresql@16/bin KNOWLEDGE_KEEP_EVIDENCE=1 bash service/knowledge/scripts/acceptance.sh`。该脚本运行 knowledge 全部包的 race 测试、真实隔离数据库/HTTP 与 go vet，结束仅停止自建的随机端口实例。
