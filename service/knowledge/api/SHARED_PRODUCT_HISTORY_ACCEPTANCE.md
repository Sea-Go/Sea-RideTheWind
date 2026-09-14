# 真实User Center产品历史的双阶段交接窗口

`service/knowledge/scripts/history_shared_acceptance.sh`在新的隔离PostgreSQL16构建并运行真实User RPC、User Center API和Knowledge HTTP服务，只执行`TestRealHTTPKnowledgeWorkflowWithUserCenter`的`-race`门禁。调用方提供四个全新绝对路径：`KNOWLEDGE_SHARED_HISTORY_{READY,RELEASE,WITHDRAWN_READY,WITHDRAWN_RELEASE}`。测试于两笔已接纳答案与引用仍可用时，以0600原子ready文件交出临时`base_url/product_token/other_token/session_id/answer_ids/accepted_ordinals/expected_citation_states`，等待第一释放；随后它按原有产品流程撤回内容，引用状态核为`unavailable`后交出第二ready并等待第二释放。测试默认无这些环境变量时不等待、不输出凭证。ready只供当前测试进程内的跨仓消费者读取，绝不入Git或日志。

2026-09-15隔离脚本最终退出0，证据`/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-shared-history.HY4pOr/go-test.log`：真实User Center登录的本人在第一阶段按`after_ordinal`读到ordinal1→2，两个引用都`available`；另一用户按AnswerID查本人答案为404。第二阶段同两笔引用都`unavailable`，另一用户的同逻辑session列表为200空页。WhaleHall Bun读取适配在这同一保活窗口里实际读两阶段并没有把quote正文放进renderer-safe结果；具体客户端测试以WhaleHall仓内回执为准。RTW Go race、vet、mod verify同时通过。

这里并没有把WhaleHall的DataCenter UUID映射成RTW UID；JWT由**真实RTW User Center**在隔离测试中签发。生产RTW会话提供者、跨端账号绑定、桌宠/主窗口真实可见曝光、引用详情点击及上线部署仍需独立验收。两阶段ready含临时token，证据目录由当前机器持有，不能公开推送原文件。
