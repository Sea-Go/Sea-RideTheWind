# 收藏文章修订同次跨仓验收

本验收只在 `service/favorite/rpc/acceptance-full.sh` 显式启动时运行，默认测试路径跳过 `TestFavoriteArticleWorkerSharedFixture`。运行前提供隔离 BTW 和 DC 工作树：

```bash
SEA_BTW_WORKER_ROOT=<BTW-210aa73-隔离树> \
SEA_DC_PLATFORM_ROOT=<DC-e8c8cd-隔离树> \
bash service/favorite/rpc/acceptance-full.sh
```

入口沿用 BTW 已推 `cmd/worker/favorite_acceptance.sh`，不改其正式 worker 装配。该脚本自建同一 PostgreSQL 16 实例、真实 DC `cmd/platform` 和 race 构建的 BTW `cmd/worker`。RTW 旧共享测试只在 `FAVORITE_SHARED_FULL_CHAIN=1` 时转入新测试：Article 独立测试进程持有文章 schema 与真实 gRPC 公开投影，Favorite gRPC 用同一 PG 实例的独立业务 schema，通过原 `CreateFavoriteFolder/CreateFavorite/DeleteFavorite` 产生高位 FavoriteId、FavoriteItem 和不可变 v1/v2 Outbox。两个事件由独立 RTW `fact-dispatch -once` 进程分别送往 DC；独立 `fact-authority` HTTP 进程保活到 BTW worker 完成并发出 release。BTW 在第三个 schema 中接纳事实。文章修订由测试夹具设置并通过真实 Article RPC 读取；没有人工构造 Favorite Outbox。

同次顺序为：Article 当前源处于编辑中、公开指针仍为 r1；Favorite 保存公开 r1 标题与 `target_revision=article-shared-authority:r1`，DC 接纳 v1；Article 指针发布 r2，公开 RPC 读回 r2，重复收藏同文件夹返回 `AlreadyExists` 且旧 v1 payload 不变；从原 FavoriteId 经 gRPC 删除，v2 仍为 r1，DC 接纳 v2；RTW 权威读逐条核对源 hash、DC 回执与前驱；BTW 正式 worker 经 Binder/Graph 在 PG `usermodel_events` 中得到两条 `accepted`、版本 1/2、相同的 `ValueRef=article/article-shared-authority/revision/article-shared-authority:r1`，v2 的 `supersedes_event_id` 指向 v1。worker 注入首次 ACK 失败后重启，重放不增事实且最终 ACK 推进到 v2 offset。

`acceptance-full.sh` 使用 `umask 077`；含临时服务令牌的 ready 文件由 RTW 原子写入并验证权限 `0600`，BTW 写 release 后 RTW 同样验证 `0600`。ready/release 只在本次隔离测试目录存在，不输出令牌。未设置完整开关时，原共享 fixture 仍按原逻辑运行，`acceptance.sh`、`acceptance-dc.sh` 和 Article `acceptance.sh` 不改变入口。

2026-09-15 最终同次验收对 RTW 基线 `c75d959`、固定只读 BTW worktree 的精确 HEAD `210aa73c1f0fb697e43d57399e47644b467185ed` 和 DC 已推集成 `e8c8cd` 退出 0。证据目录 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-fact-process.BIsOsX`：`rtw-test.log` 记录 FavoriteId `2099542074528018432`，v1/v2 为 `favorite.2099542074528018432.v1/.v2`、DC offsets `1,2`、Article 公开 r2 与源事实 r1，且两级 RTW 测试 PASS；`btw-test.log` 的 `TestFavoriteFactWorkerProcessReal` PASS，其代码逐行查询 PG 的 `value_ref/status/accepted_version/supersedes_event_id` 并核 ACK 重启和 Graph trace。两个密钥不在日志中。此前两次同次运行也通过，但由于 BTW 集成树在并行工作中前进，最终验收只采用固定 HEAD 的这次证据。

旧路径复验均退出 0：Favorite+真实 DC `acceptance-dc.sh` 证据 `sea-favorite-dc.rmN9bu`，其中旧 `TestFavoriteArticleLiveChain` PASS、新 full-chain fixture 未开关时 SKIP；Favorite 单独 `acceptance.sh` 证据 `sea-favorite-fact.dJSvi5`；Article 单独 `acceptance.sh` 证据 `sea-article-revision.wHwRqJ`。这些脚本在各自隔离 PG/race 上执行并经 vet 检查。

边界：Article 修订及公开指针由测试夹具写入文章库，本验收没有覆盖真实审核消息与对象存储；UserRPC 在 Favorite 测试进程中使用固定活跃 UID 的接口夹具。验证的是从真实 Article RPC 公开投影开始，经 Favorite 业务 gRPC、RTW 事务/独立进程、DC 事件服务到 BTW 正式 worker/Graph/PG 的同次链路，未进行生产部署。
