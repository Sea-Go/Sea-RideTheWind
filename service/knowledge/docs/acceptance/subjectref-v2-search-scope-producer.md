# RTW 主体 v2 搜索范围签发候选与交接

状态：**本机受控候选，Stage4 及全工程仍 PARTIAL**。提供方为 RTW
`feat/rtw-subjectref-v2-producer-20260915` 业务固定头
`27013272e7149d7ff1eb4f9f8b43549e2d81b46d`，消费者必须从已部署且签收的 BTW
Summary/Tools 双读版本开始；本机联验固定消费者集成
`b72dcd644575537d26da64ac646a8c90a372033b`，核心模块
`trpc-agent-go v1.8.1`。这页是 knowledge 搜索两个 HMAC 通道的 owner
交接，article、favorite、comment、like 各自的新 v2 EventSchema 仍由其事实
owner 另行签发。

## 工作区和责任

- `[W0]` RTW 任务隔离 Git worktree；`[W1]`
  `service/user/user/identity/`、`service/knowledge/api/internal/config/`、
  `model/`、`svc/`、`logic/product/`、知识测试、候选 SQL 与本页。
- `[R1]` Sea-Docs 六阶段迁移、BTW 当前双读消费者和其它客户端；
  `[D1]` 锁定 Go 模块；`[G1]` goctl 生成 API DTO，不手改；
  `[X1]` 每次重新开的回环 PG16 与真 UserCenter/BTW 测试进程；
  `[N1]` RTW 原树、BTW、DC、Web、WhaleHall、生产；
  `[T1]` 本机私有日志和 PG 测试证据，不提交凭据或原文。
- 主职责 `[C7:CONTRACT]` 是 RTW identity 与 v2 搜索 wire；
  跨 `[C4:PERSISTENCE]` 的幂等操作版本槽、`[C2:APPLICATION]`
  的通道装配及 `[C8:VERIFY]` 的两仓 HTTP 接纳。

## 签发与版本不变量

```text
UserCenter 真登录 JWT
  -> go-zero JWT 已验 userId
  -> User RPC GetUser 正 int64 UID / 同 UID / status=active
  -> RTW v2 {issuer:"rtw.identity",subject_id:"<规范十进制 UID>"}
  -> 旧 PG v1 锚 rtw.identity/platform/<UID> + 本机 Stage3 v2 sidecar
  -> 新操作在原 Reserve 事务 scope_version=v1|v2 固定一次
  -> Summary aud=btw.search.summary.v2 / Tools aud=btw.search.tools.v2
  -> BTW 严格 HMAC/字节形状校验 -> 原生 Root/Tool Graph
  -> RTW 同 SearchID/AnswerID 回读固定答案与耐久引用
```

`platform` 只是旧 DB/旧 v1 wire 的固定兼容槽，v2 两字段 wire 不含
`tenant_id`、`authority_id` 或组织。请求体、`X-User-ID`、桌宠/DC UUID
都不能签发这个主体。实时 User RPC 的缺失、错 UID、旧协议没有 status、
inactive、非法/非规范十进制 UID 在调用 BTW 前拒绝。RTW v2 signer
只对从本机 typed struct 生成的有序 `json.Marshal` 字节做 HMAC；
不同 audience 对应独立 typed Scope 类型，旧 v1 signer、旧 audience、
旧 Summary/Tools 请求体顺序和旧验签字节不改。BTW 对收到的原字节做
HMAC、严格 JSON decoder、重新编码字节相等及领域校验；重复/未知键、
额外 `tenant_id`、错误 issuer、跨 audience、坏 UID 先于 Source/Agent 拒。

