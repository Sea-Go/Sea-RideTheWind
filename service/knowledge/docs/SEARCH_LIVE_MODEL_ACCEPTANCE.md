# RTW 产品搜索接入本机 DataCenter 真模型的测试验收

日期：2026-09-15。此切片只在 `TestRealHTTPKnowledgeWorkflowWithUserCenter` 显式设置 `SEA_BTW_SUMMARY_DC_RUNTIME_FILE` 时启用：父测试向独立 BTW `cmd/api` 提供真实 RTW 已发布索引/正文和一次性 DC 网关清单；BTW 模型请求由 DC 实际路由本机 Ollama，而非原有固定 OpenAI 响应。RTW 产品服务的默认 fast/low 预算和运行代码不改；仅这个真模型验收把测试配置的 fast 超时设为 60 秒、测试 HTTP 客户端设为 95 秒。

第七轮同源测试成功路径：RTW User Center 签发主体→RTW Knowledge PostgreSQL 发布→DataCenter BGE-M3 三路索引→BTW 正式 HTTP/原生根 Agent→RTW 同修订原文/引用耐久接纳→DataCenter Chat Gateway/Ollama→RTW `knowledge_accepted_answers`、`knowledge_answer_citations`、`knowledge_search_citations` 各一行，产品历史和引用状态可读，同键 POST/GET 不重做。RTW 输出 0600 [答案报告](/private/tmp/sea-rtw-live-summary-result-r7-20260915/summary-search_bcd52eec-7e05-4b9a-8821-a524b4de98e5.json)（SHA `8047eda3f04cdea933d9d7327bb76f10040e7eabd8e55ddd0f9e399ca910d343`），端到端 `10632ms`。BTW 同模型调用与 RTW AnswerID 的精确绑定及 DC 实际 tokens/延时见 BTW `cmd/search-summary-usage-acceptance/ACCEPTANCE.md`。

先前失败轮的 400 幂等键、400预算、5秒客户端取消、空content和重复引用均没有提交 unsupported answer。最终第七轮**仅功能 L3**：原文 `Evidence`，模型答 `"Evidence" is the title of a source.`，其中 title 无原文支撑，语义质量门禁未通过。此结果不可作为上线或完整十二格搜索验收；本地服务测试进程均已释放。
