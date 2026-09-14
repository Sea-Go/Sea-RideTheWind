# H01 候选方案：DC 账号绑定与 RTW 产品会话交换

状态：**选型尚待产品确认；此独立开发分支默认关闭，未部署。** 此实现对应 Sea-Docs H01 的“RTW 维护 DC UUID→RTW UID 显式绑定”选项。DC UUID 与 RTW 数字 UID 保持不同主键；不按邮箱或昵称推断，不共享 JWT 签名密钥，也不让桌面端自报 SubjectRef。

## 工作范围与权威状态

- [W0:ROOT] 独立 RTW 工作树 `/Users/edy/Sea/.codex-worktrees/sea-rtw-account-binding-20260914`，基线 `ded36cb`。[W1:WRITE] `service/user/user/identity/{resolve.go,linking/}`、User Center 配置/装配/手写扩展 Handler 与直接测试、知识产品的真实 User Center 验收测试。[R1:READ_ONLY] DC 的 `/v1/auth/me`、WhaleHall Bun、Sea-Docs、RTW 原始脏树及集成树。[D1:DEPENDENCY] 锁定 go-zero JWT 解析器、pgx 与 User RPC；不修改模块缓存。[G1:GENERATED] 原 `handler/routes.go` 与 `types.go` 保持生成源原样；H01 候选路由由 `RegisterAccountLinkHandlers` 独立注册。[X1:EXTERNAL] 仅脚本启动的随机端口隔离 PG16 和 HTTP 会话测试服务；无共享/生产写入。[N1:OUT_OF_SCOPE] 生产启用、DC 本仓修改、WhaleHall 接线和即时撤销合同。[T1:TEMP] 保留测试 PG 数据/日志供核对。
- 主职责 [C4:PERSISTENCE] 是 RTW identity 的 UUID 永久归属与活动链接 CAS；跨 [C1:TRANSPORT] 双令牌分离及会话交换、[C2:APPLICATION] DC 当前会话和 User RPC 复核、[C7:CONTRACT] 短 RTW JWT、[C8:VERIFY] 真 PG/真实 RTW 进程验收。DC 是其 bearer 有效性的唯一判断者；User RPC 是 UID 当前有效性的唯一判断者；知识产品维持现有 RTW JWT 身份门禁。

## 合同与迁移

显式在 **RTW User Center 所属数据库**执行 `schema.sql`，再配置 `AccountLink.Enabled=true`、`AccountLink.PostgresDSN` 与固定 DC `AccountLink.DataCenterMeURL=https://.../v1/auth/me`。生产仅允许 HTTPS；隔离测试允许 loopback HTTP。默认未配置时不注册任何 H01 路由，现有登录/知识产品不变。新 pool 由 User Center 进程创建/关闭，不在 Store 构造时自动迁移。新表不存 DC bearer、RTW JWT、邮箱或密码；`rtw_dc_link_identities` 留存 UUID 首次归属，`rtw_dc_account_links` 按 UID 保存活动/停用、修订及唯一活动 UUID。

| 接口 | 凭据与输入 | 成功与拒收 |
| --- | --- | --- |
| `GET /usercenter/v1/account-link` | `X-RTW-Authorization: Bearer <RTW JWT>` | User RPC 再查活跃 UID；返回本人链接/状态/修订，缺失 404。 |
| `POST /usercenter/v1/account-link` | 同上，并以标准 `Authorization: Bearer <DC opaque access>` 提交 DC 当前会话；JSON `{"expected_revision":0}` | 先由 DC `GET /v1/auth/me`验证会话并取规范 UUID；同一 RTW UID+UUID 重投幂等，首次修订 1。活动 UID 换 DC、不同 UID 抢同 UUID、旧修订均 409。 |
| `DELETE /usercenter/v1/account-link` | RTW JWT；JSON `{"expected_revision":N}` | 活动链接 CAS 停用并加修订；随后可由同 UID 显式绑定另一个 DC UUID。旧 UUID 仍保留原 UID 归属，不得被别的 UID 接管；跨 UID 恢复需要未来单独的受审查流程。 |
| `POST /usercenter/v1/product-sessions/exchange` | 仅 `Authorization: Bearer <DC opaque access>`，无客户端 UID | 每次实际请求 DC `auth/me`，在 PG 锁内核对该 UUID 的活动链接并向 User RPC 验证 UID 状态，签发本仓 HS256、`userId=<数字 UID>`、`exp-iat=120s` 的 RTW JWT。返回 `token/expires_at_unix/link_revision`；撤销 DC 会话 401、无绑定 404、停用 RTW UID 403、DC 不可用 503。 |

