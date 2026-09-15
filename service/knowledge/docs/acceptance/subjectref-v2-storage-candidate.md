# SubjectRef v2 Knowledge 四表存储扩展候选验收（2026-09-15）

验收代码 HEAD：`4800a107566e029f8352e2db290f27830133e04e`。状态：**LOCAL_VERIFIED / 阶段三局部候选**。数据库迁移只在本机新建的 PostgreSQL 16 测试库执行；没有对共享、测试或生产库应用，也没有把普通业务写流、读键或事件版本切到 v2。

## 存储合同与默认行为

`scripts/migrate-subjectref-v2-storage.sql` 在单事务中给四张旧知识表建立四张 sidecar。Session 的 v2 主键为 `(issuer,subject_id,session_id)`；accepted answer 保持旧 `answer_id` 主键，另有 `(issuer,subject_id,session_id,accepted_ordinal)` 唯一键和 v2 Session 外键；product operation 与 Tool parent 分别以 `(issuer,subject_id,session_id,operation_key)` 为 v2 主键。sidecar 中的 `tenant_id` 仅为固定 `platform` 的旧行外键槽，不进入 v2 主键；所有 sidecar 都用 PostgreSQL CHECK 阻止非 `rtw.identity/platform/规范正 int64 UID`。accepted 的新增旧表唯一锚索引允许复合外键同时绑定 `answer_id`、旧三元组、session 和 ordinal，不能把一个用户的 AnswerID 贴到另一个用户的旧行上。五条 FK 均先 `NOT VALID` 再 `VALIDATE`；重复执行会检查 sidecar 的完整列形状（键列不得带默认值、generated 或 identity）、CHECK 表达式、PK/UQ/FK 的源键和父键列及有效索引，拒绝同名对象漂移。

整事务先锁四张旧表的写入，再从**全部**旧行投影。任一坏 issuer、旧兼容槽、非规范 UID、降维撞键、FK 或不完整镜像都使 DDL 与投影一起回滚；不跳过未知历史行，不自动合并两个旧 slot。`ON CONFLICT DO NOTHING` 仅处理完全相同的重放；事务末用双向 `EXCEPT` 对四表旧键与 sidecar 精确对账，不允许冲突被吞掉。当前脚本是一次性冻结快照/backfill 候选，整表 `SHARE` 锁和全量对账**不能直接作为生产在线迁移作业**。锁释放后普通 v1 writer 继续产生新旧表行，本切片没有修改 Store 或服务配置中的运行期双写，因此 sidecar 会变旧；只有显式重放才补齐合法新行。这不是持续同步的 v2 权威读键。

`scripts/apply-subjectref-v2-storage-candidate.sh` 必须取得 `--apply`、显式 DSN 及刚创建的 64 个十六进制字符随机 nonce（`KNOWLEDGE_SUBJECTREF_V2_TEST_NONCE`）；测试夹具 `scripts/test-subjectref-v2-storage-gate.sh` 才能在**新建的隔离 PG16** 中写入 `public.knowledge_subjectref_v2_local_test_gate`，执行脚本本身不创建标记。门禁在 preflight 前及 SQL 前两次核标记一小时有效、数据库 `postgres`、测试用户 `sea_knowledge_test`、server/client 双端 IPv4 loopback；不满足时在任何 DDL 前退出。`psql -X` 禁用操作者本地 `psqlrc` 对目标会话的隐式更改。通过后先运行既有只读四表 preflight，只有无阻断报告才执行上述 SQL。普通 `Store.Migrate()` 仍只读取原 `schema.sql`；新脚本既不被构造器加载，也不被服务默认配置调用。旧 v1 history 是默认读口，受控 `/v2/knowledge` 历史读仍验证并投影原 v1 行；本候选没有把 sidecar 设为权威读键。旧 `turn_json/turn_hash`、AnswerID、SearchID、ordinal、旧 PK/FK、citation、outbox payload、签名或已冻结 wire 均未改写。

## 分层验收结果

