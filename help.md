# 代码生成速查（goctl）

按域列出常用生成命令。RPC 从 `proto/` 生成到 `service/<域>/rpc`；HTTP API 从 `api/*.api` 生成到 `service/<域>/api`。样式沿用各域现状（`gozero`/`go_zero` 并存系历史原因，新代码建议统一 `gozero`）。

## RPC（proto → zrpc）

```bash
# user（含 admin/user 子服务，admin 生成到 service/user/admin/rpc）
goctl rpc protoc ./proto/user.proto --go_out=./service/user/rpc --go-grpc_out=./service/user/rpc --zrpc_out=./service/user/rpc --style=gozero
goctl rpc protoc ./proto/admin.proto --go_out=./service/user/admin/rpc --go-grpc_out=./service/user/admin/rpc --zrpc_out=./service/user/admin/rpc --style=gozero

# 社区互动域
goctl rpc protoc ./proto/article.proto --go_out=./service/article/rpc --go-grpc_out=./service/article/rpc --zrpc_out=./service/article/rpc --style=gozero
goctl rpc protoc ./proto/comment.proto --go_out=./service/comment/rpc --go-grpc_out=./service/comment/rpc --zrpc_out=./service/comment/rpc --style=gozero
goctl rpc protoc ./proto/like.proto --go_out=./service/like/rpc --go-grpc_out=./service/like/rpc --zrpc_out=./service/like/rpc --style=gozero
goctl rpc protoc ./proto/favorite.proto --go_out=./service/favorite/rpc --go-grpc_out=./service/favorite/rpc --zrpc_out=./service/favorite/rpc --style=gozero
goctl rpc protoc ./proto/follow.proto --go_out=./service/follow/rpc --go-grpc_out=./service/follow/rpc --zrpc_out=./service/follow/rpc --style=gozero
goctl rpc protoc ./proto/message.proto --go_out=./service/message/rpc --go-grpc_out=./service/message/rpc --zrpc_out=./service/message/rpc --style=gozero

# 任务与热度
goctl rpc protoc ./proto/task.proto --go_out=./service/task/rpc --go-grpc_out=./service/task/rpc --zrpc_out=./service/task/rpc --style=gozero
goctl rpc protoc ./proto/hot.proto --go_out=./service/hot/rpc --go-grpc_out=./service/hot/rpc --zrpc_out=./service/hot/rpc --style=go_zero

# 积分与安全
goctl rpc protoc ./proto/points.proto --go_out=./service/points/rpc --go-grpc_out=./service/points/rpc --zrpc_out=./service/points/rpc --style=go_zero
goctl rpc protoc ./proto/security.proto --go_out=./service/security/rpc --go-grpc_out=./service/security/rpc --zrpc_out=./service/security/rpc --style=gozero
```

## HTTP API（.api → handler/logic/types）

```bash
# 社区与后台
goctl api go -api api/user_center.api -dir service/user/api -style=gozero
goctl api go -api api/admin_center.api -dir service/user/admin/api -style=gozero
goctl api go -api api/article.api -dir service/article/api -style=gozero
goctl api go -api api/comment.api -dir service/comment/api -style=gozero
goctl api go -api api/favorite.api -dir service/favorite/api -style=gozero
goctl api go -api api/follow.api -dir service/follow/api -style=gozero
goctl api go -api api/like.api -dir service/like/api -style=gozero
goctl api go -api api/message.api -dir service/message/api -style=gozero
goctl api go -api api/hot.api -dir service/hot/api -style=gozero
goctl api go -api api/task.api -dir service/task/api -style=gozero

# 知识域（api/knowledge.api 为唯一契约源；域内为纯域包结构，生成物位置见 service/knowledge/README.md）
```

## 注意事项

- 生成前先确认目标 `etc/` 配置与 `internal/svc` 装配不被覆盖；goctl 对已存在文件默认跳过或需 `-migrate`。
- knowledge 域不设 RPC（API-only 用例不为目录对称额外增加 RPC），契约见 `api/knowledge.api` 与 `service/knowledge/README.md`。
- proto 契约说明见 [proto/readme.md](proto/readme.md)。
