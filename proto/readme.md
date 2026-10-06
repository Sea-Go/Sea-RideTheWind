# reco 契约字段清单（三方评审草案 v1）

> 变更ID: refactor-reco-trpc-agent-go
> 阶段: Phase 0 / Task 0.5
> 状态: 草案 - 待三方签字（后端 Sea-BreakTheWaves / 主后端 Sea-RideTheWind / 前端 Sea-RideTheWind-Fronted）
> 原则: 向后兼容；字段命名 proto 用 snake_case，api 用 camelCase，TS 用 snake_case（与现有 reco.ts 一致）

## 评审记录
| 角色 | 评审人 | 结论 | 时间 | 备注 |
|------|--------|------|------|------|
| 后端 | (待签) | | | |
| 主后端 | (待签) | | | |
| 前端 | (待签) | | | |

---

## 风格基线（参考已有 article.proto / article.api）

- **proto**：`syntax = "proto3";`；`package xxx;`；`option go_package = ".";`；中文注释紧贴字段；时间戳统一 `int64`（毫秒）；扩展字段统一 `map<string, string> ext_info`；枚举 0 值为 `*_UNSPECIFIED`。
- **api**：`syntax = "v1";`；type 块定义请求/响应结构体；Go 字段名 camelCase，`json` tag 用 snake_case；可选字段标 `,optional`；query 用 `form:`，路径参数用 `path:`；`@server` 块定义 prefix/group/jwt。
- **TS**：字段统一 snake_case；可选字段加 `?`；扩展用 `[key: string]: unknown` 或 `Record<string, unknown>`（与现有 `src/services/reco.ts` 一致）。
- **网关前缀**：reco 相关 HTTP 路由统一 `/api/v1/v2/...`（spec 明确），admin 端点统一 `/api/v1/v2/admin/...`。

---

## 1. reco.proto / reco.api

### 1.1 proto 定义

```proto
syntax = "proto3";
package reco;
option go_package = ".";

// 推荐路径模式（OrchestratorAgent 路由决策依据）
enum PathMode {
  PATH_MODE_UNSPECIFIED = 0; // 未指定，由 OrchestratorAgent 自动决策
  AUTO = 1;                  // 自动：简单→fast / 中等→hybrid / 复杂→slow
  FAST = 2;                  // 快路径：纯传统推荐，0 LLM token，P99 < 100ms
  SLOW = 3;                  // 慢路径：多 Agent 全流程，高质量高 token
  HYBRID = 4;                // 混合：快路径 Top-50 → 慢路径 Rerank Top-20 → Top-10
}

// 实际走过的路径（响应回填，便于观测与 A/B 对比）
enum PathTaken {
  PATH_TAKEN_UNSPECIFIED = 0;
  PATH_TAKEN_FAST = 1;
  PATH_TAKEN_SLOW = 2;
  PATH_TAKEN_HYBRID = 3;
}

// 标签过滤（召回阶段硬过滤，配合频道独立池）
message TagFilter {
  repeated string include_tags = 1;      // 命中任一即保留（OR）
  repeated string exclude_tags = 2;      // 命中任一即排除（NOT）
  optional string manual_type_tag = 3;   // 一级标签精确匹配
  repeated string secondary_tags = 4;    // 二级标签
}

// 推荐自定义配置（业务二开 + 模型二开 + A/B 分桶）
message RecommendConfig {
  int32 top_k = 1;                       // 返回条数，默认 10
  string recall_strategy = 2;            // 召回策略组合：content,cf,graph,channel,rule
  string rerank_model = 3;               // rerank 模型版本：self / external / llm
  map<string, double> rerank_weights = 4;// rerank 各源权重
  bool agent_loop_enabled = 5;           // 是否启用 Agent 循环（slow 路径）
  bool fallback_enabled = 6;             // 召回不足时是否兜底
  int32 max_tool_calls = 7;              // Agent 最大工具调用次数（WithMaxIterations）
  bool prompt_cache_enabled = 8;         // Prompt Cache 开关
  bool summary_enabled = 9;              // Session Summary 开关
  int32 best_of_n = 10;                  // QualityAgent Best-of-N 次数
  double quality_threshold = 11;         // 质量阈值过滤
  bool cf_enabled = 12;                  // 是否启用 CF 召回
  bool search_enabled = 13;              // 是否启用搜索召回
  bool temporal_decay_enabled = 14;      // 时间画像衰减开关
  PathMode path_mode = 15;               // 路径模式
  string quality_model = 16;             // 质量评判模型版本
  string prompt_cache_key = 17;          // Prompt Cache 分桶键（频道+用户）
}

// 成本报告（响应回填，含 token 与缓存命中）
message CostReport {
  int64 input_tokens = 1;                // 输入 token
  int64 output_tokens = 2;               // 输出 token
  int64 cached_tokens = 3;               // 缓存命中 token
  double cost_yuan = 4;                  // 估算成本（元）
  int64 latency_ms = 5;                  // 端到端延迟（毫秒）
  map<string, int64> per_stage_tokens = 6; // 分阶段 token（intent/recall/graph/rerank/quality/explain）
  map<string, int64> per_stage_latency_ms = 7; // 分阶段延迟
}

// 推荐请求
message RecommendRequest {
  string user_id = 1;                    // 用户ID
  string session_id = 2;                 // 会话ID（Session 隔离）
  string trace_id = 3;                   // 链路追踪ID（透传 OTel）
  string surface = 4;                    // 展示场景（dashboard_recommend / channel_xxx）
  string query = 5;                      // 用户查询/上下文（可空）
  string channel = 6;                    // 频道名（IP 主题频道，空表示默认流）
  TagFilter tag_filter = 7;              // 标签过滤
  RecommendConfig config = 8;            // 自定义配置
  PathMode path_mode = 9;                // 路径模式（与 config.path_mode 二选一，request 优先）
  int32 top_k = 10;                      // 返回条数（与 config.top_k 二选一）
  string period_bucket = 11;             // 时间桶（与前端 reco.ts 对齐，画像时段）
  bool explain = 12;                     // 是否生成推荐解释
  bool stream = 13;                      // 是否流式（AG-UI）
  map<string, string> ext_info = 14;     // 扩展字段
}

// 推荐响应
message RecommendResponse {
  string trace_id = 1;                   // 链路追踪ID
  string rec_request_id = 2;            // 推荐请求ID
  string status = 3;                     // 状态（ok / fallback / partial）
  repeated string article_ids = 4;       // 推荐文章ID列表
  PathTaken path_taken = 5;              // 实际路径（fast/slow/hybrid）
  CostReport cost = 6;                   // 成本报告
  RecommendConfig config = 7;            // 实际生效配置（含默认值回填）
  string channel = 8;                    // 命中频道
  map<string, double> quality_scores = 9;   // article_id → 质量分
  map<string, double> cf_scores = 10;       // article_id → CF 分
  map<string, double> rerank_scores = 11;   // article_id → rerank 分
  string temporal_profile_snapshot = 12;    // 时间画像快照（JSON，调试用）
  string graph_trace = 13;                  // 图谱查询 trace（Cypher + 结果数 + 耗时，JSON）
  string explanation = 14;                  // 推荐解释文本
  string intent_label = 15;                 // 意图标签（slow/hybrid 回填）
  double intent_complexity = 16;            // 意图复杂度（0~1，驱动路由）
  string err_msg = 17;                      // 错误信息（status!=ok 时）
  map<string, string> ext_info = 18;        // 扩展字段
}

service RecoService {
  rpc Recommend(RecommendRequest) returns (RecommendResponse);
  rpc RecommendStream(RecommendRequest) returns (stream RecommendResponse); // AG-UI 流式
  rpc ListChannels(ListChannelsRequest) returns (ListChannelsResponse);     // 复用 channel.proto
  rpc ReportEvents(ReportEventsRequest) returns (ReportEventsResponse);
}
```

### 1.2 api 网关
- `POST /api/v1/v2/reco/recommend` → RecommendReq → RecommendResp（同步）
- `POST /api/v1/v2/reco/recommend/stream` → RecommendReq → 流式 RecommendResp（AG-UI，SSE）
- `GET /api/v1/v2/reco/channels` → ListChannelsResp（频道列表，用户前台订阅）
- `POST /api/v1/v2/reco/events` → ReportEventsReq → ReportEventsResp（行为事件上报）

### 1.3 与前端 reco.ts 对齐说明
- 现有 `RecommendArticlesPayload`（rec_request_id/user_id/session_id/surface/query/period_bucket）→ 映射到 `RecommendRequest`，新增 channel/tag_filter/config/path_mode/top_k/explain/stream。
- 现有 `RecommendArticlesResponse`（trace_id/rec_request_id/status/ids/explanation/data/list/articles/items）→ 拆为扁平 `RecommendResponse`，新增 path_taken/cost/quality_scores/cf_scores/rerank_scores/temporal_profile_snapshot/graph_trace。
- 现有 `RecoEventItem`（rec_request_id/user_id/session_id/surface/article_id/rank/event_type/metadata）→ 直接映射 `ReportEventsRequest.events[]`，event_type 复用枚举（impression/click/like/dislike/favorite/read_complete）。
- `RecommendArticleItem` 的 `article_id/id/target_id` 三兼容写法保留为 `article_ids` 列表 + 详情二次拉取。

---

## 2. search.proto / search.api

### 2.1 proto 定义

