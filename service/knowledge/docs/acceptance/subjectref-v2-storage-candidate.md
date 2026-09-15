# SubjectRef v2 Knowledge 四表存储扩展候选验收（2026-09-15）

验收代码 HEAD：`7decdf4b8a750fb99ea1887f6f0c37420e7b137d`。状态：**LOCAL_VERIFIED / 阶段三局部候选**。数据库迁移只在本机新建的 PostgreSQL 16 测试库执行；没有对共享、测试或生产库应用，也没有把普通业务写流、读键或事件版本切到 v2。

## 存储合同与默认行为

`scripts/migrate-subjectref-v2-storage.sql` 在单事务中给四张旧知识表建立四张 sidecar。Session 的 v2 主键为 `(issuer,subject_id,session_id)`；accepted answer 保持旧 `answer_id` 主键，另有 `(issuer,subject_id,session_id,accepted_ordinal)` 唯一键和 v2 Session 外键；product operation 与 Tool parent 分别以 `(issuer,subject_id,session_id,operation_key)` 为 v2 主键。sidecar 中的 `tenant_id` 仅为固定 `platform` 的旧行外键槽，不进入 v2 主键；所有 sidecar 都用 PostgreSQL CHECK 阻止非 `rtw.identity/platform/规范正 int64 UID`。accepted 的新增旧表唯一锚索引允许复合外键同时绑定 `answer_id`、旧三元组、session 和 ordinal，不能把一个用户的 AnswerID 贴到另一个用户的旧行上。五条 FK 均先 `NOT VALID` 再 `VALIDATE`；重复执行会检查 sidecar 的完整列形状、CHECK 表达式、PK/UQ/FK 的源键和父键列及有效索引，拒绝同名对象漂移。

整事务先锁四张旧表的写入，再从**全部**旧行投影。任一坏 issuer、旧兼容槽、非规范 UID、降维撞键、FK 或不完整镜像都使 DDL 与投影一起回滚；不跳过未知历史行，不自动合并两个旧 slot。`ON CONFLICT DO NOTHING` 仅处理完全相同的重放；事务末用双向 `EXCEPT` 对四表旧键与 sidecar 精确对账，不允许冲突被吞掉。当前脚本是一次性冻结快照/backfill 候选，整表 `SHARE` 锁和全量对账**不能直接作为生产在线迁移作业**。锁释放后普通 v1 writer 继续产生新旧表行，本切片没有修改 Store 或服务配置中的运行期双写，因此 sidecar 会变旧；只有显式重放才补齐合法新行。这不是持续同步的 v2 权威读键。

`scripts/apply-subjectref-v2-storage-candidate.sh` 必须由操作者提供显式 DSN 和 `--apply`，先执行既有只读四表 preflight，只有无阻断报告才执行上述 SQL；`psql -X` 禁用操作者本地 `psqlrc` 对目标会话的隐式更改。普通 `Store.Migrate()` 仍只读取原 `schema.sql`；新脚本既不被构造器加载，也不被服务默认配置调用。旧 v1 history 是默认读口，受控 `/v2/knowledge` 历史读仍验证并投影原 v1 行；本候选没有把 sidecar 设为权威读键。旧 `turn_json/turn_hash`、AnswerID、SearchID、ordinal、旧 PK/FK、citation、outbox payload、签名或已冻结 wire 均未改写。

## 分层验收结果

| 层 | 固定结果 |
| --- | --- |
| L1 目录/合同 | 首次及重复应用空 PostgreSQL 16 schema 退出 0；同名 identity CHECK 或相同名称但错误父表/父键的 FK 被 catalog guard 拒绝。`bash -n`、`go vet -mod=readonly ./service/knowledge/cmd/subjectref-preflight`、`git diff --check` 均退出 0。SQL SHA-256 `d9fd3e9f222d53a91f62d3d91c0e44a961c9e9977daf6efb2528666cdcc9033f`。 |
| L2 真实 PostgreSQL 16/race | `go test -mod=readonly -race ./service/knowledge/cmd/subjectref-preflight -count=1 -v` 退出 0：原 preflight 全套与新 storage 用例全通过；两用户同 session 不串行、product/Tool 可先于 Session、四表首次/重放/新合法行补投、缺失 sidecar 精确回放、锁释放后新增 `archive` 撞键的失败回放、CHECK/FK/UQ catalog、坏 slot/issuer/UID 整事务回滚、旧 immutable trigger 仍阻止改 turn。日志 `/private/tmp/sea-rtw-v2-storage-r3.sfGMPP/race.log` SHA-256 `559378d3bbf9e5387fac4df37b2bef6c0973c0cb16727e760c4b1e9a8da1eac7`。 |
| L2 操作门禁 | 同一隔离库中，空库 preflight `blocking=false`，显式 gate 首次与重放退出 0、四表 sidecar 数均 0；插入一条旧 `archive` 行后 preflight `blocking=true`/`invalid_compatibility_slot=1`，gate 非零退出且仍无 sidecar 新行。首次报告 SHA-256 `7117db5881819d7efd335e130dc1529073d1463b699b0d6c46f6d10b05c0abf7`；阻断报告 SHA-256 `95fe5ac2d770f3bb77fd77b54f2290fac4d765e33a19bb678ff140f6b0aa5c7e`。 |
| L2 旧读与 v2 受控读 | 真实知识 Store 用 writer-valid 的 `rtw.identity/platform/42` 接纳答案，普通 `Store.Migrate()` 前 sidecar 不存在；显式投影及重放后又重放普通 schema，v1 `GetAcceptedAnswer` 与受控 v2 `GetVerifiedProductAcceptedAnswer` 在前后均为相同 AnswerID、SearchID、ordinal、`turn_json`、`turn_hash`，sidecar 正好一行。race 日志 `/private/tmp/sea-rtw-v2-storage-reader-r3.R2Jx01/race.log` SHA-256 `43933e222368be810f9820367b74ddc54d2878ca70429e8ba05051dc5c050018`。 |
| 停机边界 | 两轮最终验收的独立 PG 均经有界 `pg_ctl stop`，`pg_ctl status` 为 3；门禁只使用本机 loopback trust 库，没有线上旧数据扫描或部署。 |

第一次新测试红轮只因测试代码把 PostgreSQL `SHOW server_version_num` 的 text 直接扫描进 Go `int`，不是迁移逻辑错误；该 PG 已停止，红日志 `/private/tmp/sea-rtw-v2-storage-l2.wcaM0y/race.log` SHA-256 `04b290dbf0c15903caa7353969bf1a26c202848c47f4277c1f4b19542bc6fb92`。改为 SQL `current_setting(...)::integer` 后，最终完整 race 复轮通过。

## 交接与未完成项

生产前应由 RTW 知识 DB owner 对**真实旧行**运行只读 preflight，逐行处理非 `rtw.identity/platform/规范 UID`、投影撞键、turn/hash 或 citation 异常；报告中的本机 0 阻断不能替代生产报告。生产迁移还需要由 DB owner 把本候选的整表锁/全量事务拆成可限流、可恢复、有水位的在线 expand 作业，并验证并发创建后的四表对账。应用目前仍只写旧表；后续须在 accepted 的原事务以及 product/Tool 创建事务中补**显式开启的**新写双投影和幂等回执，失败时一并回滚旧业务结果。RTW v2 业务写入/HMAC/Event 签发、权威 v2 读键切换、历史 slot 处置和生产部署均属于后续独立验收，不由此存储候选自动启动。
