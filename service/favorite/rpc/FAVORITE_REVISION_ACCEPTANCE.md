# 收藏文章发布修订冻结

Favorite 创建文章收藏时要求 Article RPC `PublicOnly=true`，核验文章 ID、公开状态与 `published_revision_id=<ArticleID>:rN`。收藏行在原事务内保存公开投影的标题、封面及 `target_revision`；`favorite.<FavoriteId>.v1` assert 在同事务内写入相同修订。请求方填写的标题、封面不会覆盖公开快照。

在启用新 Favorite RPC 前，先于原业务库执行 `internal/model/003_favorite_target_revision.sql`。它只给 `favorite_item` 增加可空 `varchar(96)` 列，不回填旧行。旧收藏、无发布指针的 legacy PUBLISHED 文章以及非文章目标都保持 `NULL`；仅从标题、状态或当前文章指针推断历史修订会制造错误事实，因此禁止。脚本可重复执行；既有 `favorite_fact_outbox` 的 payload、事件身份和 DC 回执不被改写。

取消单个收藏或删除文件夹时，Favorite 从原业务行读取已冻结修订，沿同一 `FavoriteId` 写 `v2` retract，不重查 Article 当前指针。Article 后续发布 r2 不会把旧收藏的 r1 变成 r2；再次收藏被唯一键拒绝，也不会更新原 assert。权威读口在存活收藏的业务行与已接纳 assert 之间核对修订，并要求 retract 与其前驱 assert 的 `target_revision` 相同。旧 `NULL` 事件仍按其原哈希读取。

本切片不改变 FavoriteId/gRPC 形状、DC EventSpec 外层或 Outbox 技术接纳状态。已推 BTW 生产 `FavoriteAuthorityBinder` 对非空修订生成 `ValueRef=article/<target_id>/revision/<revision_id>`，对 `NULL` 仍生成 `article/<target_id>`；因此此 RTW 改动会影响 BTW 用户模型的对象引用，需用其真实 Binder/Graph 和双收据单独联验。实际 DC 技术接纳、RTW 权威读与 BTW 事实接纳属于三个不同验收门禁。

2026-09-15 验收：

- `SEA_DC_PLATFORM_ROOT=<隔离DC> bash service/favorite/rpc/acceptance-dc.sh` 在隔离 PostgreSQL 16 上以 `-race -count=1`、vet 退出 0；实际 DC `cmd/platform` 接纳高位 Snowflake ID，RTW 私有权威读与原 EventSpec/hash/回执一致。新增 `TestFavoriteArticleLiveChain` 启动 Article 独立测试进程，以真实网络 Article RPC→Favorite gRPC→PG 验证 r1 收藏、草稿/撤回拒绝、发布 r2 后同文件夹重复收藏不改 r1、旧 FavoriteId 的撤回仍为 r1，而新文件夹收藏取得 r2。证据目录：`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-favorite-dc.OT1SDl`。
- 当前 BTW 已推集成 `9f57ed1` 的 `internal/app/test-fact-worker-authority.sh` 指向本 RTW 树，真实 RTW 双事件→DC→BTW 生产 Binder/Graph 用例退出 0；共享事件的修订固定为 `article-shared-authority:r1`。证据目录：`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-fact-authority.38F14O`。该 BTW 测试验证双收据/状态与重放，但当前尚未对 `ValueRef` 文本作断言，且共享夹具没有在同一次运行中启动 Article 源指针 r2；两项留待正式跨仓全链测试，不能据此声称同次端到端验收。
