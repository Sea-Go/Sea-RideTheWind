# H06/H07：真实三路索引到有引用产品搜索的隔离交接

日期：2026-09-14。RTW 基线 `5c20dc0`，BTW 基线 `a72adc0`，DC 基线 `e8c8cd68`。本次只改独立 RTW 工作树中的 HTTP/PG opt-in 验收与本说明；DC、BTW 集成树和 Docs 为只读，原始脏树保持原样。

当 `SEA_DC_BGE_RUNTIME` 和 `SEA_BTW_PRODUCT_SEARCH_ROOT` 同时设置时，`TestRealHTTPKnowledgeWorkflow` 不使用原先的结构性 `testenv.Index`。它从 DC 真实 BGE-M3 控制面 `runtime.json` 冻结 Dense、学习型 Sparse、ColBERT 三份 RTW retrieval profile；RTW 正常创建 source、Wiki、release 和 build，并向隔离对象存储写入同一 release 的两片段 manifest。BTW 独立子进程读取该 manifest，经 DC 真实 typed 表示构建并自检三个 local-exact lane；RTW 读取 BTW 回传的 index manifest 与全部 lane ref，调用自己的 `AcceptBuild` 到 READY，再通过管理员 API **人工发布**。当前快照必须逐 lane 等于这些 ref。

发布后 RTW 以真实 User Center JWT/实时 User RPC 签发产品搜索范围。BTW 消费者使用真实 `search.Service.Execute` 对这三个已发布工件查询；断言三路都命中、融合首位是 RTW 来源段落，没有直接注入候选。随后同版原文读取、耐久引用收据、固定模型 JSON、原生 tRPC-Agent-Go 根 Graph/Runner、RTW PG 已接纳答案与引用逐项检查；同键 POST、GET 和跨主体拒读沿用原产品合同。没有证据的 `insufficient` 分支仍独立验证。

第一次实跑遇到 BGE Provider 单并发造成三路并行查询 429：BTW 拒绝不完整结果，RTW 正确返回可重试 503。BTW 搜索 API 装配新增显式单进程共享表示调用门限，本次使用 1。第二次在 DC 保活的真实 BGE 网关上执行 `KNOWLEDGE_KEEP_EVIDENCE=1 KNOWLEDGE_REAL_USER_GATE=1 SEA_DC_BGE_RUNTIME=<隔离 runtime.json> SEA_BTW_PRODUCT_SEARCH_ROOT=<BTW 独立工作树> bash service/knowledge/scripts/acceptance.sh`，**完整退出 0**；真实 User Center 版 54.07s PASS、普通 gRPC 版 20.16s PASS，知识与 User Center race、vet 通过。DC 控制面/Provider 测试释放后 PASS；第二轮三路查询均 HTTP 200。该运行的两个隔离 evidence directory 分别由脚本打印，含模型/PG/HTTP 日志，不提交令牌。

不设置两个 opt-in 环境变量时，原结构性验收也以 `KNOWLEDGE_KEEP_EVIDENCE=1 bash service/knowledge/scripts/acceptance.sh` 完整退出 0；`TestRealHTTPKnowledgeWorkflow` 2.62s PASS。两种模式的结论和工件来源保持区分。

此证据接纳“真实 DC BGE 三种表示 → 同代 BTW 本地 exact 三路工件 → RTW READY/人工发布 → BTW 实际召回 → RTW 引用及产品答案”**子链 `INTEGRATED`**。构建走测试固定两片段与 lane Build，未经过正式 BTW `content.IndexCoordinator`、DC 技术 job 或生产对象存储；搜索走独立测试进程，不是正式 `cmd/api` socket；模型是引用收据后的固定替身，非线上 LLM。Milvus、多实例模型并发、Collector、详搜/高智能、Tools/SSE、客户端真实 EOF/网页同链及质量规模评测仍未验，H06/H07/H02 整体继续 `PARTIAL`。
