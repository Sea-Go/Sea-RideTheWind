# H02/H07：正式 BTW 搜索 API socket 子链验收

日期：2026-09-14。RTW 独立分支 `feat/knowledge-search-api-socket-cross-20260914` 基于集成头 `ded36cb`；BTW 配套分支 `feat/search-api-socket-cross-20260914` 基于 `caed957`。本子链 **`INTEGRATED`**，H02/H06/H07 整体仍 **`PARTIAL`**。

RTW 的真实 User Center/知识 PostgreSQL 验收在一次性 DataCenter BGE-M3 runtime 下创建来源 `Book A\n\nEvidence` 与 Wiki 修订。测试 manifest 只含来源第二段和 Wiki 一段，并不声称全来源覆盖。BTW build-only 子进程从此 manifest 和活 DC typed representation 控制面构建、probe Dense/Sparse/Multi-vector 三路实际 `local-exact` 工件，写出同代不可变 ref 和同一套 document/query 配置后退出。RTW 从自己对象目录核对工件，提交 READY，并由管理员人工激活发布指针。产品请求前另起正式 `cmd/api` 二进制：显式 `local-exact`、`fast/low` 策略、DataCenter 物理表示门限 1、RTW Worker URL/token、独立回环 API/metrics socket、本地 OTLP 接收端和固定 OpenAI 兼容模型夹具。未签名请求返回 403，固定模型调用数仍为 0。

RTW 经 JWT 和真实 User RPC 确认 `rtw.identity/platform/<UID>`，签发当前发布/逻辑会话/search/answer ID 和 HMAC 范围。透明中继只转发原 body/header 到**正式进程 socket**。该进程查询刚发布的三路工件，经 RTW 同版原文 Reader 和耐久引用接纳后才调用一次固定模型夹具；夹具从真实 EvidencePack 取得证据 ID，仅返回确定性 `answer/citations` JSON，不代表真实 LLM 质量。RTW 只在自身 PG 回查相同主体、会话、search/answer、引用行及原文后公开答案。同键重投和固定 GET 与首次结果一致，BTW 只收到首次调用；跨主体请求仍 404。

验收命令先在 DC 集成树启动原有 `BGE_HOLD_FOR_CONSUMER=1` 的锁定 BGE-M3 控制面测试，并核对**活测试进程**产生的 0600 `runtime.json`。再在本 RTW 分支运行：

```bash
KNOWLEDGE_KEEP_EVIDENCE=1 KNOWLEDGE_REAL_USER_GATE=1 \
SEA_DC_BGE_RUNTIME=<本次DC隔离目录>/runtime.json \
SEA_BTW_PRODUCT_SEARCH_ROOT=<配套BTW独立工作树绝对路径> \
SEA_BTW_SEARCH_API_SOCKET_ROOT=<同一BTW独立工作树绝对路径> \
bash service/knowledge/scripts/acceptance.sh
```

最终脚本**退出码 0**：真实 User Center 和同协议 gRPC 替身两套 `TestRealHTTPKnowledgeWorkflow` 分别 PASS（63.84s、33.10s）；`service/knowledge/...` 与 User Center 相关测试以 race 模式通过，两个范围 `go vet`、`git diff --check` 通过。正式进程 `/livez` 204、`/metrics` 200；业务后存在 `sea_btw_operations_total`。本机 OTLP HTTP 接收端按 protobuf 解码并断言 instrumentation scope 为 `trpc.agent.go`、span 名为 `invoke_agent search_summary_root`；SIGTERM 后进程正常退出并输出结构化 started/stopped 事件。DataCenter 持有进程按一次性 release file 释放，原 DC 真实模型测试也退出码 0、Provider 已等待退出。两仓 `go mod verify` 和 BTW 全根 `go test -mod=readonly -race -count=1 ./...` 通过。

首次在新 BTW 工作树创建 uv 环境并加载约 2.3 GB 模型时，DC 现有 90 秒启动门限超时；复用同一锁定模型缓存、重新建立隔离 PG/Provider 后启动成功。正式 socket 首轮预检曾错误地在任何业务请求前要求业务指标；改成启动只检查端点，并在签名产品请求之后验证指标/原生 Trace，完整脚本随后两次退出 0。后续应单独评估冷启动门限，不能把预热重试视为生产部署验收。

本验收没有走正式 `content.IndexCoordinator`/DC 技术 Job 的完整生产建索引路径，也没有 Milvus、生产对象存储、真实 LLM 回答质量、详搜/中高智能、Tools/SSE、客户端浏览器或正式 Collector→DataCenter 查询链。OTLP 已证明本机接收和原生 span，尚未证明共享 Collector 下钻。