`knowledge_product_search_operations` 与 `knowledge_tool_parents` 的
`scope_version` 是新列，`DEFAULT 'v1'` 与 validated 的仅 v1/v2 CHECK
属于候选 expand DDL。新操作在旧原始 Reserve/sidecar 同一个事务中写
唯一版本；同键重试总读取原行版本。把新请求开关切回 v1只影响未来新
键，已存在 v2 操作不能降格成 v1 或再产生第二行/第二个语义事件。
`DetectSearchScopeVersions` 核实际两表、text、NOT NULL、DEFAULT v1、
唯一列 CHECK 的精确 PG 表达式。双表都没有该列时只走旧 v1；半扩展、
弱同名 CHECK、默认漂移或错误类型拒绝。候选 SQL 在任何 ALTER 前
执行相同已存在目录核验，重复应用不能把坏目录当作完成。

正式配置的 `SearchSummary.ScopeVersion` 和
`SearchTools.ScopeVersion` 缺省均为 `v1`，
`SubjectRefV2Writes.Enabled` 缺省 `false`。
显式 `v2` 必须同时启用已有 Stage3 连续写开关、独立本机 PG owner
预置的 64 hex nonce/一小时 marker、四张 sidecar 的完整约束目录，
及本候选 scope-version 列；`Mode=pro` 在装配前拒绝。旧 pending
Outbox 保持原 schema=1 payload/hash，旧 `turn_json/turn_hash`
与旧 Audience 均不回写。Knowledge 现有产品外部 HTTP 路由没有
因这个签名候选换版本；已有 `/v2/knowledge` 历史 GET 仍是只读投影。

## 本机验收与结果边界

测试入口 `scripts/test-subjectref-v2-producer.sh` 使用独立
回环 PG16、真实编译的 User RPC/API、真实注册登录取得的两枚 JWT，
再启动 Knowledge HTTP；设置
`SEA_BTW_PRODUCT_SEARCH_ROOT`、`SEA_BTW_TOOLS_CONSUMER_ROOT`、
`SEA_BTW_CITATION_CONSUMER_ROOT` 为同一固定 BTW checkout 时，
启动 BTW 独立双读测试服务，原生
`invoke_agent search_summary_root` 与
`invoke_agent search_tools_root` 检查归属于 `trpc.agent.go`
InstrumentationScope。模型是固定本机夹具；真实 LLM 质量、线上
Collector 和桌宠产品会话不由本测试签收。
固定的 `productUserRPC`、HMAC 单测与 HTTP relay 仅验证协议反例；
它们不是此处两枚 JWT 的发行者，也不替代真 UserCenter 登录证据。

PG/race 合并测试 `TestContinuousSubjectRefV2AnswerReplayAndRollback`、
`TestSearchScopeVersionCatalogRejectsWeakDDLBeforeSigner` 与
`TestV2SearchScopePinsOneWirePerOperation` 顶层 exit=0：
原 v1 `turn_json/turn_hash`、Outbox 原指纹未改变；坏默认、弱同名
CHECK、varchar、可空列、半扩展各自先拒再修；两 UID 使用同一
session/key 不串人，同一旧键切 v2 后仍签 v1，新 v2 键切回 v1
后仍签 v2，Tools 父操作同理。独立 PG 结束有
`pg_ctl stop` 和 `status=3`。
BTW 当前消费者二项严格 scope HTTP/race 测试顶层 exit=0，
覆盖 v1/v2 HMAC、签名跨版本、重复/未知键、额外身份字段和错 UID
在 Source/Agent 前拒绝。

固定头证据：

