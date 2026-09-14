# RTW 用户主体解析交接（WS02-A 局部实现）

本仓当前的权威用户身份是 `users.uid`：注册时服务端生成 Snowflake UID，`users.uid` 有唯一索引；登录由 User RPC 验证凭据，User Center API 将该 UID 写入其签发的 JWT `userId`。受保护的 HTTP 路由由 go-zero JWT 中间件核验签名，API 的 `identity.ResolveUser` 仅从该中间件上下文读取正整数 UID，再通过 User RPC `GetUser` 核对仍存在且返回 UID 相同的用户。请求体、客户端事件及任意 `X-User-ID` 头不参与解析；缺失、非法、已删除和 RPC 错配都拒绝。

经本次单平台产品决策，服务侧 `identity.ResolveSubjectRef` 将已验且经 RPC 核对的 UID 映射成完整 `SubjectRef{authority_id:"rtw.identity", tenant_id:"platform", subject_id:"<十进制 UID>"}`。这里的 `platform` 是 **RTW 当前唯一用户空间的固定命名空间**，不是组织、团队、付费租户或从 JWT/客户端取出的字段；无需增设组织/租户表。该三元组的三个字段由 RTW 代码生成，不能从客户端事件 payload、请求体或头部反序列化获得。以后若产品引入多个主体空间，必须先定义权威映射、迁移和历史关联规则，再修订这个合同，不能就地改变既有用户的主体键。

**仍待正式交接。** 当前只有 RTW 进程内的服务侧解析函数；还没有对外发布的主体签发/解析端点、服务间认证、RTW 产品事件、固定 EventSpec（producer、event type/schema version、事件主键与发生/接收时间、请求/展示/可见/阅读引用和重放冲突规则），也没有 BTW `TrustedFactBinder` 的真实 RTW 来源接线。被禁用用户是否仍可签发主体的规则也待定义，现有 `GetUser` 只返回存在性，不返回状态。正式 H09.a 需要真实 RTW 事件经 DC 交付至 BTW，再逐条核对来源与主体收据；本函数和三元组自身不能替代此验收。

本切片验收范围是**真实 go-zero JWT 中间件 + 原 User RPC 接口的局部身份解析和固定平台命名空间三元组生成**；测试 RPC 使用固定桩，没有真实用户数据库、事件来源或服务间接口，因此 WS02-A/H09.a 仍为 `PARTIAL/NOT_VERIFIED`。

本地验证（2026-09-14，隔离工作树）：`go test -mod=readonly -race -count=1 ./service/user/user/api/internal/identity ./service/user/user/api/internal/logic/user ./service/user/user/api/internal/handler/user`、`go test -mod=readonly ./service/user/user/...`、`go vet ./service/user/user/...` 均通过。JWT 测试用本仓 `jwt.GetToken` 签发，再经 go-zero 公开 `rest/handler.Authorize` 校验；错误签名在读取 RPC 前即 401，正确签名的 UID 与 RPC 回执一致才生成三元组。测试同时证明恶意请求体/头部主体不覆盖服务端生成值、RPC 错配或删除失败时不会返回三元组、最大 `int64` UID 不会因浮点转化丢失。这是组件/接口桩验证，不代表在线 RTW↔DC↔BTW 联调。