```proto
syntax = "proto3";
package search;
option go_package = ".";

// 搜索意图类型（ParseIntent 结构化输出）
enum SearchIntentType {
  SEARCH_INTENT_UNSPECIFIED = 0;
  INFORMATIONAL = 1;  // 信息型
  NAVIGATIONAL = 2;   // 导航型
  TRANSACTIONAL = 3;  // 事务型
  COMPARATIVE = 4;    // 比较型
}

// 搜索意图（SearchAgent.understand 输出）
message SearchIntent {
  SearchIntentType label = 1;           // 意图类型
  double confidence = 2;                // 置信度
  repeated string signals = 3;          // 触发信号
  double complexity = 4;                // 复杂度（0~1）
  repeated string entities = 5;         // 实体（作者/IP/标签/标题）
  string time_intent = 6;               // 时效意图（latest/within_7d/within_30d/ever）
  map<string, string> ext_info = 7;
}

// 重写后的查询（SearchAgent.rewrite 输出）
message RewrittenQuery {
  string original = 1;                   // 原始 query
  string rewritten = 2;                  // 重写后 query
  repeated string synonyms = 3;          // 同义词扩展
  repeated string related = 4;           // 相关词
  string correction = 5;                 // 拼写纠正（空表示无需纠正）
  map<string, string> ext_info = 6;
}

// 搜索过滤
message SearchFilter {
  repeated string tags = 1;              // 标签过滤
  optional string manual_type_tag = 2;   // 一级标签
  optional string channel = 3;           // IP 频道
  optional string author_id = 4;         // 作者
  optional int64 time_from = 5;          // 时间范围起（毫秒）
  optional int64 time_to = 6;            // 时间范围止（毫秒）
  optional double quality_threshold = 7; // 质量阈值
  map<string, string> ext_info = 8;
}

// 图谱知识（实体识别后查图谱获取）
message GraphKnowledge {
  repeated string entities = 1;          // 命中实体
  repeated string related_articles = 2;  // 关联文章ID
  repeated string related_authors = 3;   // 关联作者ID
  repeated string related_ips = 4;       // 关联 IP
  string cypher = 5;                     // 生成的 Cypher（调试用）
  string subgraph = 6;                   // 子图 JSON（节点+边，前端可视化）
  map<string, string> ext_info = 7;
}

// 搜索命中（单条）
message SearchHit {
  string article_id = 1;                 // 文章ID
  string title = 2;                      // 标题
  string brief = 3;                      // 摘要
  string cover_image_url = 4;            // 封面
  string manual_type_tag = 5;            // 一级标签
  repeated string secondary_tags = 6;    // 二级标签
  string author_id = 7;                  // 作者ID
  string author_name = 8;                // 作者名
  string snippet = 9;                    // 命中片段（高亮）
  string chunk_id = 10;                  // 向量块ID
  double article_score = 11;             // 文章综合分
  double vector_score = 12;              // 向量分
  double rerank_score = 13;              // rerank 分
  double match_score = 14;               // 匹配分
  map<string, string> ext_info = 15;
}

// 搜索查询
message SearchQuery {
  string search_request_id = 1;          // 搜索请求ID
  string user_id = 2;                    // 用户ID（个性化）
  string session_id = 3;                 // 会话ID
  string trace_id = 4;                   // 链路追踪ID
  string query = 5;                      // 原始 query
  int32 topk = 6;                        // 返回条数
  bool need_answer = 7;                  // 是否需要直接答案（FAQ 式）
  bool explain = 8;                      // 是否返回 explain_trace
  SearchFilter filter = 9;               // 过滤
  bool personalize = 10;                 // 是否个性化
  bool use_graph = 11;                   // 是否启用图谱知识
  string image_url = 12;                 // 以图搜文图片URL（非空触发图片搜索）
  map<string, string> ext_info = 13;
}

// 搜索结果
message SearchResult {
  string trace_id = 1;                   // 链路追踪ID
  string search_request_id = 2;          // 搜索请求ID
  string status = 3;                     // 状态
  string answer = 4;                     // 直接答案（need_answer=true）
  SearchIntent intent = 5;               // 意图
  RewrittenQuery rewritten = 6;          // 重写查询
  repeated SearchHit hits = 7;           // 命中列表
  repeated SearchHit items = 8;          // 命中列表（别名，前端兼容）
  GraphKnowledge graph_knowledge = 9;    // 图谱知识
  string explanation = 10;               // 搜索解释
  repeated ExplainTraceItem explain_trace = 11; // 解释 trace
  map<string, string> debug = 12;        // 调试信息
  string err_msg = 13;
  map<string, string> ext_info = 14;
}

// 解释 trace 项
message ExplainTraceItem {
  string name = 1;                       // 阶段名（understand/rewrite/hybrid/graph/rerank）
  string data = 2;                       // JSON 数据
}

// 搜索补全
message SearchSuggestion {
  string text = 1;                       // 补全文本
  double score = 2;                      // 补全分
  string source = 3;                     // 来源（prefix/hot/personalize）
  map<string, string> ext_info = 4;
}

service SearchService {
  rpc Search(SearchQuery) returns (SearchResult);
  rpc Suggest(SearchSuggestionRequest) returns (SearchSuggestionResponse);
  rpc SearchByTitle(SearchQuery) returns (SearchResult);   // 按标题
  rpc SearchByAuthor(SearchQuery) returns (SearchResult);  // 按作者
  rpc SearchByImage(SearchQuery) returns (SearchResult);   // 以图搜文
}
```

### 2.2 api 网关
- `POST /api/v1/v2/reco/search` → SearchReq → SearchResp（语义混合搜索）
- `GET /api/v1/v2/reco/search/suggest` → SuggestReq → SuggestResp（补全，< 100ms）
- `POST /api/v1/v2/reco/search/title` → SearchReq → SearchResp（按标题）
- `POST /api/v1/v2/reco/search/author` → SearchReq → SearchResp（按作者）
- `POST /api/v1/v2/reco/search/image` → SearchReq → SearchResp（以图搜文）

### 2.3 与前端 reco.ts 对齐说明
- 现有 `ContentSearchPayload`（search_request_id/user_id/session_id/query/topk/need_answer/explain）→ 映射 `SearchQuery`，新增 filter/personalize/use_graph/image_url。
- 现有 `SearchRecoHit` 字段（article_id/id/target_id/author_id/author_name/title/brief/cover/manual_type_tag/secondary_tags/snippet/chunk_id/type_tags/tags/article_score/vector_score/rerank_score/match_score）→ 对齐 `SearchHit`，去掉三兼容 id 写法。
- 现有 `SearchRecoData`（trace_id/request_id/search_request_id/status/answer/intent/hits/items/explanation/explain_trace/debug）→ 映射 `SearchResult`，新增 rewritten/graph_knowledge。
- 现有 `StructuredSearchPayload`（search_request_id/query/topk）→ 复用 `SearchQuery`（user_id/session_id 可选）。

---

## 3. channel.proto / channel.api

### 3.1 proto 定义

```proto
syntax = "proto3";
package channel;
option go_package = ".";

// 频道配置（独立池 + 质量阈值 + filterKey + rerank 版本 + IP 绑定）
message ChannelConfig {
  int32 pool_min_size = 1;               // 独立召回池最小水位（不足触发兜底）
  int32 pool_max_size = 2;               // 独立召回池最大水位
  double quality_threshold = 3;          // 频道独立质量阈值
  string filter_key = 4;                 // 画像 filterKey（频道切换隔离）
  string rerank_model_version = 5;       // 频道独立 rerank 模型版本
  string rerank_model_type = 6;          // self / external / llm
  IPBindingRule ip_binding = 7;          // IP 绑定规则
  map<string, string> ext_info = 8;
}

// IP 绑定规则（type_tag + secondary_tags 组合绑定 IP 主题）
message IPBindingRule {
  string type_tag = 1;                   // 一级标签
  repeated string secondary_tags = 2;    // 二级标签组合
  string ip_name = 3;                    // IP 名称（如"旅游"）
  bool exclusive = 4;                    // 是否独占（命中后不再进默认流）
  map<string, string> ext_info = 5;
}

// 频道（IP 主题频道）
message Channel {
  string name = 1;                       // 频道名（唯一键，如 travel / game）
  string display_name = 2;               // 展示名
  string description = 3;                // 描述
  string ip_name = 4;                    // 绑定 IP
  ChannelConfig config = 5;              // 频道配置
  int32 pool_current_size = 6;           // 当前池水位
  bool enabled = 7;                      // 是否启用
  int64 create_time = 8;                 // 创建时间（毫秒）
  int64 update_time = 9;                 // 更新时间（毫秒）
  map<string, string> ext_info = 10;
}

// 注册频道请求（二开点：业务方注册自定义 IP 频道）
message RegisterChannelRequest {
  string name = 1;                       // 频道名
  string display_name = 2;
  string description = 3;
  string ip_name = 4;
  ChannelConfig config = 5;
  IPBindingRule ip_binding = 6;
  bool enabled = 7;
  map<string, string> ext_info = 8;
}

message RegisterChannelResponse {
  string name = 1;                       // 注册成功的频道名
  bool success = 2;
}

message ListChannelsRequest {
  bool enabled_only = 1;                 // 仅返回启用频道（用户前台传 true）
}

message ListChannelsResponse {
  repeated Channel channels = 1;
  int32 total = 2;
}

message GetChannelRequest {
  string name = 1;
}

message UpdateChannelRequest {
  string name = 1;
  optional string display_name = 2;
  optional string description = 3;
  optional ChannelConfig config = 4;
  optional bool enabled = 5;
}

message DeleteChannelRequest {
  string name = 1;
}

service ChannelService {
  rpc RegisterChannel(RegisterChannelRequest) returns (RegisterChannelResponse);
  rpc ListChannels(ListChannelsRequest) returns (ListChannelsResponse);
  rpc GetChannel(GetChannelRequest) returns (Channel);
  rpc UpdateChannel(UpdateChannelRequest) returns (RegisterChannelResponse);
  rpc DeleteChannel(DeleteChannelRequest) returns (RegisterChannelResponse);
}
```

