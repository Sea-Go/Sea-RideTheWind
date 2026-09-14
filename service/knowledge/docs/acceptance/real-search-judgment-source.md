# 搜索判断来源的跨仓同轮验收

2026-09-15，基于 RTW `bd1fd46`、BTW `f53faae`、DC `cc73d0af` 的隔离分支。只在 RTW 测试配置启用 `SearchJudgments`；测试管理员的 grade=2 和撤回都是**合成操作**，没有真实人工标注，也没有候选池覆盖回执。RTW 正式 HTTP/PG 将两版判断与原始 EventSpec、Outbox 一起提交；正式 `DispatchOne`/`HTTPSender` 将整个共享 `ridethewind.knowledge` producer 发给真实 DC `cmd/platform`/独立 PostgreSQL。BTW 用真 DC event batch/receipt 和 RTW 私有事件读，执行 `searchsource.Consumer` 的独立 ODS 事务与自己的 ACK。

最终在 `KNOWLEDGE_REAL_USER_GATE=1` 且设置 `SEA_BTW_SEARCHSOURCE_CONSUMER_ROOT`、`SEA_DC_EVENT_PLATFORM_ROOT` 的隔离测试中，`TestRealHTTPKnowledgeWorkflowWithUserCenter` 和 `TestRealHTTPKnowledgeWorkflow` 两个 race 场景均 PASS。各场景 DC 接纳 8 条事件：2 条判断修订、6 条同 producer 非 qrel 事件；BTW ODS 各保留 8 行并把非 qrel 明确标为 `technical_skip`，判断撤回的 grade 为 NULL，独立 consumer 与 DC ACK 都到 offset 8，重读不重复。最终运行日志 `/private/tmp/sea-ws07-searchsource-final.log`；RTW 证据目录 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-acceptance.bOYeWA`；BTW 两次 PG/race 目录分别为 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-search-qrel-source.kMDXSM`、`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-search-qrel-source.yM0JPD`。脚本包含 RTW/BTW race、相关 vet 与 diff 检查，退出码 0。这些是本机临时证据，未部署。

首轮真实调用暴露 BTW `HTTPAuthority` 把 RTW 实际成功 envelope `code=200` 错当 `code=0` 的协议断点，证据 `/private/tmp/sea-ws07-searchsource-first.log` 与 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-search-qrel-source.gp2Qci/go-test.log`。改为仅接受 200，单测保留非 200 拒错。随后同一个 BTW 包内合成 PG 测试留下 offset 3，真实测试误复用同一一次性数据库，导致旧 EventID 不匹配；真实测试现在先清空自身隔离 PG 的 ODS/cursor 再从 DC offset 1 验收。这个清空只在 opt-in 测试执行，不属于消费者运行时逻辑。失败阶段没有成功 ACK，不计为验收通过。

本次等级是**L3 本机原始来源链 / WS07-D、WS09-A 的子链完成**；正式商品判断入口默认关闭。这里只证明来源捕获、不可变字节、技术传输、混合 producer 的 ODS/独立游标与 ACK；ODS 下游 qrel 子流、ClickHouse/dbt、候选池覆盖、人工评审和指标可用性须另附同源收据。测试生成的 `judgment_source=human_judgment` 是协议字段，不能把这次合成测试称为真实人工判断。
