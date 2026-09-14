# 文章公开读取与作者草稿交接验收

日期：2026-09-14。状态：**RTW 文章公开读与作者读的隔离 PostgreSQL/HTTP 门禁 LOCAL_VERIFIED；WS02-B/WS03-D 全链路 PARTIAL**。

权威源为 `api/article.api`、`proto/article.proto` 与 Article PG 的 `article_revision`、`article_publication`。公开 `GET /v1/articles` 在数据库 `COUNT` 和分页之前，仅选 `article_publication.state=published` 且指向真实不可变修订的文章，或没有发布指针且源行仍为 `PUBLISHED(2)` 的旧文章。公开 `GET /v1/article/:id` 只返回同一资格下的详情；新文章投影冻结修订的标题、摘要、封面、标签、作者和 Markdown，外部状态固定为 `PUBLISHED(2)`。因此源行正审核 r2 时，仍可读旧已审 r1；r2 被指针接受后切换到 r2；撤回或软删后不可公开。列表不输出可变对象键或正文。公开详情在资格成立、正文可得之后才条件性增加浏览量，撤回赢得竞争时不增量。

没有发布指针的旧 `PUBLISHED` ArticleId 保留原链接和 MinIO 对象读取，`ext_info.publication_gap=legacy_revision_missing`，不伪造修订号；有真实指针时返回 `ext_info.published_revision_id`。该旧兼容路径只能保证源行当前为 2，**不能保证旧对象不可变**，正式回填需审阅原对象与签收迁移。指针存在但撤回或悬空时不回退旧行。作者 `GET /v1/me/article/:id` 位于 JWT 路由组，HTTP 从 token 提取 `userId`，RPC 在取 MinIO 正文前核对 `AuthorID`，返回当前源草稿；HTTP 在返回前还二次核作者，更新/删除的预读也传同一作者 ID。未认证和异人不能读草稿。

混部顺序必须为**先更新 Article RPC，再更新 Article/Hot API**。旧 RPC 忽略新 `public_only/requester_id` 字段；新公开 HTTP 对非状态 2 的旧回执拒绝序列化，作者 HTTP 二次核人，避免返回草稿正文。但旧 RPC 列表的 total 可能仍含草稿，即使当前页全为已发布，所以它不满足分页契约；没有版本握手前不得倒序发布或把混部期计为完整验收。

生成路径：`goctl 1.9.2 api go -api api/article.api -dir service/article/api -style go_zero` 生成路由、类型及新 handler/logic scaffold；`protoc 29.2` 配合 `protoc-gen-go v1.36.6`、`protoc-gen-go-grpc v1.5.1` 从 `proto/article.proto` 生成 RPC PB。对改动前 proto 的基线重生，两个 PB 仅 protoc 版本头不同；服务方法未变，提交仅更新 `article.pb.go`。原 routes 文件的人工作业 `70s` 超时与上传 `10 MiB` 限额须在 goctl 重生后保留，本增量 diff 只新增 JWT 作者路由。`AuthorName` 原本只在生成类型中，现补到 `.api` 权威源，避免再次生成丢失。

验收：`GOFLAGS=-p=2 GOMAXPROCS=2 bash service/article/rpc/acceptance.sh` 自启停隔离 PG16，执行 Article RPC 全包 race、vet 和 diff check。测试覆盖草稿/待审无指针、撤回指针、编辑期间旧 r1 公开、r2 切换、再次撤回、旧无指针链接、已审元数据投影、批准标签过滤、两页总数与排序、异人 RPC 拒收及作者读当前源。真实 PG + 空 MinIO client 的断言证明新修订公开读取不依赖对象存储；旧链接和作者草稿使用本地假 S3，未联真实 MinIO。`go test -mod=readonly -race -count=1 ./service/article/api/... ./service/hot/api/...` 验证 HTTP handler、go-zero 同款 JWT middleware、旧 RPC 回执拒漏和 Hot 公开消费；`go vet ./service/article/api/... ./service/article/rpc/... ./service/hot/api/...` 通过。HTTP 与 PG 当前分别在本地门禁验证，**尚未搭建同一真实 HTTP→gRPC→PG→MinIO 进程链**。

公开消费者检查：Article API 公开详情/列表和 Hot 热榜使用 `public_only`；Hot 对外保留编辑期间旧 r1 的状态 2/已审标题。Article 作者 GET 与更新/删除预读使用 `requester_id`。Favorite 的 `resolveArticleSnapshot` 目前使用内部读取提取标题/封面，可能让用户收藏他人的草稿并持有其元数据，需其领域另行限定；Comment fallback 只读取作者 ID、没有公开正文，但草稿是否可成为评论目标仍待该域规则。此分支按并发边界未修改 Favorite/Comment/Like。

Web 交接：`src/app/post/edit/[id]/page.tsx` 目前通过 `getArticle(articleId,{token,incr_view:false})` 读公开 URL；应新增作者读取 service 函数指向 `GET /v1/me/article/:id`，仅编辑页切换，公开详情继续走原 URL。未完成 Web 切换前，未发布文章编辑页会读不到当前草稿。生产发布、旧数据修订回填、真实 MinIO/HTTP 全链路与端到端 Web 验收未执行。