### 3.2 api 网关
- `GET /api/v1/v2/reco/channels` → ListChannelsResp（用户前台，enabled_only=true）
- `GET /api/v1/v2/admin/channels` → ListChannelsResp（管理员，全部）
- `POST /api/v1/v2/admin/channels` → RegisterChannelReq → RegisterChannelResp
- `GET /api/v1/v2/admin/channels/:name` → Channel
- `PUT /api/v1/v2/admin/channels/:name` → UpdateChannelReq → RegisterChannelResp
- `DELETE /api/v1/v2/admin/channels/:name` → RegisterChannelResp

### 3.3 与前端对齐说明
- 新增 `Channel`/`ChannelConfig` TS 类型（前端 `src/services/reco.ts` 当前无频道类型，需新增）。
- `IPBindingRule` 用于管理员"IP 主题绑定"配置面板。
- `filter_key` 用于前端频道切换时传给 `/recommend` 请求的 `config.prompt_cache_key`，实现 Session 隔离。

---

## 4. quality.proto / quality.api

### 4.1 proto 定义

```proto
syntax = "proto3";
package quality;
option go_package = ".";

// 文章质量 6 维评分（LLM Verifier + Best-of-N）
message ArticleQuality {
  string article_id = 1;                 // 文章ID
  double authority = 2;                  // 权威性（0~1）
  double depth = 3;                      // 深度（0~1）
  double freshness = 4;                  // 时效性（0~1）
  double completeness = 5;               // 完整性（0~1）
  double readability = 6;                // 可读性（0~1）
  double citation = 7;                   // 引用质量（0~1）
  double overall = 8;                    // 综合分（加权）
  string grade = 9;                      // 等级（A~T，logprobs 概率分布取 max）
  map<string, double> grade_distribution = 10; // 等级概率分布
  string judge_model = 11;               // 评判模型版本
  int64 judged_at = 12;                  // 评判时间（毫秒）
  map<string, string> ext_info = 13;
}

// 质量报告（含 Rubrics 维度 + 评语）
message ArticleQualityReport {
  string article_id = 1;
  ArticleQuality quality = 2;            // 6 维评分
  repeated RubricDimension rubrics = 3;  // Rubric 各维度明细
  string summary = 4;                    // 质量总结
  string grounding_result = 5;           // validate grounding 结果（PASS/FAIL + 原因）
  int64 create_time = 6;                 // 报告生成时间（毫秒）
  map<string, string> ext_info = 7;
}

// Rubric 维度（accuracy/authority/depth/freshness/completeness/readability/citation）
message RubricDimension {
  string name = 1;                       // 维度名
  double score = 2;                      // 得分（0~1）
  string rationale = 3;                  // 评判理由
  string evidence = 4;                   // 证据片段
  map<string, string> ext_info = 5;
}

// 质量反馈（反馈闭环）
message QualityFeedback {
  string article_id = 1;
  string feedback_type = 2;              // upvote / downvote / flag
  string reason = 3;                     // 反馈原因
  string annotator_id = 4;               // 标注人ID（管理员）
  int64 create_time = 5;                 // 反馈时间（毫秒）
  map<string, string> ext_info = 6;
}

// 质量评估请求
message QualityEvaluateRequest {
  string article_id = 1;
  bool use_best_of_n = 2;                // 是否 Best-of-N
  int32 n = 3;                           // Best-of-N 次数
  string judge_model = 4;                // 指定评判模型
  bool logprobs = 5;                     // 是否返回概率分布
  map<string, string> ext_info = 6;
}

message QualityEvaluateResponse {
  ArticleQualityReport report = 1;
  bool success = 2;
  string err_msg = 3;
}

message GetQualityReportRequest {
  string article_id = 1;
}

message ListQualityFeedbackRequest {
  optional string article_id = 1;
  optional string feedback_type = 2;
  int32 page = 3;
  int32 page_size = 4;
}

message ListQualityFeedbackResponse {
  repeated QualityFeedback feedbacks = 1;
  int64 total = 2;
  int32 page = 3;
  int32 page_size = 4;
}

service QualityService {
  rpc Evaluate(QualityEvaluateRequest) returns (QualityEvaluateResponse);
  rpc GetReport(GetQualityReportRequest) returns (ArticleQualityReport);
  rpc SubmitFeedback(QualityFeedback) returns (QualityFeedbackResp);
  rpc ListFeedback(ListQualityFeedbackRequest) returns (ListQualityFeedbackResponse);
}

message QualityFeedbackResp {
  bool success = 1;
  string err_msg = 2;
}
```

### 4.2 api 网关
- `POST /api/v1/v2/admin/quality/evaluate` → QualityEvaluateReq → QualityEvaluateResp
- `GET /api/v1/v2/admin/quality/report/:article_id` → ArticleQualityReport
- `POST /api/v1/v2/admin/quality/feedback` → QualityFeedback → QualityFeedbackResp
- `GET /api/v1/v2/admin/quality/feedback` → ListQualityFeedbackReq → ListQualityFeedbackResp

### 4.3 与前端对齐说明
- 新增 `ArticleQuality`/`ArticleQualityReport`/`QualityFeedback` TS 类型到 `src/services/admin-reco.ts`。
- 6 维评分用于管理员"质量评估"面板雷达图。
- `grade_distribution` 用于前端展示 A~T 概率柱状图。

---

## 5. rerank.proto / rerank.api

### 5.1 proto 定义

```proto
syntax = "proto3";
package rerank;
option go_package = ".";

// rerank 模型版本（self/external 切换 + A/B 分桶）
enum RerankModelType {
  RERANK_MODEL_TYPE_UNSPECIFIED = 0;
  SELF = 1;           // 自研 rerank（Cross-encoder + Two-tower + LambdaMART）
  EXTERNAL = 2;       // 外部 rerank（DashScope 兜底）
  LLM = 3;            // LLM rerank（大模型精排）
}

// rerank 模型
message RerankModel {
  string version = 1;                    // 版本号
  RerankModelType type = 2;              // 模型类型
  string artifact_path = 3;              // 模型产物路径（ONNX/GGUF）
  bool enabled = 4;                      // 是否启用
  double traffic_ratio = 5;              // 流量占比（A/B 分桶）
  int64 create_time = 6;                 // 创建时间（毫秒）
  map<string, string> metrics = 7;       // 离线指标（NDCG@10/CTR 等）
  map<string, string> ext_info = 8;
}

// rerank 特征（特征工程输出）
message RerankFeatures {
  string article_id = 1;
  map<string, double> text_features = 2;     // 文本特征
  map<string, double> behavior_features = 3;// 行为特征
  map<string, double> temporal_features = 4;// 时间特征
  map<string, double> quality_features = 5; // 质量特征
  map<string, double> profile_features = 6; // 画像特征
  map<string, double> diversity_features = 7; // 多样性特征
  map<string, string> ext_info = 8;
}

// A/B 测试配置
message ABTestConfig {
  string name = 1;                       // 实验名
  string control_version = 2;            // 对照组版本
  string treatment_version = 3;          // 实验组版本
  double control_ratio = 4;              // 对照组流量
  double treatment_ratio = 5;            // 实验组流量
  repeated string metrics = 6;           // 对比指标（CTR/CVR/latency/cost）
  bool enabled = 7;
  int64 start_time = 8;                  // 开始时间（毫秒）
  int64 end_time = 9;                    // 结束时间（毫秒）
  map<string, string> ext_info = 10;
}

// 模型版本切换
message ModelVersion {
  string current_version = 1;            // 当前生效版本
  RerankModelType current_type = 2;      // 当前类型
  string fallback_version = 3;           // 降级版本（self 失败回退 external）
  RerankModelType fallback_type = 4;     // 降级类型
  ABTestConfig ab_test = 5;              // 进行中的 A/B
  map<string, string> ext_info = 6;
}

// rerank 请求（内部 RPC，inference 服务）
message RerankRequest {
  string trace_id = 1;
  string user_id = 2;
  string query = 3;
  repeated string article_ids = 4;       // 候选文章ID
  repeated RerankFeatures features = 5;  // 候选特征
  string model_version = 6;              // 指定版本（空则用当前生效）
  RerankModelType model_type = 7;        // 指定类型
  int32 top_k = 8;                       // 返回条数
  map<string, string> ext_info = 9;
}

// rerank 响应
message RerankResponse {
  string trace_id = 1;
  repeated string ranked_article_ids = 2;// 排序后文章ID
  map<string, double> scores = 3;        // article_id → rerank 分
  string model_version = 4;              // 实际使用版本
  RerankModelType model_type = 5;        // 实际使用类型
  bool fallback_triggered = 6;           // 是否触发降级
  string err_msg = 7;
  map<string, string> ext_info = 8;
}

message ListRerankModelsRequest {
  optional RerankModelType type = 1;
  bool enabled_only = 2;
}

message ListRerankModelsResponse {
  repeated RerankModel models = 1;
  int32 total = 2;
}

message GetModelVersionRequest {}

message UpdateModelVersionRequest {
  optional string current_version = 1;
  optional RerankModelType current_type = 2;
  optional string fallback_version = 3;
  optional RerankModelType fallback_type = 4;
}

service RerankService {
  rpc Rerank(RerankRequest) returns (RerankResponse);   // 内部 inference RPC
  rpc ListModels(ListRerankModelsRequest) returns (ListRerankModelsResponse);
  rpc GetModelVersion(GetModelVersionRequest) returns (ModelVersion);
  rpc UpdateModelVersion(UpdateModelVersionRequest) returns (ModelVersion);
  rpc ManageABTest(ABTestConfig) returns (ABTestConfig);
}
```

