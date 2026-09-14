# RTW人工发布到BTW候选池的隔离交接

状态：**真实RTW Worker/人工发布→BTW隔离PG不可变候选池子链`INTEGRATED`（2026-09-14）**；WS08-E/H08/H11整体仍`PARTIAL`。

RTW知识服务的`TestRealHTTPKnowledgeWorkflow`在真实go-zero HTTP/隔离PostgreSQL中创建来源与Wiki修订、release/build，并按现有`AcceptBuild` READY后由管理员显式发布。设置`SEA_BTW_ITEM_CONSUMER_ROOT=<BTW集成树绝对路径>`时，父测试把本次RTW Worker URL/令牌、隔离PG DSN、module/revision/item ID写入0600 fixture；独立编译BTW `internal/app` race测试二进制，不把候选或正文直接注入它。BTW用生成客户端读取RTW当前发布快照及逐修订metadata，在同一隔离PG的独立随机schema执行`migrations/recommend/001_pools.sql`，提交不可变`PoolRelease`并查1–100条新内容候选；断言指定RTW来源item与修订、发布三路ref/feature hash完全一致，且该release只有一行。隔离schema由子进程结束时清理，原RTW知识schema不被候选表覆盖。

在RTW集成树运行`GOFLAGS=-p=2 GOMAXPROCS=2 KNOWLEDGE_KEEP_EVIDENCE=1 KNOWLEDGE_REAL_USER_GATE=1 SEA_BTW_ITEM_CONSUMER_ROOT=/Users/edy/Sea/.codex-worktrees/sea-btw-runtime-content-20260914 bash service/knowledge/scripts/acceptance.sh`，完整退出0；真实User Center版与gRPC替身版`TestRealHTTPKnowledgeWorkflow`分别PASS（35.88秒、10.31秒），Knowledge/User Center race及vet均通过。BTW子进程有`--- PASS: TestRTWRealItemPoolHandoff`回执，日志由RTW总测试记录；本次证据目录为脚本打印的隔离临时路径。该门禁源于BTW`internal/app/recommend_rtw_real_test.go`，不替代它的本地PG/race/ItemCF数学反例。

当前只证明**同代真实RTW发布元数据进入可重建候选池**。没有正式推荐进程装配、内容撤回后的跨仓二阶段请求、成熟真实H09/WS07-C DWS来源、ItemCF耐久发布、用户Bundle与item index的同空间配对、真实展示反馈或线上效果；池的`behavior_state=not_connected`不得写成个性化推荐已生效。
