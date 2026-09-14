# RTW 用户主体解析交接（WS02-A 局部实现）

本仓当前的权威用户身份是 `users.uid`：注册时服务端生成 Snowflake UID，`users.uid` 有唯一索引；登录由 User RPC 验证凭据，User Center API 将该 UID 写入其签发的 JWT `userId`。受保护的 HTTP 路由由 go-zero JWT 中间件核验签名，API 的 `identity.ResolveUser` 仅从该中间件上下文读取正整数 UID，再通过 User RPC `GetUser` 核对仍存在且返回 UID 相同的用户。请求体、客户端事件及任意 `X-User-ID` 头不参与解析；缺失、非法、已删除和 RPC 错配都拒绝。

经本次单平台产品决策，服务侧 `identity.ResolveSubjectRef` 将已验且经 RPC 核对的 UID 映射成完整 `SubjectRef{authority_id:"rtw.identity", tenant_id:"platform", subject_id:"<十进制 UID>"}`。这里的 `platform` 是 **RTW 当前唯一用户空间的固定命名空间**，不是组织、团队、付费租户或从 JWT/客户端取出的字段；无需增设组织/租户表。该三元组的三个字段由 RTW 代码生成，不能从客户端事件 payload、请求体或头部反序列化获得。以后若产品引入多个主体空间，必须先定义权威映射、迁移和历史关联规则，再修订这个合同，不能就地改变既有用户的主体键。

**仍待正式交接。** 当前只有 RTW 进程内的服务侧解析函数；还没有对外发布的主体签发/解析端点、服务间认证、RTW 产品事件、固定 EventSpec（producer、event type/schema version、事件主键与发生/接收时间、请求/展示/可见/阅读引用和重放冲突规则），也没有 BTW `TrustedFactBinder` 的真实 RTW 来源接线。正式 H09.a 需要真实 RTW 事件经 DC 交付至 BTW，再逐条核对来源与主体收据；本函数和三元组自身不能替代此验收。

本切片验收范围是**真实 go-zero JWT 中间件 + 原 User RPC 接口的局部身份解析和固定平台命名空间三元组生成**；测试 RPC 使用固定桩，没有真实用户数据库、事件来源或服务间接口，因此 WS02-A/H09.a 仍为 `PARTIAL/NOT_VERIFIED`。

本地验证（2026-09-14，隔离工作树）：`go test -mod=readonly -race -count=1 ./service/user/user/api/internal/identity ./service/user/user/api/internal/logic/user ./service/user/user/api/internal/handler/user`、`go test -mod=readonly ./service/user/user/...`、`go vet ./service/user/user/...` 均通过。JWT 测试用本仓 `jwt.GetToken` 签发，再经 go-zero 公开 `rest/handler.Authorize` 校验；错误签名在读取 RPC 前即 401，正确签名的 UID 与 RPC 回执一致才生成三元组。测试同时证明恶意请求体/头部主体不覆盖服务端生成值、RPC 错配或删除失败时不会返回三元组、最大 `int64` UID 不会因浮点转化丢失。这是组件/接口桩验证，不代表在线 RTW↔DC↔BTW 联调。

## 账户状态与旧令牌：后续增量合同

User RPC 的源协议 `proto/user.proto` 给 `UserInfo` 新增 `optional int64 status = 6`，由权威 `users.status` 明确填入。这里 proto3 **presence 必须保留**：`status=0` 且字段存在才表示活跃；字段缺失表示旧 User RPC/不完整回执；非零状态不授予主体。共享 `service/user/user/identity.ResolveUser/ResolveSubjectRef` 对停用返回 `ErrUserInactive`，知识产品读面映射 403、User Center 本人资料读面映射封禁码 1011；缺字段返回 `ErrStatusUnavailable`，知识读面为 503、User Center 本人资料为服务繁忙。其他直接 `GetUser` 调用方仍收到 `Found=true` 和附带状态，不改变文章作者名、消息资料、收藏/关注存在性查询，也不改变独立 Admin RPC 的管理员读面。协议增量按旧字段号兼容，但知识身份解析**有意拒绝**缺状态的旧服务回执；部署顺序必须先升级 User RPC 所有实例并确认 `GetUser` 对活跃 `status=0` 显式带字段，再切换知识 API 和 User Center 新身份解析。回滚解析前先评估旧令牌停用读面风险。

固定版本的 `protoc 29.2` 与 `protoc-gen-go v1.31.0` 从 `proto/user.proto` 重新生成 `service/user/user/rpc/pb/user.pb.go`；变更前对原 proto 原样重生，SHA256 与仓库已有生成文件一致，确认生成路径与工具版本。`TestUserStatusProto3Presence` 证明旧回执缺字段、活跃 0 与停用 1 三种线上序列化结果可区分；`TestGetUserReportsPresentAccountStatusFromPostgres` 在脚本自建 PG16 内直接核对数据库 0/1/2 到真实 GetUser 回执；完整真实 User Center→User RPC→知识 HTTP 旧 JWT 用例及结果见 `service/knowledge/docs/acceptance/accepted-answers.md`。本地命令 `KNOWLEDGE_REAL_USER_GATE=1 bash service/knowledge/scripts/acceptance.sh` 同时跑知识与 User Center 全包 race/vet，退出 0。

当前 User RPC `LoginLogic` 仅把数据库状态 1 判为封禁，其他非零状态仍可登录；共享身份解析对所有非零状态拒绝主体。状态 2 的正式账户含义与登录侧统一判断尚未约定，这是独立已知缺口。本切片也未验证 Redis 登出黑名单、正式服务发现、RTW 用户事件→DC→BTW Binder、生产密钥或线上部署；WS02-A/H09.a/H02 整体仍非 ACCEPTED。