### 5.2 api 网关
- `GET /api/v1/v2/admin/rerank/models` → ListRerankModelsResp
- `GET /api/v1/v2/admin/rerank/version` → ModelVersion
- `PUT /api/v1/v2/admin/rerank/version` → UpdateModelVersionReq → ModelVersion
- `POST /api/v1/v2/admin/rerank/abtest` → ABTestConfig → ABTestConfig
- `POST /api/v1/v2/admin/rerank/infer` → RerankReq → RerankResp（内部 RPC 调试，可选暴露）

### 5.3 与前端对齐说明
- 新增 `RerankModel`/`RerankFeatures`/`ABTestConfig`/`ModelVersion` TS 类型到 `src/services/admin-reco.ts`。
- `RerankModelType` 枚举用于管理员"自研/外部切换"面板。
- `fallback_triggered` 用于前端展示降级告警。

---

## 6. cf.proto / cf.api

### 6.1 proto 定义

```proto
syntax = "proto3";
package cf;
option go_package = ".";

// CF 算法类型
enum CFAlgoType {
  CF_ALGO_TYPE_UNSPECIFIED = 0;
  USER_CF = 1;    // User-based CF（用户行为序列 + 余弦相似）
  ITEM_CF = 2;    // Item-based CF（共现矩阵 + Jaccard/余弦）
  MF = 3;         // 矩阵分解 ALS（64/128 维隐因子）
}

// CF 分数
message CFScore {
  string article_id = 1;
  double user_cf_score = 2;              // User-CF 分
  double item_cf_score = 3;              // Item-CF 分
  double mf_score = 4;                   // MF 分
  double fused_score = 5;                // 融合分
  string source = 6;                     // 来源（user_cf/item_cf/mf/coldstart）
  map<string, string> ext_info = 7;
}

// CF 配置
message CFConfig {
  CFAlgoType algo = 1;                   // 主算法
  map<CFAlgoType, double> weights = 2;   // 各算法权重（融合）
  int32 top_n_similar = 3;               // Top-N 相似数
  int32 mf_factors = 4;                  // MF 隐因子维度（64/128）
  int32 refresh_interval_sec = 5;        // 增量刷新间隔（秒，5min 增量 / 1h 全量）
  bool online_update = 6;                // 是否实时增量更新
  map<string, string> ext_info = 7;
}

// 冷启动配置
message ColdStartConfig {
  bool use_profile_recall = 1;           // 新用户用画像召回
  bool use_hot_fallback = 2;             // 热门兜底
  bool use_content_cf_transfer = 3;      // 新文章用内容向量迁移 CF 分
  int32 hot_top_k = 4;                   // 热门兜底条数
  map<string, string> ext_info = 5;
}

// 相似用户查询
message CFSimilarUsersRequest {
  string user_id = 1;
  int32 top_n = 2;
  CFAlgoType algo = 3;
}

message CFSimilarUsersResponse {
  repeated SimilarUser users = 1;
}

message SimilarUser {
  string user_id = 1;
  double similarity = 2;
  repeated string shared_articles = 3;
}

// 相似文章查询
message CFSimilarItemsRequest {
  string article_id = 1;
  int32 top_n = 2;
  CFAlgoType algo = 3;
}

message CFSimilarItemsResponse {
  repeated SimilarItem items = 1;
}

message SimilarItem {
  string article_id = 1;
  double similarity = 2;
  int32 co_occur_count = 3;              // 共现次数
}

message GetCFConfigRequest {}
message UpdateCFConfigRequest {
  optional CFConfig config = 1;
  optional ColdStartConfig cold_start = 2;
}

service CFService {
  rpc SimilarUsers(CFSimilarUsersRequest) returns (CFSimilarUsersResponse);
  rpc SimilarItems(CFSimilarItemsRequest) returns (CFSimilarItemsResponse);
  rpc GetConfig(GetCFConfigRequest) returns (CFConfigResponse);
  rpc UpdateConfig(UpdateCFConfigRequest) returns (CFConfigResponse);
}

message CFConfigResponse {
  CFConfig config = 1;
  ColdStartConfig cold_start = 2;
  bool success = 3;
}
```

### 6.2 api 网关
- `GET /api/v1/v2/admin/cf/config` → CFConfigResponse
- `PUT /api/v1/v2/admin/cf/config` → UpdateCFConfigReq → CFConfigResponse
- `GET /api/v1/v2/admin/cf/similar/users` → CFSimilarUsersReq → CFSimilarUsersResp
- `GET /api/v1/v2/admin/cf/similar/items` → CFSimilarItemsReq → CFSimilarItemsResp

### 6.3 与前端对齐说明
- 新增 `CFScore`/`CFConfig`/`ColdStartConfig` TS 类型到 `src/services/admin-reco.ts`。
- `CFAlgoType` 用于管理员"CF 配置"面板算法切换。
- `CFScore` 三分数（user_cf/item_cf/mf）用于 `RecommendResponse.cf_scores` 调试展示。

---

## 7. profile.proto / profile.api

### 7.1 proto 定义

```proto
syntax = "proto3";
package profile;
option go_package = ".";

// 用户画像三层（静态/动态/行为）+ 时间画像层
message UserProfile {
  string user_id = 1;
  StaticProfile static = 2;              // 静态层（注册信息）
  DynamicProfile dynamic = 3;            // 动态层（偏好）
  BehaviorProfile behavior = 4;          // 行为层（事件统计）
  TemporalProfile temporal = 5;          // 时间画像层
  int64 update_time = 6;                 // 更新时间（毫秒）
  map<string, string> ext_info = 7;
}

// 静态画像（注册/onboarding）
message StaticProfile {
  string username = 1;
  repeated string interests = 2;         // 兴趣
  string primary_purpose = 3;            // 主要目的
  repeated string preferred_article_types = 4; // 偏好文章类型
  string preferred_article_length = 5;   // 偏好长度
  string preferred_style = 6;            // 偏好风格
  repeated string backgrounds = 7;       // 背景
  string difficulty_preference = 8;      // 难度偏好
  repeated string excluded_contents = 9; // 排除内容
  repeated string reading_time_slots = 10; // 阅读时段
  repeated string personalized_recommendation_types = 11; // 个性化推荐类型
  int64 onboarded_at = 12;               // onboarding 时间（毫秒）
  map<string, string> ext_info = 13;
}

// 动态画像（偏好演化）
message DynamicProfile {
  repeated string current_tags = 1;      // 当前活跃标签
  repeated string current_topics = 2;    // 当前活跃主题
  string tier = 3;                       // 用户层级
  repeated string long_term_tags = 4;    // 长期标签
  repeated string short_term_tags = 5;   // 短期标签
  map<string, string> ext_info = 6;
}

// 行为画像（事件统计）
message BehaviorProfile {
  int32 total_clicks = 1;
  int32 total_likes = 2;
  int32 total_dislikes = 3;
  int32 total_favorites = 4;
  int32 total_read_complete = 5;
  int32 total_searches = 6;
  map<string, int32> tag_clicks = 7;     // 标签 → 点击数
  map<string, int32> author_follows = 8; // 作者 → 关注数
  int64 last_active_time = 9;            // 最近活跃时间（毫秒）
  map<string, string> ext_info = 10;
}

// 时间画像（长期/短期/会话/周期/趋势 + 衰减 + 淘汰）
message TemporalProfile {
  LongTermInterests long_term = 1;       // 长期兴趣
  ShortTermInterests short_term = 2;     // 短期兴趣
  repeated InterestEntry session = 3;    // 会话兴趣
  PeriodicPattern periodic = 4;          // 周期模式
  TrendingInterests trending = 5;        // 趋势兴趣
  DecayState decay_state = 6;            // 衰减状态
  EliminationPool elimination_pool = 7;  // 淘汰池
  map<string, string> ext_info = 8;
}

// 兴趣条目（每兴趣含 Tag/Weight/Strength/LastReinforcedAt/DecayTau/Archived）
message InterestEntry {
  string tag = 1;                        // 标签
  double weight = 2;                     // 权重
  double strength = 3;                   // 强度 S
  int64 last_reinforced_at = 4;          // 最近强化时间（毫秒）
  double decay_tau = 5;                  // 衰减时间常数（秒）
  bool archived = 6;                     // 是否归档（淘汰池）
  map<string, string> ext_info = 7;
}

// 长期兴趣
message LongTermInterests {
  repeated InterestEntry entries = 1;
  int64 computed_at = 2;                 // 计算时间（毫秒）
}

// 短期兴趣
message ShortTermInterests {
  repeated InterestEntry entries = 1;
  int64 window_start = 2;                // 窗口起（毫秒）
  int64 window_end = 3;                  // 窗口止（毫秒）
}

// 周期模式（按时间槽位）
message PeriodicPattern {
  map<string, repeated InterestEntry> slot_tags = 1; // slot（如 "0-6"/"6-12"）→ 兴趣
  string granularity = 2;                // 粒度（hour/day/weekday）
  int64 computed_at = 3;
}

// 趋势兴趣（24h vs 7d 基线，2σ 突增）
message TrendingInterests {
  repeated TrendingEntry entries = 1;
  int64 detected_at = 2;
}

message TrendingEntry {
  string tag = 1;
  double current_rate = 2;               // 当前速率
  double baseline_rate = 3;              // 基线速率
  double z_score = 4;                    // 突增 z-score
  bool is_trending = 5;
}

// 衰减状态
message DecayState {
  double global_decay_tau = 1;           // 全局衰减常数
  double ebbinghaus_s = 2;               // Ebbinghaus 强度
  int64 last_tick_at = 3;                // 最近衰减 tick（毫秒）
  map<string, double> per_tag_tau = 4;   // 标签级衰减常数
}

// 淘汰池
message EliminationPool {
  repeated string archived_tags = 1;     // 已淘汰标签
  repeated ReviveCandidate revive_candidates = 2; // 复活候选
  int64 last_tick_at = 3;
}

message ReviveCandidate {
  string tag = 1;
  double archived_score = 2;
  int64 archived_at = 3;                 // 淘汰时间（毫秒）
}

// 画像调优请求（管理员调参）
message ProfileTuningRequest {
  string user_id = 1;
  optional DecayState decay_state = 2;   // 调整衰减参数
  repeated string force_revive_tags = 3; // 强制复活标签
  repeated string force_archive_tags = 4;// 强制淘汰标签
  optional TemporalProfile override = 5; // 覆盖时间画像（调试）
  map<string, string> ext_info = 6;
}

message ProfileTuningResponse {
  UserProfile profile = 1;               // 调优后画像
  bool success = 2;
  string err_msg = 3;
}

message GetUserProfileRequest {
  string user_id = 1;
  bool include_temporal = 2;             // 是否含时间画像
}

service ProfileService {
  rpc GetUserProfile(GetUserProfileRequest) returns (UserProfile);
  rpc TuneProfile(ProfileTuningRequest) returns (ProfileTuningResponse);
  rpc GetMyProfile(GetUserProfileRequest) returns (UserProfile); // 用户前台 self
}
```

