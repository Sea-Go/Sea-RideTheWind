<div align="center">
  <img src="./logo.png" alt="Sea-TryGo" width="180" />

# Sea-RideTheWind

_基于 Go / go-zero 构建的现代化微服务业务后端框架._

> 面向内容、互动、任务与用户体系的一体化服务端工程实践。


</div>

---

## 前言
由于一些原因，在原先开源的仓库中发生了诸如审核不严格导致的垃圾 PR、以及部分敏感信息泄露等问题。为了更好地控制项目质量和安全，我们决定将仓库迁移到一个新的地址，并重新整理了项目结构和文档说明。

之后这个项目会在一段时间内进入持续维护的阶段，并大概在今年的暑假之前开启新的征程，届时欢迎大家继续关注和支持，也希望您通过进入下面的群聊加入我们。

项目资料与访问入口汇总

- 📄 [项目文档](https://my.feishu.cn/wiki/FWSkwcTKwiuCcGkzqwPcfx0Inuc?from=from_copylink )   密码：174w667#
- 👥QQ 交流群：750807478
- 🧩 [前端仓库](https://github.com/Sea-Go/Sea-RideTheWind-Fronted) / [推荐算法仓库](https://github.com/Sea-Go/Sea-BreakTheWaves)
- 🚀 [在线体验](sea-ridethewindbreakthewaves.xyz)
## 项目介绍
后端的 RideTheWind 与推荐系统的 BreakTheWaves 组合在一起，整体寓意为“乘风破浪”。

其中，传统服务侧的 RideTheWind 对应“乘风”。这里的“风”，一方面象征当前技术演进和基础设施持续完善所带来的外部条件与发展机遇，另一方面也代表我们能够借助成熟框架、通用能力和工程经验，更高效地推进系统建设与能力沉淀。之所以强调“乘风”，并不只是强调顺势而为，更重要的是希望在已有便利条件的基础上，通过亲自复刻、深入实现和持续打磨，真正理解系统设计背后的取舍逻辑与工程方法。只有经过完整的实践过程，才能更加准确地识别设计中的优点与不足，从而形成可复用、可演进的技术认知。

推荐系统侧的 BreakTheWaves 则对应“破浪”。这里的“浪”，主要指向以 AI 为代表的新一轮技术浪潮，以及由此带来的架构演进、能力升级和方法创新。面对快速变化的新技术环境，我们希望保持开放和主动的探索姿态，不局限于沿用既有路径，而是在业务场景中持续尝试新的技术方案，验证新方法的实际价值，并在探索过程中不断积累经验、沉淀能力。所谓“破浪”，强调的并非追逐概念本身，而是在技术趋势中找到与业务发展真正契合的方向，并通过持续实践取得切实成果。

这也刚好对应了"识海"这个社区的名字，我们在广阔的海洋中乘风破浪，并在此行程中有所认识。 
## 当前仓库内容概览

- 微服务目录：`service/user`、`service/article`、`service/comment`、`service/favorite`、`service/like`、`service/task`、`service/hot`、`service/points`、`service/security`
- API 网关定义：`api/*.api`
- RPC 协议定义：`proto/*.proto`
- 基础设施编排：`docker-compose.yaml`
- 容器与部署脚本：`dockerfile`、`manage.sh`
- 辅助文档：`doc/docker.md`、`help.md`、`proto/readme.md`


## 项目特性

- **模块拆分清晰**  
  以业务域为单位拆分服务，便于独立开发、测试与扩展。

- **接口定义完整**  
  同时提供 `api` 网关定义与 `proto` RPC 协议，适合前后端联调与服务间通信。

- **基础设施完善**  
  内置 PostgreSQL、Redis、Kafka、MinIO、Neo4j、Milvus、Elasticsearch、Flink 等组件的编排支持。

- **可观测性友好**  
  集成 Prometheus、Grafana、Jaeger、cAdvisor、node-exporter，方便指标、日志与链路排查。

- **便于容器化部署**  
  提供 `docker-compose.yaml`、`dockerfile` 与 `manage.sh`，便于在开发与测试环境快速落地。

- **适合业务中台演进**  
  当前结构适用于社区系统、内容平台、活动任务系统、用户成长体系等场景。

## 快速开始

可先阅读仓库中的部署与生成说明，再根据机器角色启动基础设施或业务服务。

### 1. 准备环境

建议先准备以下基础依赖：

- Go 1.25+
- Docker / Docker Compose
- `goctl`
- PostgreSQL / Redis / Kafka / MinIO 等基础设施（也可直接通过 Compose 启动）

### 2. 查看服务生成与维护说明

- Proto 生成说明：[`proto/readme.md`](./proto/readme.md)
- 常用生成命令：[`help.md`](./help.md)
- Docker 组件说明：[`doc/docker.md`](./doc/docker.md)

### 3. 启动基础设施

优先检查：

- `docker-compose.yaml`
- `manage.sh`

若采用分机器部署模式，可在 `manage.sh` 中配置：

- `NODE_ROLE=infra`：基础设施机器
- `NODE_ROLE=app`：业务服务机器
- `INFRA_HOST`：基础设施主机地址


## 链接

| 分类 | 说明 |
|:-:|:-:|
| API 定义 | [`api/admin_center.api`](./api/admin_center.api) · [`api/user_center.api`](./api/user_center.api) · [`api/article.api`](./api/article.api) |
| RPC 协议 | [`proto/user.proto`](./proto/user.proto) · [`proto/article.proto`](./proto/article.proto) · [`proto/comment.proto`](./proto/comment.proto) |
| 部署说明 | [`doc/docker.md`](./doc/docker.md) |
| 命令参考 | [`help.md`](./help.md) · [`proto/readme.md`](./proto/readme.md) |
| 运行脚本 | [`manage.sh`](./manage.sh) |

> 建议优先从 `doc/docker.md` 与 `manage.sh` 入手理解整套运行环境，再回到 `api` / `proto` 层做接口联调与服务扩展。

## 服务说明

### 用户与管理员体系

- `user_center.api`：用户注册、登录、查询、更新、注销
- `admin_center.api`：管理员登录、自身资料维护、用户管理、封禁与解禁等

### 内容与互动体系

- `article.api`：文章创建、编辑、删除、列表查询、图片上传
- `comment.api`：评论发布、查询、点赞、删除、审核与主题状态管理
- `like.api`：点赞 / 点踩、状态查询、点赞记录与获赞统计
- `favorite.api`：收藏夹与收藏内容管理

### 成长与运营体系

- `task.api`：任务查询
- `points.proto` / `hot.proto`：积分与热点相关能力
- `security.proto`：安全与风控相关服务能力预留

## 基础设施

根据 `doc/docker.md` 与当前编排文件，项目覆盖了如下核心依赖：

- **存储层**：PostgreSQL、Redis、MinIO、Neo4j、Milvus、Elasticsearch
- **消息与流处理**：Kafka、Flink
- **服务治理与元数据**：etcd
- **监控与追踪**：Prometheus、Grafana、Jaeger、node-exporter、cAdvisor
- **可视化面板**：Kibana、Kafka UI、RedisInsight、NeoDash、MinIO Console

## 推荐使用方式

- 将 `api` 作为 HTTP 入口层
- 将 `proto` 与 `service/*/rpc` 作为服务间调用基础
- 将 `docker-compose.yaml` 用于本地或测试环境基础设施拉起
- 将 `manage.sh` 作为多机部署与容器运行辅助脚本
- 将 `doc/docker.md` 作为运维与联调时的面板入口速查文档

## 致谢

- 感谢 Go、go-zero、gRPC、PostgreSQL、Redis、Kafka、Prometheus 等开源生态提供的工程基础。
- 感谢所有为项目设计、编码、测试与部署提供支持的人。
- 也感谢正在阅读这份文档的你。

---
## 参与人员名单及其贡献
由于原先仓库不方便放出来，现贡献展示如下：
![img.png](doc/image/img.png)
![img.png](doc/image/img1.png)
![img.png](doc/image/img2.png)
![img.png](doc/image/img3.png)
---

## 许可证

请根据仓库实际附带的许可证文件与第三方依赖许可证进行使用与分发。

如需对外发布、商用或二次开发，建议先统一补充：

1. 项目主许可证说明
2. 第三方依赖许可证归属说明
3. 图片、品牌标识与文档资源的使用范围说明