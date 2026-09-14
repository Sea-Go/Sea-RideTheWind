# 真实User Center产品历史的双阶段交接窗口

`service/knowledge/scripts/history_shared_acceptance.sh`在新的隔离PostgreSQL16构建并运行真实User RPC、User Center API和Knowledge HTTP服务，只执行`TestRealHTTPKnowledgeWorkflowWithUserCenter`的`-race`门禁。调用方提供四个全新绝对路径：`KNOWLEDGE_SHARED_HISTORY_{READY,RELEASE,WITHDRAWN_READY,WITHDRAWN_RELEASE}`。测试于两笔已接纳答案与引用仍可用时，以0600原子ready文件交出临时`base_url/product_token/other_token/session_id/answer_ids/accepted_ordinals/expected_citation_states`，等待第一释放；随后它按原有产品流程撤回内容，引用状态核为`unavailable`后交出第二ready并等待第二释放。测试默认无这些环境变量时不等待、不输出凭证。ready只供当前测试进程内的跨仓消费者读取，绝不入Git或日志。

2026-09-15隔离脚本最终退出0，证据`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-shared-history.HY4pOr/go-test.log`：真实User Center登录的本人在第一阶段按`after_ordinal`读到ordinal1→2，两个引用都`available`；另一用户按AnswerID查本人答案为404。第二阶段同两笔引用都`unavailable`，另一用户的同逻辑session列表为200空页。WhaleHall Bun读取适配在这同一保活窗口里实际读两阶段并没有把quote正文放进renderer-safe结果；具体客户端测试以WhaleHall仓内回执为准。RTW Go race、vet、mod verify同时通过。

最终可复跑父入口`python3 service/knowledge/scripts/whalehall_history_acceptance.py --whalehall-root <隔离WhaleHall树> --output <新证据目录>`自动启动上述真RTW/UserCenter窗口，读取两份0600 ready，在每个阶段运行WhaleHall Bun原客户端测试，释放后等待RTW race/vet/mod verify终态；失败也释放两阶段，避免留下服务。新**最终**报告`/private/tmp/sea-whalehall-history-parent-20260915/report.json`为`passed`：两笔已接纳答案在available阶段均`excerptVisible=true`，撤回后的unavailable阶段均`excerptVisible=false`，另一个真实账号两阶段都是空页，Bun结果不含原始quote字段；RTW测试日志`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-shared-history.qa7khe/go-test.log`以race PASS。早期`HY4pOr`只验证了**始终不显示摘录**的旧客户端实现，不替代新正反引用验收。

这里并没有把WhaleHall的DataCenter UUID映射成RTW UID；JWT由**真实RTW User Center**在隔离测试中签发。生产RTW会话提供者、跨端账号绑定、桌宠/主窗口真实可见曝光、引用详情点击及上线部署仍需独立验收。两阶段ready含临时token，证据目录由当前机器持有，不能公开推送原文件。