### 7.2 api 网关
- `GET /api/v1/v2/reco/profile/me` → UserProfile（用户前台，self）
- `GET /api/v1/v2/admin/profile/:user_id` → GetUserProfileReq → UserProfile
- `POST /api/v1/v2/admin/profile/tune` → ProfileTuningReq → ProfileTuningResp

### 7.3 与前端对齐说明
- 现有 `OnboardingQuestionnairePayload`（user_id/username/interests/primary_purpose/preferred_article_types/preferred_article_length/preferred_style/backgrounds/difficulty_preference/excluded_contents/reading_time_slots/personalized_recommendation_types）→ 直接映射 `StaticProfile`。
- 现有 `OnboardingQuestionnaireResponse`（trace_id/status/user_id/period_bucket/memory_types/updated_at/warnings）→ 由 `UserProfile.update_time` + ext_info 承载。
- 时间画像可视化（长期/短期/周期/趋势 + 衰减曲线 + 淘汰池）→ 前端 `/dashboard/reco/profile` 消费 `TemporalProfile`。

---

## 8. eval.proto / eval.api

### 8.1 proto 定义

```proto
syntax = "proto3";
package eval;
option go_package = ".";

// 评估指标类型
enum EvalMetricType {
  EVAL_METRIC_TYPE_UNSPECIFIED = 0;
  RECALL_AT_K = 1;    // Recall@K
  NDCG_AT_K = 2;      // NDCG@K
  CTR = 3;            // 点击率
  CVR = 4;            // 转化率
  LLM_JUDGE = 5;      // LLM Judge
  RUBRIC = 6;         // Rubric 评分
}

// 评估用例（golden case）
message EvalCase {
  string case_id = 1;
  string name = 2;
  string channel = 3;                    // 所属频道
  string ip = 4;                         // 所属 IP
  string intent = 5;                     // 所属意图
  string query = 6;                      // 输入 query
  repeated string expected_article_ids = 7; // 期望文章ID（确定性指标）
  repeated string expected_tags = 8;     // 期望标签
  map<string, string> expected_metadata = 9; // 期望元数据
  Rubric rubric = 10;                    // LLM Judge rubric
  string annotator_id = 11;              // 标注人
  int64 create_time = 12;                // 创建时间（毫秒）
  int64 update_time = 13;                // 更新时间（毫秒）
  bool enabled = 14;
  map<string, string> ext_info = 15;
}

// Rubric（LLM Judge 评分标准）
message Rubric {
  string name = 1;
  repeated RubricCriterion criteria = 2; // 评分维度
  string judge_model = 3;                // 评判模型
  int64 version = 4;                     // 版本
  map<string, string> ext_info = 5;
}

message RubricCriterion {
  string name = 1;                       // 维度名（accuracy/authority/depth/...）
  double weight = 2;                     // 权重
  string description = 3;                // 评分描述
  string scale = 4;                      // 评分尺度（A~T 或 0~1）
  map<string, string> ext_info = 5;
}

// 评估指标
message EvalMetric {
  EvalMetricType type = 1;               // 指标类型
  int32 k = 2;                           // K 值（Recall@K/NDCG@K）
  double value = 3;                      // 指标值
  string unit = 4;                       // 单位
  int32 sample_count = 5;                // 样本数
  map<string, string> ext_info = 6;
}

// 行为漂移报告
message BehaviorDriftReport {
  string report_id = 1;
  int64 window_start = 2;                // 窗口起（毫秒）
  int64 window_end = 3;                  // 窗口止（毫秒）
  map<string, double> current_distribution = 4;  // 当前指标分布
  map<string, double> baseline_distribution = 5; // 历史基线分布
  map<string, double> drift_scores = 6;  // 各指标漂移分
  repeated string drifted_metrics = 7;   // 漂移指标列表
  double threshold = 8;                  // 漂移阈值
  bool drifted = 9;                      // 是否漂移
  int64 generated_at = 10;               // 生成时间（毫秒）
  map<string, string> ext_info = 11;
}

// 人工标注
message Annotation {
  string annotation_id = 1;
  string case_id = 2;                    // 关联用例
  string annotator_id = 3;               // 标注人
  string recommendation_id = 4;          // 关联推荐结果
  repeated string article_ids = 5;       // 标注的文章ID
  string label = 6;                      // 标签（relevant/irrelevant/partial）
  double score = 7;                      // 标注分
  string comment = 8;                    // 备注
  int64 create_time = 9;                 // 标注时间（毫秒）
  map<string, string> ext_info = 10;
}

// 评估运行请求
message EvalRunRequest {
  repeated string case_ids = 1;          // 指定用例（空则全量）
  optional string channel = 2;           // 按频道筛选
  optional string ip = 3;                // 按 IP 筛选
  optional string intent = 4;            // 按意图筛选
  repeated EvalMetricType metrics = 5;   // 指标
  int32 k = 6;                           // K 值
  string baseline_version = 7;           // 对比基线版本
  bool detect_drift = 8;                 // 是否检测漂移
  string judge_model = 9;                // LLM Judge 模型
  map<string, string> ext_info = 10;
}

// 评估运行响应
message EvalRunResponse {
  string run_id = 1;
  repeated EvalMetric metrics = 2;       // 指标结果
  repeated EvalCaseResult case_results = 3; // 每用例结果
  BehaviorDriftReport drift_report = 4;  // 漂移报告（detect_drift=true）
  int64 run_at = 5;                      // 运行时间（毫秒）
  string err_msg = 6;
  map<string, string> ext_info = 7;
}

message EvalCaseResult {
  string case_id = 1;
  repeated string actual_article_ids = 2; // 实际推荐
  repeated EvalMetric metrics = 3;       // 用例级指标
  bool passed = 4;                       // 是否通过
  string rationale = 5;                  // LLM Judge 理由
}

message ListEvalCasesRequest {
  optional string channel = 1;
  optional string ip = 2;
  optional string intent = 3;
  bool enabled_only = 4;
  int32 page = 5;
  int32 page_size = 6;
}

message ListEvalCasesResponse {
  repeated EvalCase cases = 1;
  int64 total = 2;
  int32 page = 3;
  int32 page_size = 4;
}

message SubmitAnnotationRequest {
  Annotation annotation = 1;
}

message SubmitAnnotationResponse {
  string annotation_id = 1;
  bool success = 2;
}

service EvalService {
  rpc ListCases(ListEvalCasesRequest) returns (ListEvalCasesResponse);
  rpc UpsertCase(EvalCase) returns (EvalCase);
  rpc DeleteCase(EvalCase) returns (SubmitAnnotationResponse);
  rpc Run(EvalRunRequest) returns (EvalRunResponse);
  rpc SubmitAnnotation(SubmitAnnotationRequest) returns (SubmitAnnotationResponse);
  rpc GetDriftReport(GetDriftReportRequest) returns (BehaviorDriftReport);
}

message GetDriftReportRequest {
  optional int64 window_start = 1;
  optional int64 window_end = 2;
}
```

### 8.2 api 网关
- `GET /api/v1/v2/admin/eval/cases` → ListEvalCasesReq → ListEvalCasesResp
- `POST /api/v1/v2/admin/eval/cases` → EvalCase → EvalCase（upsert）
- `DELETE /api/v1/v2/admin/eval/cases/:case_id` → SubmitAnnotationResp
- `POST /api/v1/v2/admin/eval/run` → EvalRunReq → EvalRunResp
- `GET /api/v1/v2/admin/eval/reports/drift` → GetDriftReportReq → BehaviorDriftReport
- `POST /api/v1/v2/admin/eval/annotate` → SubmitAnnotationReq → SubmitAnnotationResp