绑定请求中的两个凭据不能一起进入 go-zero 通用 JWT 失败日志：该中间件会转储请求。扩展 Handler 使用 go-zero 公开 `TokenParser`，只在**剥离 DC 头和正文的请求副本**中验证 RTW JWT，并复用现有 RTW 黑名单中间件；原请求的 DC bearer 只交 DC `auth/me`。失败响应不含令牌。DC verifier 不跟随重定向、不接受非 loopback HTTP，限制响应大小，只使用 `id`，忽略邮箱/昵称。

## 已验范围

- `bash service/user/user/identity/linking/acceptance.sh`：自建随机端口 PG16，go-zero 真路由、PG 唯一键/并发抢绑、幂等、停用/换绑、同人和异人 UUID 反例、错误 RTW JWT、DC 会话撤销/切号、RTW UID 停用、120 秒 JWT、DC 超时/错误/重定向/响应无效反例，在 `-race` 与 vet 下通过。HTTP DC `auth/me` 为可切换当前会话的**测试实现**，并非 DataCenter 真进程。
- `KNOWLEDGE_REAL_USER_GATE=1 KNOWLEDGE_KEEP_EVIDENCE=1 bash service/knowledge/scripts/acceptance.sh`：真实 RTW User RPC、User Center、隔离用户 PG 与知识产品 HTTP 进程；网页身份的 RTW 登录 JWT 显式绑定 DC HTTP 会话 fixture，之后只用 DC bearer 交换短 RTW 产品 JWT；真实知识产品路线经 User RPC 解析为同一 UID，另一 UID 不可读其会话/历史。全包 race/vet 通过。此链不能称 DC Redis/PG 原生 bearer 已联验。

最终独立绑定日志在 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-h01-account-link.coc8Yf/go-test.log`；真实 RTW 知识链日志在 `/var/folders/f_/l5hv3b1d6sx8zwr_cc8fkjkm0000gn/T/sea-knowledge-acceptance.d8Ruyr/test.log` 和同目录 `user-test.log`。`TestRealHTTPKnowledgeWorkflowWithUserCenter` 26.33 秒通过；独立 PG 并发、HTTP 和 DC 合同反例全部通过；`go mod verify` 与 `git diff --check` 退出 0。验收目录保留但脚本启动的 PG 与 RTW 子进程已停止。已检查进程日志，无 `wh_access_` 或 JWT 字符串匹配。

**时间边界**：DC `auth/me` 在每次交换时检查当前 bearer；DC 会话撤销、换号或 RTW 解绑后，**下一次交换**会拒收。交换前验证与签发之间仍有跨服务时序窗。已签发的普通 RTW JWT 没有 DC 会话内省或绑定修订校验；即使 DC 随后撤销或解绑，现有知识路由仍可能接受它直到最多 120 秒到期。RTW UID 停用仍会由现有知识 User RPC 门禁即时拒绝。要实现 DC 撤销的即时产品阻断，需要另立跨服务会话/撤销合同；WhaleHall Bun 在当前账号代次变化时必须取消未决操作并清除旧 JWT，不能拿测试固定 JWT 进生产。

真实 DC `nativeauth`/Redis/PG 到 RTW 的同次联验、正式账户恢复/跨 UID 迁移政策、桌面 Bun 与网页同历史、线上配置/发布尚未完成；H01/WS04-A 仍为 `PARTIAL`。
