# WS02-B 收藏互动事实验收

日期：2026-09-14。状态：**RTW Favorite gRPC→PostgreSQL→H09.a本域Outbox 子链 LOCAL_VERIFIED；H09.a/WS02-B整体 PARTIAL**。

工作区域：[W0] 独立 RTW `sea-rtw-community-facts-20260914`，固定已推基线 `1a8aa0b`；[W1] `service/favorite/rpc/internal/{model,logic,server}`、本域 SQL/测试/说明；[R1] DC eventing、ArticleRPC、UserRPC与Docs只读；[D1] GORM/pgx/go-zero依赖只读；[G1] 原favorite protobuf/API路由不手改；[X1] 隔离PG16及测试RPC服务，无共享Kafka/DC写入；[N1] 原始脏树和其他业务域；[T1] 留存独立PG验收目录。主职责[C4:Persistence]原 FavoriteItem 与事实outbox同事务，跨[C1]原gRPC合同、[C3]本人/目标/重投状态、[C7]H09 payload与[C8]真实存储/协议验收。

`001_favorite_fact_outbox.sql` 为现有 Favorite PG 增加独立技术待投表；服务仍使用原 `favorite_id`、文件夹和列表接口。新增收藏成功时在同一事务插入 FavoriteItem 与 `favorite.<id>.v1` assert，单项取消在同事务删除该行并写 `v2` retract；删除文件夹逐条写每个收藏的 retract 后才整体删除。文件夹归属在行锁内再核对。重复文件夹-目标、并发重复、错误用户或重复撤回都不产生额外事实；outbox写失败则业务行回滚。已保存内容再次收藏会获得新的 FavoriteId，不能复活旧ID。

H09 payload 固定 `schema_version/event_id/subject_ref/target_type/target_id/target_revision/operation/source_ref/event_time/available_at/favorite_id/folder_id`；完整主体从 UserRPC 已验证的数字 UID构造为 `rtw.identity/platform/<UID>`，停用或旧RPC缺状态拒收。当前 ArticleRPC不给可证明修订号，`target_revision=null`，不得从显示标题猜版本；`source_ref=rtw.favorite/<FavoriteId>`。外层按 DC eventing 字段命名，但本域仅存 pending outbox，**未声称 DC 已技术接纳**。业务时间是事务内取样时间，不是PG提交时钟。

`bash service/favorite/rpc/acceptance.sh` 自建隔离PG16.14，`-race -count=1` + vet退出0；最终证据 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-favorite-fact.Y3AgOn/go-test.log`。现有真实gRPC CreateFolder/CreateFavorite/DeleteFavorite仍返回原ID，经网络与PG核对同ID正向/撤回outbox；模型用例证明两并发同目标仅一个插入、跨用户拒、重复无增量、删文件夹逐项撤回、故障注入时业务行与outbox同回滚。状态合同测试确认停用UID、旧RPC无状态字段和RPC回错UID均拒收。

DC EventSpec/技术投递、WS07 DWD及 BTW 用户模型真正消费、ArticleRPC正式目标修订、网页/桌宠旧路由端到端仍未联验。本文件不把收藏断言当作客户端真实曝光，也不把本地Outbox状态写成数仓标签或实验效果。