### 8.3 与前端对齐说明
- 新增 `EvalCase`/`EvalRunRequest`/`EvalMetric`/`Rubric`/`BehaviorDriftReport`/`Annotation` TS 类型到 `src/services/admin-reco.ts`。
- 现有 `RecoEvaluationSummary`（surface/window/metric_values）→ 由 `EvalRunResponse` + `BehaviorDriftReport` 替代，保留旧接口向后兼容。
- `EvalMetricType` 枚举用于管理员"评估"面板指标选择。

---

## 9. cpu.proto / cpu.api

### 9.1 proto 定义

```proto
syntax = "proto3";
package cpu;
option go_package = ".";

// CPU profile 快照
message CPUProfile {
  int64 captured_at = 1;                 // 采样时间（毫秒）
  int32 goroutines = 2;                  // goroutine 数
  int32 pool_size = 3;                   // ants 池大小
  int32 pool_active = 4;                 // ants 池活跃数
  int32 pprof_samples = 5;               // pprof 采样数
  double cpu_usage = 6;                  // CPU 利用率
  int64 gc_pause_ns = 7;                 // GC 暂停（纳秒）
  int32 gc_count = 8;                    // GC 次数
  repeated PoolStat pools = 9;           // 各池统计
  repeated CacheStat caches = 10;        // 各缓存统计
  GOMAXPROCSConfig gomaxprocs = 11;      // GOMAXPROCS 配置
  string flame_graph = 12;               // 火焰图 SVG/JSON
  map<string, string> ext_info = 13;
}

// pprof 请求
message PProfRequest {
  string profile_type = 1;               // profile 类型（cpu/heap/goroutine/block/mutex）
  int32 duration_sec = 2;                // 采样时长（秒）
  bool flame_graph = 3;                  // 是否返回火焰图
  int32 sample_rate = 4;                 // 采样频率
  map<string, string> ext_info = 5;
}

message PProfResponse {
  bytes profile_data = 1;                // pprof 原始数据
  string flame_graph = 2;                // 火焰图（SVG/JSON）
  int64 captured_at = 3;
  string err_msg = 4;
}

// 池统计
message PoolStat {
  string name = 1;                       // 池名（recall/rerank/cf/graph/...）
  int32 size = 2;                        // 当前大小
  int32 active = 3;                      // 活跃数
  int32 waiting = 4;                     // 等待数
  int32 capacity = 5;                    // 容量
  double utilization = 6;                // 利用率
  int64 total_processed = 7;             // 累计处理数
  map<string, string> ext_info = 8;
}

// GOMAXPROCS 配置
message GOMAXPROCSConfig {
  int32 current = 1;                     // 当前 GOMAXPROCS
  int32 quota = 2;                       // CPU 配额（automaxprocs 读取）
  bool automaxprocs_enabled = 3;         // 是否启用 automaxprocs
  int32 recommended = 4;                 // 推荐值
  map<string, string> ext_info = 5;
}

// 缓存统计
message CacheStat {
  string name = 1;                       // 缓存名（L1_article_meta/L2_recall/L3_prompt）
  string level = 2;                      // L1/L2/L3
  int64 size = 3;                        // 当前条目数
  int64 capacity = 4;                    // 容量
  int64 hits = 5;                        // 命中数
  int64 misses = 6;                      // 未命中数
  double hit_ratio = 7;                  // 命中率
  int64 evictions = 8;                   // 淘汰数
  int64 latency_us = 9;                  // 平均延迟（微秒）
  map<string, string> ext_info = 10;
}

message GetCPUProfileRequest {
  bool include_flame_graph = 1;
}

message ListPoolsRequest {}
message ListPoolsResponse {
  repeated PoolStat pools = 1;
}

message ListCachesRequest {}
message ListCachesResponse {
  repeated CacheStat caches = 1;
}

message GetGOMAXPROCSRequest {}

service CPUService {
  rpc GetProfile(GetCPUProfileRequest) returns (CPUProfile);
  rpc PProf(PProfRequest) returns (PProfResponse);
  rpc ListPools(ListPoolsRequest) returns (ListPoolsResponse);
  rpc ListCaches(ListCachesRequest) returns (ListCachesResponse);
  rpc GetGOMAXPROCS(GetGOMAXPROCSRequest) returns (GOMAXPROCSConfig);
  rpc UpdateGOMAXPROCS(GOMAXPROCSConfig) returns (GOMAXPROCSConfig);
}
```

### 9.2 api 网关
- `GET /api/v1/v2/admin/cpu/profile` → CPUProfile
- `POST /api/v1/v2/admin/cpu/pprof` → PProfReq → PProfResp
- `GET /api/v1/v2/admin/cpu/pools` → ListPoolsResp
- `GET /api/v1/v2/admin/cpu/cache` → ListCachesResp
- `GET /api/v1/v2/admin/cpu/maxprocs` → GOMAXPROCSConfig
- `PUT /api/v1/v2/admin/cpu/maxprocs` → GOMAXPROCSConfig → GOMAXPROCSConfig

### 9.3 与前端对齐说明
- 新增 `CPUProfile`/`PProfRequest`/`PoolStat`/`GOMAXPROCSConfig`/`CacheStat` TS 类型到 `src/services/admin-reco.ts`。
- `flame_graph` 用于管理员"CPU 监控"面板渲染火焰图。
- `PoolStat.utilization` / `CacheStat.hit_ratio` 用于水位与命中率仪表盘。

---

## 10. graph.proto / graph.api

### 10.1 proto 定义

```proto
syntax = "proto3";
package graph;
option go_package = ".";

// 图谱节点类型（12 类，与 schema.go 一致）
enum GraphNodeType {
  GRAPH_NODE_TYPE_UNSPECIFIED = 0;
  ARTICLE = 1;
  TAG = 2;
  TYPE_TAG = 3;
  AUTHOR = 4;
  IP = 5;
  CHANNEL = 6;
  TOPIC = 7;
  ENTITY = 8;
  IMAGE = 9;
  USER = 10;
  BEHAVIOR = 11;
  QUALITY_REPORT = 12;
}

// 图谱节点
message GraphNode {
  string id = 1;                         // 节点ID
  GraphNodeType type = 2;                // 节点类型
  string name = 3;                       // 节点名
  map<string, string> properties = 4;    // 节点属性
  repeated string labels = 5;            // 标签
  map<string, string> ext_info = 6;
}

// 图谱边
message GraphEdge {
  string source_id = 1;                  // 起点ID
  string target_id = 2;                  // 终点ID
  string relation = 3;                   // 关系类型（HAS_TAG/WROTE_BY/LIKED/CO_OCCURRED_WITH/VISUALLY_SIMILAR/...）
  GraphNodeType source_type = 4;
  GraphNodeType target_type = 5;
  map<string, string> properties = 6;    // 边属性（如 count/weight）
  map<string, string> ext_info = 7;
}

// Cypher 执行请求
message CypherRequest {
  string cypher = 1;                     // Cypher 语句（参数化）
  map<string, string> params = 2;        // 参数
  int32 limit = 3;                       // 结果上限
  bool read_only = 4;                    // 只读（默认 true）
  map<string, string> ext_info = 5;
}

message CypherResponse {
  repeated GraphNode nodes = 1;
  repeated GraphEdge edges = 2;
  repeated map<string, string> rows = 3; // 原始行结果
  int64 latency_ms = 4;
  string err_msg = 5;
  map<string, string> ext_info = 6;
}

// 图谱查询请求（LLM 生成 Cypher）
message GraphQueryRequest {
  string trace_id = 1;
  string user_id = 2;
  string query = 3;                      // 自然语言查询
  bool generate_cypher = 4;              // 是否 LLM 生成 Cypher
  int32 hop_limit = 5;                   // 多跳上限
  int32 top_k = 6;
  map<string, string> ext_info = 7;
}

message GraphQueryResponse {
  string cypher = 1;                     // 生成的 Cypher
  repeated GraphNode nodes = 2;
  repeated GraphEdge edges = 3;
  string subgraph = 4;                   // 子图 JSON（前端可视化）
  int64 latency_ms = 5;
  string err_msg = 6;
  map<string, string> ext_info = 7;
}

// 图谱召回请求（多跳召回候选文章）
message GraphRecallRequest {
  string user_id = 1;
  string channel = 2;                    // 频道
  repeated string seed_article_ids = 3;  // 种子文章
  repeated string seed_entities = 4;     // 种子实体
  string template = 5;                   // 召回模板（user_liked_similar/user_similar_user/co_occur/entity/article_author/ip/...）
  int32 top_k = 6;
  int32 hop_limit = 7;
  map<string, string> ext_info = 8;
}

message GraphRecallResponse {
  repeated GraphRecallCandidate candidates = 1;
  string cypher = 2;                     // 执行的 Cypher
  int64 latency_ms = 3;
  map<string, string> ext_info = 4;
}

message GraphRecallCandidate {
  string article_id = 1;
  double score = 2;
  string path = 3;                       // 召回路径（JSON，如 user→liked→article→similar→article）
  map<string, string> ext_info = 4;
}

// 实体链接请求
message EntityLinkRequest {
  string query = 1;                      // 输入文本
  repeated GraphNodeType candidate_types = 2; // 候选实体类型
  int32 top_k = 3;
  double threshold = 4;                  // 相似度阈值
  map<string, string> ext_info = 5;
}

message EntityLinkResponse {
  repeated LinkedEntity entities = 1;
  map<string, string> ext_info = 2;
}

message LinkedEntity {
  string entity_id = 1;
  GraphNodeType type = 2;
  string name = 3;
  double similarity = 4;
  repeated string aliases = 5;
}

// 图片搜索请求（以图搜文 / 以图搜图）
message ImageSearchRequest {
  string image_url = 1;                  // 图片URL
  bytes image_bytes = 2;                 // 图片字节（二选一）
  string search_type = 3;                // image_to_article / image_to_image
  int32 top_k = 4;
  double threshold = 5;
  map<string, string> ext_info = 6;
}

message ImageSearchResponse {
  repeated ImageSearchHit hits = 1;
  int64 latency_ms = 2;
  string err_msg = 3;
  map<string, string> ext_info = 4;
}

message ImageSearchHit {
  string article_id = 1;                 // 关联文章ID（以图搜文）
  string image_id = 2;                   // 图片节点ID
  string image_url = 3;                  // 图片URL
  double similarity = 4;
  map<string, string> ext_info = 5;
}

// 图谱 Schema 响应（12 类节点 + 关系定义）
message GraphSchemaResponse {
  repeated NodeTypeSchema node_types = 1;     // 12 类节点
  repeated RelationSchema relations = 2;      // 关系定义
  repeated IndexSchema indexes = 3;           // 索引
  repeated ConstraintSchema constraints = 4;  // 约束
  int64 version = 5;
  map<string, string> ext_info = 6;
}

message NodeTypeSchema {
  GraphNodeType type = 1;
  string name = 2;
  repeated string properties = 3;        // 属性列表
  repeated string indexes = 4;           // 索引字段（user_id/channel/ip/author/time）
  string description = 5;
}

message RelationSchema {
  string relation = 1;                   // 关系名
  GraphNodeType source_type = 2;
  GraphNodeType target_type = 3;
  repeated string properties = 4;
  string description = 5;
}

message IndexSchema {
  GraphNodeType node_type = 1;
  repeated string fields = 2;
  bool unique = 3;
}

message ConstraintSchema {
  GraphNodeType node_type = 1;
  string field = 2;
  string constraint_type = 3;            // uniqueness/existence
}

message GetSchemaRequest {}

service GraphService {
  rpc ExecuteCypher(CypherRequest) returns (CypherResponse);
  rpc Query(GraphQueryRequest) returns (GraphQueryResponse);
  rpc Recall(GraphRecallRequest) returns (GraphRecallResponse);
  rpc EntityLink(EntityLinkRequest) returns (EntityLinkResponse);
  rpc ImageSearch(ImageSearchRequest) returns (ImageSearchResponse);
  rpc GetSchema(GetSchemaRequest) returns (GraphSchemaResponse);
}
```

