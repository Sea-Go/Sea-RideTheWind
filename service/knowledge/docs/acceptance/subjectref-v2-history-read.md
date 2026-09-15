# SubjectRef v2 历史只读投影验收（2026-09-15）

验收代码 HEAD：`b732a49c521773019cfd1142d77b658aa96d1f8e`。状态：`LOCAL_VERIFIED`；只验证本机隔离库与进程，未部署。

## 产品合同

新增独立的 JWT `UserAuth` `/v2/knowledge` 三个只读 GET：`/answer-sessions/:session_id/accepted-answers`、`/:answer_id`、`/:answer_id/citations`。请求仍只使用既有 session/answer/分页字段，SubjectRef 只由 JWT UID 经真实 User Center RPC 验证后解析。list/detail 的外层主体只返回 `{issuer:"rtw.identity",subject_id:"规范正 int64 UID 字符串"}`。旧 `turn_json` 仍是完整 v1 turn，旧 storage `authority_id/tenant_id/subject_id` 和 `turn_hash` 没有改键、改值或重算写回；citations 继续查询引用实时状态。

v2 专用读取在 `REPEATABLE READ READ ONLY` PG 快照中核旧 scope 与 UID、turn 原始 SHA-256、Request/Result 的 session/answer/search/subject、证据包原字节和 receipt、持久引用每一行的 `search_id/evidence/locator/quote_hash`。stage-1 preflight 与 v2 共用 critical-key/Unicode/Go 大小写别名扫描；preflight 保持旧模式，v2 另拒完整 turn 中任意对象的**精确**重复键。认证 issuer+规范 UID+session 下所有旧 slot 若有相同 ordinal，整段 v2 history 返回冲突；即使 platform 自己的列表为空、撞键只发生在 archive/legacy 两个旧 slot，也返回冲突。

## 分层证据

| 层 | 本机验收结果 |
| --- | --- |
| L1 合同/生成 | 固定 goctl 1.9.2 执行 `service/knowledge/scripts/generate.sh`，随后 `git diff --exit-code` 为 0；原 Swagger 56 个 path 及各 path 的**内联 response schema** 逐项相等，只增 3 个 `/v2` path。该 Swagger 没有根 `definitions` 对象。生成 Go/TypeScript v1 DTO 没有字段变更。 |
| L2 隔离 PG | 最终代码头 `TestProductHistoryV2*` race 退出 0：`9223372036854775807` 精确 UID、另一 UID 同 AnswerID 404、原 hash/重复或别名主体键/额外 realm/quote hash 均拒。新增两个旧 slot 仅 archive/legacy 同 ordinal、platform 空页时 v2 list/detail 冲突，旧 v1 list 空/detail 404；另经正式 `AcceptSearchCitations` 创建第二条合法 SearchID 后受控改持久引用的 FK，旧 answer/turn/hash 与 v1 detail 保持正确，v2 list/detail 以及 v2 citations logic 都返回 `ErrArtifactUnavailable`。 |
| L3 真实进程/观测 | 最终代码头的真实 User Center RPC/API + Knowledge HTTP `TestRealHTTPKnowledgeWorkflowWithUserCenter` race 通过；两真实 JWT 在同 session 各有一条旧答案，只读 v2 list/detail 只见己方，跨 UID 按固定 AnswerID 404，停用 owner 后 v1/v2 GET 403、other 仍 200。旧 v1 detail GET 响应原字节在 v2 访问及引用撤回前后完全相同；旧 PG `turn_json` 与 `turn_hash`、v2 turn 字节一致。撤回后 v2 citation 状态转 unavailable，原 quote hash 与 turn SHA 不变。最终轮 telemetry/共享 scanner/preflight race、受影响 vet、生成 diff 同为 0。 |

同最终轮隔离 PG 中重跑 stage-1 主异常 fixture：17 行、22 findings、0 截断、blocking=true。新 0600 报告 `/private/tmp/sea-rtw-v2-history-p2-final.71A8Yf/preflight-report.json` 与先前 `/private/tmp/rtw-subjectref-preflight-r2.SxLUI0/anomaly-report-final-r2.json` 经 `cmp` 逐字相同，二者 SHA-256 均为 `b7d57f83ea17054cc69902c759afbe89f02b6c1d55d6076ab6d001ebb7dd4e8e`。

最终 L2/L3 证据目录 `/private/tmp/sea-rtw-v2-history-p2-final.71A8Yf`：真实 HTTP、PG 反例、telemetry、preflight、vet、生成及 PG 停止日志各在同目录。`observability/knowledge-http.jsonl` SHA-256 为 `6813b6395765ea5661f7467abb1e6b0c4792692b80765f2065b78448517afc9a`；`knowledge-metrics.prom` SHA-256 为 `e229836bfd05e2fbac31a74a3a13eca41ea8d504d1e6ffe5fd75c74ebce26ff4`。843 条真实进程 JSONL 全含 `timestamp/level/service/environment/service_version/instance_id/component/log_source/event/message`，全匹配验收代码 SHA；v2 list/get 原业务阶段共 26 条，trace/span/request/operation 缺口 0，终态 outcome/duration 缺口 0，成功终态的规范 UID 缺口 0。两个 v2 只读 operation 都有成功 metrics，均无 commits 计数，也没有 UID 指标标签。PG 已由脚本有界停止，`pg_ctl status` 退出 3。

最终轮为节省宿主空间复用了前一轮已构建的 race User RPC/API 二进制；从工作树基点 `f1c158fa042e06a2c260c32c81fe943315c08400` 到最终代码 HEAD 的 diff 没有 `service/user/` 文件变更。Knowledge HTTP 测试进程由最终代码头重新编译，日志 `service_version` 也固定为该 HEAD。第一次提交后定向轮在重编无改动 User RPC 时遇到 linker `no space left on device`，未启动 HTTP；它不计通过证据。早期 `6f9aaf5` 的功能轮发生在两项 P2 修正前，也不计最终代码通过证据。P2 红轮 `/private/tmp/sea-rtw-v2-history-p2-red.t9Nhvm/model-red.log` 分别复现了空页撞键与持久引用 SearchID 错误接纳，修正后的最终轮分别通过。

## 边界与交接

该切片仅发布 v2 **历史读候选接口**；现有 Web/WhaleHall 正式客户端仍默认请求 v1。v2 历史写入、Outbox、DB 改键、真实环境历史行 preflight、账号绑定与部署均未验证，不能据本机 PASS 推断线上旧行无异常。接入方应先以同一旧 turn fixture 验 v2 外层/v1 turn 混读，再独立评审生产旧行与路由切换；若生产旧 slot 碰撞，v2 GET 会阻断并保留旧 v1 原行为。
