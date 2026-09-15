# RTW Knowledge SubjectRef v2 阶段一只读预检

固定输入：RTW knowledge 开发集成提交 `8db77843b579d33cd1eafb0c339bd7a6b14f0134`。本切片只核对四张 PostgreSQL v1 主体表：`knowledge_answer_sessions`、`knowledge_accepted_answers`、`knowledge_product_search_operations`、`knowledge_tool_parents`。它不迁表、不改答案、不切 v2 写路径，也不取得异常主体的权威映射。

执行入口为 `go run ./service/knowledge/cmd/subjectref-preflight -report <new-path>`，连接串只从 `KNOWLEDGE_PREFLIGHT_DSN` 环境变量读取。程序在单个 `REPEATABLE READ READ ONLY` 事务内验证 schema、键、行和答案字节；报告文件以 `0600` 创建，已有路径拒绝覆盖。无异常退出 0，有阻断项退出 3，连接/schema/写报告错误退出 1。JSON 的 SHA-256 是包含末尾换行的**实际报告文件字节**，程序只把该 SHA 输出到 stderr。

预检合同：

1. v1 `authority_id` 必须为 `rtw.identity`，`tenant_id` 必须为固定兼容槽 `platform`，`subject_id` 必须是 RTW User Center 正 `int64` UID 的规范十进制字符串。`platform` 不是实际租户；DC UUID 不能充当 RTW UID。
2. 四表逐一按 `(authority_id,subject_id,其余业务键)` 查去掉 `tenant_id` 后的重复组。不同 v1 槽即使原主键各自合法，投影重键仍阻断；报告给重复组行数和键 SHA，不合并。
3. 核旧版三张复合主键、答案的 `answer_id` 主键、主体复合 FK 和 session 序号 unique；核答案到 session 的主体/会话 FK、正 ordinal、session `last_ordinal` 与连续答案序号。已 `committed` 的产品 operation 必须与其 answer 的 `answer_id/search_id/完整主体/session` 一致。Tool parent 的子记录仍以 `operation_id` 引用，本切片不改变该键。
4. 对每个答案按原 `turn_json` UTF-8 字节计算 SHA-256，比较 `turn_hash`，再核 JSON 内的 answer/search/session/v1 主体与行一致；catalog 中还须有有效的 `knowledge_accepted_answer_immutable` 更新/删除前触发器。程序绝不 UPDATE/DELETE 历史答案。

报告中的 `counts` 统计各类异常，`rows` 统计每张表扫描行数；零行的表可不出现在 `rows` 中。`findings` 只含异常代码、业务键 SHA 和投影组行数；原 UID、原 turn 和 DSN 不进入报告。最多保留 200 条 finding，`omitted_findings` 统计其余异常，任何截断仍保持阻断。只有 `blocking=false`、`omitted_findings=0` 且全部 schema/数据检查成功，才允许迁移 owner 继续设计 expand/contract；此报告本身不授权迁移。

验证分层与精确证据：

| 层 | 本机验收 | 结果 |
| --- | --- | --- |
| L1 确定性 | `go test ./service/knowledge/cmd/subjectref-preflight -count=1`；含正 int64 边界、零、前导零、溢出和 DC UUID | 通过；未设 PG 环境变量时数据库测试明确 skip |
| L2 隔离存储 | 本机临时 PostgreSQL，使用仓内 `schema.sql` 建隔离 schema；干净和异常 fixture；缺不可变触发器/旧 FK 反例 | 干净通过；异常报告 11 行、17 findings、0 截断、`blocking=true`；异常报告 SHA `bdd110e44a825480bb6133ad20d9a1b8e0f87423ed70e1bc4e8fec8806a9ef35` |
| L3 可执行边界 | 本机独立数据库构建并运行 CLI，空表报告后插入非规范 `042` UID 再运行 | 空表退出 0，SHA `7117db5881819d7efd335e130dc1529073d1463b699b0d6c46f6d10b05c0abf7`；异常退出 3，SHA `34d09273e6a5abd51989bbf7697f1ae8a6479ea2ce42355b3d8a57ca5b7307f7`；预检前后 session 行数均为 1 |

本机报告只证明预检对上述 fixture 的行为，未连接共享测试库或生产库。哈希核对能发现现存 `turn_json/turn_hash` 不一致；若历史两者都曾被一致地篡改且没有可信外部基线，它无法证明原始字节从未变化。catalog 检查触发器名称、事件和启用状态，不证明其函数体与可信版本逐字相同。迁移 owner 仍需另做真实数据水位、旧/新读面对账、并发写及恢复演练。