### 10.2 api 网关
- `POST /api/v1/v2/admin/graph/query` → GraphQueryReq → GraphQueryResp
- `POST /api/v1/v2/admin/graph/cypher` → CypherReq → CypherResp（直接执行 Cypher，管理员调试）
- `POST /api/v1/v2/admin/graph/recall` → GraphRecallReq → GraphRecallResp
- `POST /api/v1/v2/admin/graph/entity_link` → EntityLinkReq → EntityLinkResp
- `POST /api/v1/v2/admin/graph/image_search` → ImageSearchReq → ImageSearchResp
- `GET /api/v1/v2/admin/graph/schema` → GraphSchemaResponse

### 10.3 与前端对齐说明
- 新增 `GraphNode`/`GraphEdge`/`GraphSchemaResponse` TS 类型，用于 `/dashboard/reco/graph` 与 `/admin/reco/graph` 可视化（D3/Cytoscape）。
- `GraphNodeType` 12 类枚举与图谱 schema.go 节点约束一一对应。
- `subgraph` 字段（JSON）供前端图谱组件直接渲染节点关系图。
- 以图搜文端点 `/api/v1/v2/reco/search/image` 复用 `search.proto`，此处 `/admin/graph/image_search` 为管理员测试入口。

---

## 11. skill.proto / skill.api

### 11.1 proto 定义

```proto
syntax = "proto3";
package skill;
option go_package = ".";

// Skill 类别（12 类）
enum SkillCategory {
  SKILL_CATEGORY_UNSPECIFIED = 0;
  RECALL = 1;        // 召回类
  RANK = 2;          // 排序类
  QUALITY = 3;       // 质量类
  SEARCH = 4;        // 搜索类
  GRAPH = 5;         // 图谱类
  PROFILE = 6;       // 画像类
  CHANNEL = 7;       // 频道类
  EVENT = 8;         // 事件类
  EXPLAIN = 9;       // 解释类
  EVAL = 10;         // 评估类
  TOOL = 11;         // 工具类
  OBS = 12;          // 观测类
}

// Skill 清单
message Skill {
  string name = 1;                       // 唯一名（如 recall.content）
  SkillCategory category = 2;            // 类别
  string display_name = 3;               // 展示名
  string description = 4;                // 描述
  SkillManifest manifest = 5;            // SKILL.md 清单
  repeated string tools = 6;             // 绑定工具
  repeated string resources = 7;         // 绑定资源
  bool enabled = 8;                      // 是否启用
  bool builtin = 9;                      // 是否内置（false 表示业务二开）
  int64 create_time = 10;                // 创建时间（毫秒）
  map<string, string> ext_info = 11;
}

// Skill 清单（SKILL.md 内容）
message SkillManifest {
  string name = 1;                       // 名称
  string description = 2;                // 描述
  string trigger_condition = 3;          // 触发条件
  string usage = 4;                      // 使用说明
  string version = 5;                    // 版本
  string author = 6;                     // 作者
  map<string, string> ext_info = 7;
}

// 注册 Skill 请求（二开点：业务方放置技能目录）
message RegisterSkillRequest {
  string name = 1;
  SkillCategory category = 2;
  string display_name = 3;
  string description = 4;
  SkillManifest manifest = 5;
  repeated string tools = 6;
  repeated string resources = 7;
  bool enabled = 8;
  string skill_dir = 9;                  // 技能目录路径（扫描加载）
  map<string, string> ext_info = 10;
}

message RegisterSkillResponse {
  string name = 1;
  bool success = 2;
  string err_msg = 3;
}

message ListSkillsRequest {
  optional SkillCategory category = 1;
  bool enabled_only = 2;
  bool builtin_only = 3;                 // 仅内置
  bool custom_only = 4;                  // 仅二开
}

message ListSkillsResponse {
  repeated Skill skills = 1;
  int32 total = 2;
  map<string, int32> category_counts = 3; // 各类 Skill 计数
}

// 调用 Skill 请求
message InvokeSkillRequest {
  string name = 1;                       // Skill 名
  string input = 2;                      // 输入（JSON）
  string trace_id = 3;
  bool materialize_to_tool_result = 4;   // 是否物化到 tool result（不污染 Prompt Cache 前缀）
  int32 timeout_sec = 5;
  map<string, string> ext_info = 6;
}

message InvokeSkillResponse {
  string output = 1;                     // 输出（JSON）
  bool success = 2;
  int64 latency_ms = 3;
  string err_msg = 4;
  map<string, string> ext_info = 5;
}

message GetSkillRequest {
  string name = 1;
}

service SkillService {
  rpc ListSkills(ListSkillsRequest) returns (ListSkillsResponse);
  rpc GetSkill(GetSkillRequest) returns (Skill);
  rpc RegisterSkill(RegisterSkillRequest) returns (RegisterSkillResponse);
  rpc InvokeSkill(InvokeSkillRequest) returns (InvokeSkillResponse);
  rpc EnableSkill(EnableSkillRequest) returns (RegisterSkillResponse);
}

message EnableSkillRequest {
  string name = 1;
  bool enabled = 2;
}
```

### 11.2 api 网关
- `GET /api/v1/v2/admin/skill/list` → ListSkillsReq → ListSkillsResp
- `GET /api/v1/v2/admin/skill/:name` → Skill
- `POST /api/v1/v2/admin/skill/register` → RegisterSkillReq → RegisterSkillResp
- `POST /api/v1/v2/admin/skill/invoke` → InvokeSkillReq → InvokeSkillResp
- `PUT /api/v1/v2/admin/skill/:name/enable` → EnableSkillReq → RegisterSkillResp

### 11.3 与前端对齐说明
- 新增 `Skill`/`SkillManifest`/`InvokeSkillRequest`/`InvokeSkillResponse` TS 类型到 `src/services/admin-reco.ts`。
- `SkillCategory` 12 类枚举用于管理员"Skill 管理"面板分类展示。
- `category_counts` 用于前端展示 12 类 Skill 数量分布。
- `builtin` 字段区分内置与二开 Skill。

### 11.4 Skills 完整清单（50+，分 12 类，与 spec.md 一致）

