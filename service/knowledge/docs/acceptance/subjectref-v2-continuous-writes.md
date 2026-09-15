# SubjectRef v2 知识新写双投影候选

状态：**阶段三的本机局部候选，未启用生产业务流量**。本切片从知识开发集成固定头 `77b983cf204f5d96213599e8d5bb5eb920185756` 出发，补足此前四表一次性离线快照后普通 writer 会让 sidecar 继续落后的问题。旧知识行、AnswerID、SearchID、`turn_json`、`turn_hash`、citation、Outbox payload、v1 HMAC 和事件版本都不修改。

## 写入与启动契约

`model.New()` 与普通 `Store.Migrate()` 仍只装配旧 schema 和旧写入。只有 Knowledge ServiceContext 配置中显式设置 `SubjectRefV2Writes.Enabled=true`，且 `Mode` 非 `pro`，才装配 `WithContinuousSubjectRefV2Writes()`。单独构造带 Option 的 Store 仍处于不可写状态；ServiceContext 必须先调用 `CheckContinuousSubjectRefV2Writes()` 成功才能对外提供写入。

本机候选先由 DB owner 在**新建的隔离 PostgreSQL 16**上执行四表只读预检，再用既有 `apply-subjectref-v2-storage-candidate.sh --apply` 应用候选 DDL 与一次性离线投影。启动时需要测试 owner 预置的一小时内随机 64 位十六进制 nonce 标记、`postgres`/`sea_knowledge_test`、双端 IPv4 回环，以及四个真实 sidecar 的准确目录。目录核对不仅统计名字：逐表核 CHECK 表达式、14 个 CHECK/PK/UQ/FK 的所属关系、键列与父表列、validated/index ready，以及四表列顺序、类型、NOT NULL、无默认值/generated/identity。弱同名 FK 或默认 UID 列在第一业务写前拒绝。这个标记只证明**隔离测试库**和已验的候选 DDL，不能作为生产在线扩表的审批机制。

本机测试配置示例：

```yaml
SubjectRefV2Writes:
  Enabled: true
  LocalTestNonce: <test-owner-provisioned-64-hex-nonce>
```

开关开启时 SubjectRef 的新键只取固定 `issuer=rtw.identity` 和 RTW UserCenter 权威正 `int64` UID 的规范十进制 `subject_id`；旧 `tenant_id=platform` 只存兼容锚，不进入 v2 主键。旧 `archive` 或非规范 UID 的请求会在旧行创建前拒绝；桌宠/DC UUID 不参与这个键，也不推断 RTW UID。

AcceptedAnswer 在原 Session `FOR UPDATE` 行锁事务内先建立旧 Session，再核 v2 Session、写旧答案/引用和 v2 answer sidecar，最后更新原 ordinal 并只提交一次。AnswerID 丢回执重试仍先核旧 `turn_hash`、原 `turn_json`、SearchID 与主体，然后在同事务补缺失的 Session/Answer sidecar；异文冲突或 sidecar CHECK/FK/唯一键异常使旧业务行与投影一并回滚。Product Search 与 Tool Parent 的创建在候选开启时各用一笔事务：旧 INSERT、按原主体/session/key `FOR SHARE` 读回固定 request hash 或 module ID、投影 sidecar，再提交；有效重试补缺失 sidecar，不产生第二个 SearchID/AnswerID/Tool OperationID。开关关闭时两个 Reserve 仍按原单条旧表 INSERT 路径运行。

写面失败以现有 OBS 入口分类为 `SUBJECTREF_V2_STORAGE_UNAVAILABLE` 或 `SUBJECTREF_V2_PROJECTION_CONFLICT`，不写 `fmt` 阶段输出、不把 UID 放进 Prometheus label 或复制完整答复正文。旧知识 Outbox 不因双投影新增第二份主体事件。

## 本机验收

`KNOWLEDGE_V2_TEST_FILTER='.' bash service/knowledge/scripts/test-subjectref-v2-continuous-writes.sh` 在新建、测试专用的 PostgreSQL 16 上顶层退出 0；每个 Go 测试使用独立 schema，测试 owner 写入随机标记，退出时 PG 有界停止且 `pg_ctl status` 退出 3。受影响 `model`、`svc`、`subjectref-preflight` 三包**全部** `-race -count=1` 通过；vet 与 diff-check 退出 0。父测试日志 SHA-256=`02c0b95c47032615c38ca30bd5d304ef283d4d378bc5814a0e3fe7cc5ac5cfd0`，vet 与 diff-check 各为 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`，PG stop SHA-256=`e7b24f63c690806243625ca8cb53f81f1a71a030c6c34a3820e76021c6810564`。日志在测试工具生成的 `sea-rtw-v2-continuous-writes.kHUFvA` 证据目录；它只属于本机候选，不是生产验收。

真实 PG 断言覆盖：两个规范正 UID 同 session 各有 ordinal 1 与独立 v2 key；缺 Session/Answer/Product/Tool sidecar 的原键重试只修复一行；并发丢答复重试不重复答案；旧 AnswerID 撞另一 UID、额外 sidecar FK 或 CHECK 拒绝旧 Session/答案/operation 原行提交，解除异常后同操作成功；默认关闭旧写不投影而显式开启的核对重试补投；旧冻结答案 `turn_json`/`turn_hash` 与现有 Outbox fingerprint 前后相同。启动测试还证明 Option 单独启用不可写，错误 nonce、`pro` 模式、指向弱外表的同名 validated FK、带默认 UID 的 sidecar 列均在首写前拒；正确 nonce/DDL 配可达的测试 UserService gRPC 可完整建立 ServiceContext。

第一次扩展 ServiceContext 正向测试曾给未监听的 `127.0.0.1:12345` 假 RPC，go-zero 按契约拨号超时使该轮退出 1；它是测试夹具错误。该轮 PostgreSQL 已停止；改为有界本机 gRPC UserService 后上述三包全量 race 复轮退出 0，没有改业务代码来掩盖拨号要求。

## 交接边界

本切片证明**新写连续投影的本机事务候选**，不把 sidecar 变成权威 v2 读键。此前或开关关闭期间产生的旧行仍需 DB owner 离线全量预检、在线迁移水位和新旧面对账；一次性 `SHARE` 锁回填不能直接上生产。生产真实四表/citation 语义扫描、限流扩表、持续水位与恢复演练、RTW v2 业务 HMAC/事件签发、BTW 消费签收、普通 Web/WhaleHall v2 默认流量、H01 DC UUID↔RTW UID 账号关联和生产部署均欠验。回退只关闭后续双投影开关，旧 v1 读和原行继续可用；不得改写已存在的冻结答案或事件来伪装版本。
