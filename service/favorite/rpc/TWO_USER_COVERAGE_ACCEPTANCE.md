# 双用户收藏事实同次覆盖来源验收

`TestFavoriteDeliveryTwoUsersSharedAuthorityFixture`仅在设置`FAVORITE_TWO_READY_FILE`与`FAVORITE_TWO_RELEASE_FILE`时运行，使用隔离PG schema、正式Favorite存储、`fact-dispatch`进程、真实DataCenter eventing和私有`fact-authority`进程。它按顺序提交用户1001收藏建立、用户1002收藏建立、用户1001收藏撤回；各事件经RTW Outbox派发并在DC取得offset 1/2/3，随后RTW权威读逐项核主体、输入hash、技术receipt和撤回前驱。0600 ready文件只给父测试传递临时连接信息，不输出token。

BTW`warehouse/coverage/combined_acceptance.sh`在隔离RTW/DC/PG16/ClickHouse/dbt/SeaweedFS父运行中消费此来源，同次验全局W1/W3、主体稀疏切片与空主体、WS08 Graph/PG、H10原S3候选、v2历史接纳、快照和默认关闭固定规则Bundle。2026-09-15最终报告位于`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-coverage-combined.xDFmBz/cross-domain/report.json`，`multi.evidence_level=L2_same_run_real_RTW_DC_PG_CH_S3`；RTW该测试、BTW父用例均以`-race`通过。原双用户HTTP权威夹具仍单独运行，负责错归属等拒错证据。

这证实的是RTW**收藏源**双用户交错流，不是User Center真实注册/登录、WhaleHall/DataCenter账号绑定、线上多用户流量或正式FeatureSpec/Pair批准。v2 Bundle只建`candidate_default_off`，没有活动头或Serving激活。
