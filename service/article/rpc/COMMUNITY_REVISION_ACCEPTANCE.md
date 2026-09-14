# WS02-B 社区文章修订与 H03 Outbox 验收

日期：2026-09-14。状态：**RTW Article PostgreSQL 业务提交子链 LOCAL_VERIFIED；H03/H09.a/WS02-B 整体 PARTIAL**。此分支保留原文章 RPC/ArticleId 和推荐 `article_sync_outbox`；新增的 `article_revision`、`article_publication` 与 `article_domain_outbox` 才是社区修订/可读状态的领域权威，不借知识模块的手动发布指针表达社区发布。

工作边界：[W0] 独立 RTW `sea-rtw-community-facts-20260914`，起点为已推 `1a8aa0b`；[W1] `service/article/rpc/internal/{model,mqs,logic}` 及本域迁移、测试/说明；[R1] DC `contracts/eventing`、BTW内容消费者、WS07数仓和Docs只读；[D1] 锁定GORM/pgx/go-zero版本只读；[G1] 现有生成路由/protobuf不手改；[X1] 只用脚本随机端口隔离PG16，Message/审核输入为测试固定桩，无共享服务写入；[N1] 原始脏树、root集成树及其他业务域；[T1] 脚本保留PG证据目录。主职责[C4:Persistence]文章修订/指针/outbox原子提交，跨[C3]审核版本与撤回不变量、[C7]H03事件、[C8]真实PG/race验收。

## 固定语义

- 作者创建仍用旧 ArticleId 和旧路由，处于 REVIEWING；审核消息加 `review_nonce`，正文编辑使用 `<ArticleId>-<SHA256>.md` 新对象 key，旧对象不覆盖。审核消费在读对象前和写推荐同步 outbox 的文章行锁内同时核对 ArticleId、作者、路径与 nonce。即使只改标题、正文路径不变，旧审核消息也不能批准新元数据。旧无 nonce 的待审记录仅在文章行也无 nonce 时兼容。
- `ArticleSyncResultConsumer` 仅在结果的 `event_id + version_ms` 与当前文章待审同步绑定且结果成功时，在**一个文章 PG 事务**中把保存于原推荐同步 outbox 的已审 Markdown 冻结为连续 `article_revision`，SHA-256 定位正文，CAS 更新 `article_publication`，写 `community.article.published` 域 outbox 并改文章状态 PUBLISHED。该 H03 事件含旧 ArticleId、新 revision ID、完整作者 SubjectRef、正文/hash/源对象、`search_evidence=true`、`wiki_module_id=null`；它不把社区文章自动塞入 Wiki。
- 文章由 PUBLISHED 改为非发布状态或经原 DeleteArticle RPC 软删时，修订指针和 `community.article.retracted` 域 outbox 与文章行/旧推荐同步 delete outbox 在同一事务推进。旧成功回执无法恢复已撤回文章；重复回执不新增修订或通知。逻辑删除保留旧正文对象与不可变修订，历史引用可按 revision ID 查；物理对象 GC 必须另定保留/消费 ACK 政策。
- `002_community_revision.sql` 必须先于新版 Article RPC 启动显式执行；启动时检查 `article_revision_immutable` 触发器，缺迁移即拒绝启动。旧 PUBLISHED 文章若尚无领域修订，首次撤回仍走旧文章业务流程，但 `ext_info.h03_publication_gap=legacy_revision_missing` 明示回填缺口，**不伪造修订 ID 或已签收 H03 事件**。旧文章正式回填需要源对象审阅与迁移计划。

## 已执行验证

`bash service/article/rpc/acceptance.sh` 在独立PG16.14和 `go test -mod=readonly -race -count=1 -v ./service/article/rpc/...`、`go vet` 下退出 0；最终日志为 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-article-revision.MP6SpA/go-test.log`。测试经实际 sync-result consumer 证明首次/第二次成功分别形成r1/r2与指针版本1/2，老正文/hash仍不变；旧消息同路径但旧 nonce、旧结果、重复结果、跨作者删除拒绝。经真实 UpdateArticleLogic/ DeleteArticleLogic 验证状态撤回与软删除，指针版本继续递增，旧修订仍可读取；PG触发器拒改不可变正文，注入 H03 outbox 写失败时修订/指针/文章状态一起回滚。内容 hash 对象 key 单测保持原 ArticleId 与旧链接定位不变。

这是**RTW领域本地提交**：审核RPC/MinIO/Kafka/推荐对端没有在这一命令中真实联调，`article_domain_outbox` 还没有通过 DC EventSpec/技术receipt送达 BTW 或 WS07；可消费载荷和本地 pending 查询不等于 DC ACK 或社区内容已入搜索。Event `occurred_at`/业务时间在事务前取值，不能冒充 PostgreSQL commit timestamp。Web/桌宠、公开修订读取接口及旧PUBLISHED文章回填仍待交接。评论仍不因文章的 `search_evidence=true` 自动成为首期搜索证据。
