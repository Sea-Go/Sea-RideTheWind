# RTW 用户主体解析交接（WS02-A 局部实现）

本仓当前的权威用户身份是 `users.uid`：注册时服务端生成 Snowflake UID，`users.uid` 有唯一索引；登录由 User RPC 验证凭据，User Center API 将该 UID 写入其签发的 JWT `userId`。受保护的 HTTP 路由由 go-zero JWT 中间件核验签名，API 的 `identity.ResolveUser` 仅从该中间件上下文读取正整数 UID，再通过 User RPC `GetUser` 核对仍存在且返回 UID 相同的用户。请求体、客户端事件及任意 `X-User-ID` 头不参与解析；缺失、非法、已删除和 RPC 错配都拒绝。

**尚未实现完整 SubjectRef 签发。** 当前 `users` 表、User RPC、JWT 和用户中心 API 均没有权威 `tenant_id`、组织成员关系或用户→租户归属，也没有发布的 authority ID 注册表。DataCenter H01 只负责三元组 wire 值；BTW 的事实 worker 要求 RTW 来源适配器提供完整 `authority_id/tenant_id/subject_id`。不能以 `uid` 充当租户，不能从客户端事件 payload 获取租户，也不能把单租户假设写死成默认值。

正式交接前需由 WS02 产品/身份 owner 决定并落地：① 租户的领域含义及权威关系表/服务（包括用户迁移、注销、跨租户、历史关联语义）；② `authority_id` 的稳定命名与版本；③ 服务端签发/解析端点及服务间认证方式，以及被禁用用户是否仍可签发主体的规则（现有 `GetUser` 只返回存在性，不返回状态）；④ RTW 产品事件的固定 EventSpec（producer、event type/schema version、事件主键与发生/接收时间、请求/展示/可见/阅读引用和重放冲突规则）。实现后使用真实用户、真实租户关系、真实 RTW 事件，经 DC 事件交付与 BTW `TrustedFactBinder` 联验；缺任一环节不得接纳 H01 业务映射或 H09.a。

本切片验收范围是**真实 go-zero JWT 中间件 + 原 User RPC 接口的局部身份解析**，测试 RPC 使用固定桩；没有真实用户数据库、租户映射、事件来源或服务间接口，因此 WS02-A/H09.a 仍为 `PARTIAL/NOT_VERIFIED`。

本地验证（2026-09-14，隔离工作树）：`go test -mod=readonly -race -count=1 ./service/user/user/api/internal/identity ./service/user/user/api/internal/logic/user ./service/user/user/api/internal/handler/user`、`go test -mod=readonly ./service/user/user/...`、`go vet ./service/user/user/...` 均通过。JWT 测试用本仓 `jwt.GetToken` 签发，再经 go-zero 公开 `rest/handler.Authorize` 校验；错误签名在读取 RPC 前即 401，正确签名的 UID 与 RPC 回执一致才成功。该证据是组件/接口桩验证，不代表在线 RTW↔DC↔BTW 联调。