| 类别 | Skill 名 | 说明 |
|------|----------|------|
| 召回 RECALL | `recall.content` | 内容召回（向量 + BM25） |
| | `recall.rule` | 规则召回（热点/最新/编辑） |
| | `recall.cf` | 协同过滤召回（User-CF/Item-CF/MF） |
| | `recall.graph` | 图谱召回（Neo4j 多跳） |
| | `recall.channel` | 频道独立池召回 |
| | `recall.hybrid` | 混合召回编排 |
| 排序 RANK | `rank.traditional` | 传统排序（LR/GBDT/加权） |
| | `rerank.self` | 自研 rerank（Cross-encoder + Two-tower + LambdaMART） |
| | `rerank.external` | 外部 rerank（DashScope 兜底） |
| | `rerank.llm` | LLM rerank（大模型精排） |
| | `rerank.ab_test` | A/B 测试切换 |
| 质量 QUALITY | `quality.score` | 文章多维质量评分 |
| | `quality.batch_score` | 批量质量评分 |
| | `quality.judge` | LLM Verifier Best-of-N 评判 |
| | `quality.feedback` | 质量反馈闭环 |
| | `quality.report` | 质量报告生成 |
| 搜索 SEARCH | `search.hybrid` | 语义混合搜索 |
| | `search.understand` | 查询理解（intent/实体/时效） |
| | `search.rewrite` | 查询重写（同义/拼改/相关） |
| | `search.personalize` | 个性化搜索 |
| | `search.filter` | 搜索过滤 |
| | `search.suggest` | 搜索补全 |
| | `search.history` | 搜索历史与反馈 |
| | `search.graph` | 图谱知识搜索 |
| 图谱 GRAPH | `graph.query` | Cypher 查询执行 |
| | `graph.generate_cypher` | LLM 生成 Cypher |
| | `graph.recall` | 图谱多跳召回 |
| | `graph.entity_link` | 实体链接 |
| | `graph.image_search` | 图片关联搜索 |
| | `graph.similar` | 相似实体查询 |
| 画像 PROFILE | `profile.load` | 用户画像加载 |
| | `profile.update` | 画像增量更新 |
| | `profile.temporal` | 时间画像管理（衰减/强化/淘汰） |
| | `profile.summary` | Session Summary 画像压缩 |
| | `profile.tune` | 画像参数调优 |
| 频道 CHANNEL | `channel.list` | 频道列表 |
| | `channel.register` | 频道注册（二开点） |
| | `channel.get_pool` | 频道独立池 |
| | `channel.ensure_pool` | 频道池维护 |
| | `channel.route` | 频道路由 |
| 事件 EVENT | `event.report` | 行为事件上报 |
| | `event.feedback` | 反馈事件 |
| | `event.hook_register` | Hook 注册（二开点） |
| 解释 EXPLAIN | `explain.recommend` | 推荐解释生成 |
| | `explain.search` | 搜索解释生成 |
| 评估 EVAL | `eval.run` | 评估集运行 |
| | `eval.regression` | 回归测试 |
| | `eval.drift` | 行为漂移检测 |
| | `eval.annotate` | 人工标注闭环 |
| 工具 TOOL | `tool.milvus_search` | Milvus 向量搜索 |
| | `tool.pool_manage` | 候选池管理 |
| | `tool.memory_manage` | 记忆管理 |
| | `tool.user_history` | 用户历史 |
| | `tool.doc_ingest` | 文档摄入 |
| | `tool.session_search` | Session 事实恢复 |
| 观测 OBS | `obs.cost` | 成本统计 |
| | `obs.trace` | 链路追踪 |
| | `obs.metric` | 指标暴露 |
| | `obs.pprof` | CPU profiling |

合计：召回 6 + 排序 5 + 质量 5 + 搜索 8 + 图谱 6 + 画像 5 + 频道 5 + 事件 3 + 解释 2 + 评估 4 + 工具 6 + 观测 4 = **59 个 Skill**。

---

## 跨契约一致性约定

### 公共字段命名统一
- `article_id` / `user_id` / `session_id` / `trace_id` / `channel` 在所有 proto 中统一 snake_case，api JSON tag 同 snake_case，TS 同 snake_case。
- `rec_request_id` / `search_request_id` 保持现有前端命名，不改为 `requestId`。
- 时间戳统一 `int64` 毫秒（与 `article.proto` 的 `create_time`/`update_time` 一致）。

### 枚举值命名统一
- 所有枚举 0 值为 `*_UNSPECIFIED`（与 `article.proto` 的 `ARTICLE_STATUS_UNSPECIFIED` 一致）。
- 枚举名使用大写下划线（`PATH_MODE_FAST` / `RERANK_MODEL_TYPE_SELF`）。
- proto enum 值在不同 package 内允许重名（`reco.PATH_TAKEN_FAST` vs `reco.PATH_MODE_FAST`），通过 package 前缀消歧。

### 时间戳统一
- 所有时间字段：`int64` + 毫秒时间戳；字段名语义后缀 `_time`/`_at`。
- 延迟字段：`int64` + 毫秒，后缀 `_ms`（如 `latency_ms`）；纳秒仅限 GC 内部，后缀 `_ns`。

### map 扩展字段统一
- 所有 message 末尾统一 `map<string, string> ext_info`（与 `article.proto` 的 `Article.ext_info` 一致）。
- ext_info 仅用于向前兼容扩展，不承载核心业务字段。
- TS 侧用 `[key: string]: unknown` 或 `Record<string, unknown>` 承载（与现有 `reco.ts` 一致）。

### 命名风格对照表

| 层 | 字段命名 | 结构体命名 | 备注 |
|----|----------|------------|------|
| proto | snake_case | PascalCase | `option go_package = "."` |
| api (go-zero) | snake_case（json tag） | PascalCase（Go 字段） | `,optional`/`,default=` |
| TS | snake_case | PascalCase（interface 名） | 可选 `?` |

### 路由前缀约定
- 用户前台：`/api/v1/v2/reco/...`（spec 明确，与现有 `RECO_API_PATHS` 兼容）。
- 管理员：`/api/v1/v2/admin/...`（按模块细分 `/admin/channels`/`/admin/quality`/...）。
- reco 网关由 `Sea-RideTheWind` 拥有，前端不直连 reco。

### 向后兼容承诺
- 本草案签字后，Phase 1+ 实现不得破坏已定义字段（仅允许新增字段或新增可选字段）。
- 新增字段必须 `optional` 或带默认值，且 ext_info 优先承载实验性字段。
- 枚举仅允许追加新值，不得删除或重排已有值。

### 待澄清问题（待三方讨论）
1. `RecommendResponse.article_ids` 是否需要返回完整 `Article` 列表，还是仅 ID 由前端二次拉取？（现有 reco.ts 倾向 ID + 二次拉取）
2. `RecommendConfig` 字段过多（17 个），是否拆为 `RecallConfig`/`RerankConfig`/`QualityConfig` 子结构？（spec 列为单结构，暂按单结构起草）
3. `search.image_url` 与 `ImageSearchRequest` 是否合并？（当前 search 用 image_url 触发，graph 用独立请求，二者并存）
4. `eval` 是否需要支持在线 A/B 配置端点？（spec 提到 A/B 但 Task 1.8 未列 ABTestConfig，建议补充到 eval 或 rerank）
5. `skill` 清单是否需要在 `ListSkillsResponse` 中暴露每个 Skill 的调用次数统计？（便于管理员观测 Skill 使用分布）

---

## 附录：11 契约与 spec/tasks 对照

| # | proto/api | Task | spec 关键 message | 本草案 message 数 |
|---|-----------|------|-------------------|-------------------|
| 1 | reco | 1.1 | RecommendRequest/RecommendResponse/RecommendConfig/TagFilter/CostReport | 5 + 2 enum + service |
| 2 | search | 1.2 | SearchQuery/SearchResult/SearchHit/SearchIntent/RewrittenQuery/SearchFilter/SearchSuggestion/GraphKnowledge | 9 + 1 enum + service |
| 3 | channel | 1.3 | Channel/ChannelConfig/RegisterChannelRequest/ListChannelsResponse | 8 + service |
| 4 | quality | 1.4 | ArticleQuality(6维)/ArticleQualityReport/QualityFeedback/QualityEvaluateRequest/Response | 11 + service |
| 5 | rerank | 1.5 | RerankRequest/Response/RerankModel/RerankFeatures/ABTestConfig/ModelVersion | 12 + 1 enum + service |
| 6 | cf | 1.6 | CFScore/CFSimilarUsersRequest/CFSimilarItemsRequest/CFConfig/ColdStartConfig | 11 + 1 enum + service |
| 7 | profile | 1.7 | UserProfile(三层)/TemporalProfile/LongTermInterests/ShortTermInterests/PeriodicPattern/TrendingInterests/DecayState/EliminationPool/ProfileTuningRequest | 16 + service |
| 8 | eval | 1.8 | EvalCase/EvalRunRequest/Response/EvalMetric/Rubric/BehaviorDriftReport/Annotation | 13 + 1 enum + service |
| 9 | cpu | 1.9 | CPUProfile/PProfRequest/PoolStat/GOMAXPROCSConfig/CacheStat | 10 + service |
| 10 | graph | 1.10 | GraphNode/GraphEdge/CypherRequest/Response/GraphQueryRequest/GraphRecallRequest/EntityLinkRequest/ImageSearchRequest/GraphSchemaResponse(12类节点) | 17 + 1 enum + service |
| 11 | skill | 1.11 | Skill/SkillManifest/RegisterSkillRequest/ListSkillsResponse/InvokeSkillRequest/Response(50+ Skills 清单分12类) | 9 + 1 enum + service + 59 skill 清单 |

**Spec 完备性核查结论**：spec.md 与 tasks.md 对 11 契约的 message 列举完整，字段语义清晰。本草案在 spec 基础上补充了：每个 message 的具体字段编号/类型/注释（spec 仅给出 message 名与关键字段，未给完整字段表）、api 网关路由细节、与前端 reco.ts 的对齐映射。5 个待澄清问题需三方在评审会上决议。