| 层 | 固定结果 |
| --- | --- |
| L1 目录/合同 | 首次及重复应用空 PostgreSQL 16 schema 退出 0；同名 identity CHECK 或相同名称但错误父表/父键的 FK 被 catalog guard 拒绝。`bash -n`、`go vet -mod=readonly ./service/knowledge/cmd/subjectref-preflight`、`git diff --check` 均退出 0。SQL SHA-256 `1f511623993975e57554b0686243be76adebf520f9b25165c80ff3925f4106cf`。 |
| L2 真实 PostgreSQL 16/race | `go test -mod=readonly -race ./service/knowledge/cmd/subjectref-preflight -count=1 -v` 退出 0：原 preflight 全套与新 storage 用例全通过；两用户同 session 不串行、product/Tool 可先于 Session、四表首次/重放/新合法行补投、缺失 sidecar 精确回放、锁释放后新增 `archive` 撞键的失败回放、CHECK/FK/UQ catalog、坏 slot/issuer/UID 整事务回滚、旧 immutable trigger 仍阻止改 turn。日志 `/private/tmp/sea-rtw-v2-storage-head4800.b20ONo/preflight-race.log` SHA-256 `63082de8a398dfff7a776a9f439aaee4a37b39e14f81c40e374ac1e719e364f7`。 |
| L2 操作门禁 | 固定代码头在新建本机 PG16 中执行 `bash scripts/test-subjectref-v2-storage-gate.sh` 退出 0：缺 nonce、缺 marker、错 nonce 和 Unix socket 四轮各 exit 2 且 sidecar 未建；测试夹具写入新随机标记后，首次/重放各 PASS；随后一条旧 `archive` 行触发 preflight `blocking=true`，gate 非零且 sidecar 仍 0。门禁证据 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-rtw-v2-storage-gate-test.AqdE1q`：缺标记日志 SHA-256 `09aa497c4e26416d7395247ec10e1e7fd18b76b9e7b405e8466a707d0cb7305d`、首次 `f04bc9489bad37f5cd96fb68bf87c8366cc59ea99ec2a4531582c204408ad8ce`、阻断 `1f6ff33c508402b9d240a3021f3e9a3820798efa36c7e7ca1293ea0fd2d0fe78`；`pg_ctl status` 为 3。 |
| L2 旧读与 v2 受控读 | 真实知识 Store 用 writer-valid 的 `rtw.identity/platform/42` 接纳答案，普通 `Store.Migrate()` 前 sidecar 不存在；显式投影及重放后又重放普通 schema，v1 `GetAcceptedAnswer` 与受控 v2 `GetVerifiedProductAcceptedAnswer` 在前后均为相同 AnswerID、SearchID、ordinal、`turn_json`、`turn_hash`，sidecar 正好一行。race 日志 `/private/tmp/sea-rtw-v2-storage-head4800.b20ONo/model-race.log` SHA-256 `dc828070d2e7993c2e299a6bc6f8fd082b5fad3fa969c6fa626e404a29c5468a`。 |
| 停机边界 | 固定代码头的完整 race PG 与隔离 gate PG 均经有界 `pg_ctl stop`，`pg_ctl status` 为 3；前者的 `code-head.txt` 固定 `4800a107566e029f8352e2db290f27830133e04e`，空 `code-status.txt` SHA-256 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`。没有线上旧数据扫描或部署。 |

第一次新测试红轮只因测试代码把 PostgreSQL `SHOW server_version_num` 的 text 直接扫描进 Go `int`，不是迁移逻辑错误；该 PG 已停止，红日志 `/private/tmp/sea-rtw-v2-storage-l2.wcaM0y/race.log` SHA-256 `04b290dbf0c15903caa7353969bf1a26c202848c47f4277c1f4b19542bc6fb92`。改为 SQL `current_setting(...)::integer` 后，最终完整 race 复轮通过。

独立审查的第一个 P2 红轮在**旧目录校验**首apply后给 sidecar `subject_id` 加 `DEFAULT '42'`，重复迁移错误通过；真实 PG race 红日志 `/private/tmp/sea-rtw-v2-storage-p2-key-red.b6wFsV/race.log` SHA-256 `fa209b07189b27f4730ae5017fec1b55adaec3dbf11804a70e9384348ba73f15`，PG 已停。修正后目录检查同时核 `pg_attrdef/attgenerated/attidentity`，默认 UID 与 `accepted_ordinal GENERATED ALWAYS AS IDENTITY` 两组反例均拒绝；最终完整 race 包含这两组用例。第二个 P2 旧脚本只凭任意非空 DSN 与 `--apply` 就执行，之前本机**无标记**库成功执行的日志 `/private/tmp/sea-rtw-v2-storage-r3.sfGMPP/gate-first.log` SHA-256 `94dd122b8143ee9414d7d681f0d753bd7ebd2d321b82e0cd850805d841779d7c` 保留为红证据；新门禁已由固定代码头的隔离 PG 四组拒绝与两组通过验证。

## 交接与未完成项

生产前应由 RTW 知识 DB owner 对**真实旧行**运行只读 preflight，逐行处理非 `rtw.identity/platform/规范 UID`、投影撞键、turn/hash 或 citation 异常；报告中的本机 0 阻断不能替代生产报告。生产迁移还需要由 DB owner 把本候选的整表锁/全量事务拆成可限流、可恢复、有水位的在线 expand 作业，并验证并发创建后的四表对账。应用目前仍只写旧表；后续须在 accepted 的原事务以及 product/Tool 创建事务中补**显式开启的**新写双投影和幂等回执，失败时一并回滚旧业务结果。RTW v2 业务写入/HMAC/Event 签发、权威 v2 读键切换、历史 slot 处置和生产部署均属于后续独立验收，不由此存储候选自动启动。