| 切片 | 结果和可复核证据 |
| --- | --- |
| RTW `2701327` × 冻结 BTW `b72dcd6` 的显式 v2 | `scripts/test-subjectref-v2-producer.sh` 顶层 exit=0；真 UserCenter 注册/登录两 UID、知识 HTTP、BTW Summary/Tools 独立进程；空证据与固定原文引用各到 RTW 同 ID 接纳，主测试 82.49 秒 PASS。父日志 `/private/tmp/sea-rtw-v2-producer-fixed-2701327-b72-l3.log` SHA256 `2137b00b4bf7a6df68bd8d0f3c6c985bfd9745ed660cc9ff3e9b597b9f0e0aaf`；本轮私有 evidence `sea-knowledge-acceptance.xLfO7W`，PG stop 且 `pg_ctl status` 退出 3。 |
| RTW `2701327` × 同一冻结 BTW `b72dcd6` 的默认 v1 | `KNOWLEDGE_REAL_USER_GATE=1`、`KNOWLEDGE_TEST_FILTER=^TestRealHTTPKnowledgeWorkflowWithUserCenter$` 的原 `acceptance.sh` 顶层 exit=0；旧受众/旧三字段主体 HMAC 经消费者原字节验真，空证据、固定引用、同键丢回复回查保持原逻辑，主测试 70.01 秒 PASS。父日志 `/private/tmp/sea-rtw-default-v1-fixed-2701327-b72-l3.log` SHA256 `0f1577461000e427c63b9b6711747af1078a6364a9fe1d54a8ad702138c34c12`；私有 evidence `sea-knowledge-acceptance.HpU0LQ`，PG stop/status=3。 |
| 冻结 BTW `b72dcd6` 的双版严格消费者 | `TestSignedScopeHTTPRejectsBeforeSourceAndAgent`、`TestSignedToolsScopeAndNativeGraphHTTP` 两项 race PASS/0 FAIL；日志 `/private/tmp/sea-btw-b72-v2-consumer-strict-final.log` SHA256 `d6b905beabc4f1d0a2e13938c7128ffe82999ecff5f354b32fc340684f3648b0`。 |
| 本机三项 PG16/race 合一 | old turn/hash + Outbox 指纹、坏目录拒绝、同操作固定 wire 三项 PASS/0 FAIL；父日志 `/private/tmp/sea-rtw-v2-producer-three-pg-race-l2.log` SHA256 `aee2893950799cc7f99ac1dc1ded5582773ea9c931a9d59116450c58ca36f977`，私有 evidence `sea-rtw-v2-continuous-writes.ZsLBnz`，PG stop/status=3。 |

两版知识脚本还各执行当前源码的 knowledge/usercenter `go vet`
和 `git diff --check`，均随顶层 exit=0；`go mod verify`
返回 `all modules verified`。冻结 BTW b72 的目录及 RTW 业务固定头
`2701327` 试后均 clean。上述日志含测试 EventID、SearchID、AnswerID
与引用 ID，不含 JWT、scope key 或数据库凭据。

## 生产准入、回退与交接

本候选只有**新开本机 PG 数据**，生产四张知识表旧行预检、
异常归属、生产在线 Stage3 expand/持续双投影与默认关闭期间水位
尚未签收；本机 marker/nonce 不能当作生产迁移工单。正式 Stage4
前，RTW DB owner 须给出实际全量异常清单、旧/新读面对账、水位、
不中断的列/索引扩展和恢复演练；BTW owner 须签正式部署的
Summary/Tools v1/v2 双读与 SDK 源锁；DC eventing 及 RTW 各事实
owner 须分别定义 v2 EventSchema 和原 Outbox 回放。本机两用户
没有解决 DC UUID↔RTW UID 的产品绑定，Web/WhaleHall 默认
产品流量、Session/coverage/bundle/dataset 也未切换。

在本机受控库回退时，未来新 Reserve 配置改回 `v1`，
已固定的 v2 操作仍按 v2 完成或明确等待，不改行、不重签 v1，
BTW 两版 verifier 保留；已接纳历史只读原字节，Outbox 重投仍沿用
原 EventID/原版本。交接给跨仓集成 owner 时，应同时记录 RTW
提供方 HEAD、BTW 消费者 HEAD、各通道有效配置、PG 候选 DDL
与水位/异常数、旧 turn/Outbox 指纹、上游同 SearchID/AnswerID
结果、关闭日志与未签生产前置。
