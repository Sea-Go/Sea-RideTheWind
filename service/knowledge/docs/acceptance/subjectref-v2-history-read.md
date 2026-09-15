# SubjectRef v2 历史只读投影验收（2026-09-15）

验收代码 HEAD：`ee53b1167d4c4cbead8ebd9dddffa7ef1b86c1e5`。状态：`LOCAL_VERIFIED`；只验证本机隔离库与进程，未部署。

## 产品合同

新增独立的 JWT `UserAuth` `/v2/knowledge` 三个只读 GET：`/answer-sessions/:session_id/accepted-answers`、`/:answer_id`、`/:answer_id/citations`。请求仍只使用既有 session/answer/分页字段，SubjectRef 只由 JWT UID 经真实 User Center RPC 验证后解析。list/detail 的外层主体只返回 `{issuer:"rtw.identity",subject_id:"规范正 int64 UID 字符串"}`。旧 `turn_json` 仍是完整 v1 turn，旧 storage `authority_id/tenant_id/subject_id` 和 `turn_hash` 没有改键、改值或重算写回；citations 继续查询引用实时状态。

v2 专用读取在 `REPEATABLE READ READ ONLY` PG 快照中核旧 scope 与 UID、turn 原始 SHA-256、Request/Result 的 session/answer/search/subject、证据包原字节和 receipt、持久引用的 evidence/locator/quote hash。stage-1 preflight 与 v2 共用 critical-key/Unicode/Go 大小写别名扫描；preflight 保持旧模式，v2 另拒完整 turn 中任意对象的**精确**重复键。另一个旧 slot 若投影成同 issuer+UID+session+ordinal，整段 v2 history 返回冲突，包括请求落在后续空页时。

## 分层证据

| 层 | 本机验收结果 |
| --- | --- |
| L1 合同/生成 | 固定 goctl 1.9.2 执行 `service/knowledge/scripts/generate.sh`，随后 `git diff --exit-code` 为 0；原 Swagger 56 个 path 与原 definitions 逐项不变，只增 3 个 `/v2` path。生成 Go/TypeScript v1 DTO 没有字段变更。 |
| L2 隔离 PG | 完整 `KNOWLEDGE_REAL_USER_GATE=1 service/knowledge/scripts/acceptance.sh` 的 Knowledge/User race+vet 轮退出 0（`/private/tmp/sea-rtw-v2-history-acceptance-final-20260915.log`）；最终代码头另跑 `TestProductHistoryV2*` race，旧 slot 撞键、`9223372036854775807` 精确 UID、另一 UID 同 AnswerID 404、原 hash/重复或别名主体键/额外 realm/quote hash 篡改均拒。 |
| L3 真实进程/观测 | 最终代码头的真实 User Center RPC/API + Knowledge HTTP `TestRealHTTPKnowledgeWorkflowWithUserCenter` race 通过；两真实 JWT 在同 session 各有一条旧答案，只读 v2 list/detail 只见己方，跨 UID 按固定 AnswerID 404，停用 owner 后 v1/v2 GET 403、other 仍 200。旧 v1 detail GET 响应原字节在 v2 访问及引用撤回前后完全相同；旧 PG `turn_json` 与 `turn_hash`、v2 turn 字节一致。撤回后 v2 citation 状态转 unavailable，原 quote hash 与 turn SHA 不变。最终轮 telemetry/共享 scanner race、受影响 vet、生成 diff 同为 0。 |

同隔离 PG 中重跑 stage-1 主异常 fixture：17 行、22 findings、0 截断、blocking=true。新 0600 报告 `/private/tmp/sea-rtw-v2-history-preflight-shared-20260915.json` 与先前 `/private/tmp/rtw-subjectref-preflight-r2.SxLUI0/anomaly-report-final-r2.json` 经 `cmp` 逐字相同，二者 SHA-256 均为 `b7d57f83ea17054cc69902c759afbe89f02b6c1d55d6076ab6d001ebb7dd4e8e`。

最终 L3 证据目录 `/private/tmp/sea-rtw-v2-history-head-final.ZQdvXZ`：真实 HTTP、PG 反例、telemetry、vet、生成及 PG 停止日志各在同目录。`observability/knowledge-http.jsonl` SHA-256 为 `4ef7a316233dd91f3d869dea742c2a7ded61bf4f47d92805c3302b5fa1df5e9d`；`knowledge-metrics.prom` SHA-256 为 `e2aa1f42e518d5b4b72aa64965f20ce653cded84d6b688ee927c245493aa3f96`。843 条真实进程 JSONL 全含 `timestamp/level/service/environment/service_version/instance_id/component/log_source/event/message`，全匹配验收代码 SHA；v2 list/get 原业务阶段共 26 条，trace/span/request/operation 缺口 0，终态 outcome/duration 缺口 0，成功终态的规范 UID 缺口 0。两个 v2 只读 operation 都有成功 metrics，均无 commits 计数，也没有 UID 指标标签。PG 已由脚本有界停止。

最终轮为节省宿主空间复用了前一轮已构建的 race User RPC/API 二进制；从工作树基点 `f1c158fa042e06a2c260c32c81fe943315c08400` 到最终代码 HEAD 的 diff 没有 `service/user/` 文件变更。Knowledge HTTP 测试进程由最终代码头重新编译，日志 `service_version` 也固定为该 HEAD。第一次提交后定向轮在重编无改动 User RPC 时遇到 linker `no space left on device`，未启动 HTTP；它不计通过证据。

## 边界与交接

该切片仅发布 v2 **历史读候选接口**；现有 Web/WhaleHall 正式客户端仍默认请求 v1。v2 历史写入、Outbox、DB 改键、真实环境历史行 preflight、账号绑定与部署均未验证，不能据本机 PASS 推断线上旧行无异常。接入方应先以同一旧 turn fixture 验 v2 外层/v1 turn 混读，再独立评审生产旧行与路由切换；若生产旧 slot 碰撞，v2 GET 会阻断并保留旧 v1 原行为。
