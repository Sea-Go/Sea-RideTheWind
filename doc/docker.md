**# Sea-RideTheWind Docker Compose 组件说明（含可视化面板与账号密码）

## 可视化面板（Web UI）入口（建议收藏）
> 下面均为 **宿主机访问地址**（即你运行 docker-compose 的机器上访问）。

| 面板/系统 | 入口（宿主机访问） | 默认账号/密码 | 用途 |
|---|---|---|---|
| **Grafana** | http://localhost:33000 | `admin / admin` | 指标可视化大盘（数据源通常接 Prometheus） |
| **Prometheus** | http://localhost:39090 | 无（默认不启用登录） | 指标查询、Targets 状态、规则与告警排查 |
| **Jaeger UI** | http://localhost:16686 | 无（默认不启用登录） | 分布式链路追踪查询与依赖分析 |
| **Kibana** | http://localhost:35601 | 无（本配置 `xpack.security.enabled=false`） | Elasticsearch 日志检索与可视化 |
| **Kafka UI** | http://localhost:38080 | 无（默认不启用登录） | Kafka 集群管理/Topic/Consumer/消息查看 |
| **RedisInsight** | http://localhost:35540 | 无（首次可能引导创建本地账号） | Redis 可视化管理、键浏览、慢查询等 |
| **Neo4j Browser** | http://localhost:37474 | `neo4j / Sea-TryGo` | Neo4j 图数据库 Web 管理与查询 |
| **NeoDash** | http://localhost:35005 | 无（面板内配置 Neo4j 连接） | Neo4j 仪表盘（图/表/查询面板） |
| **MinIO Console** | http://localhost:39001 | `minioadmin / minioadmin` | 对象存储管理台（Bucket/对象/策略/AccessKey） |
| **Flink Web UI** | http://localhost:38081 | 无（默认不启用登录） | Flink 作业管理、任务/算子监控、日志查看 |
| **cAdvisor UI** | http://localhost:38082 | 无（默认不启用登录） | 容器资源监控 UI（也常被 Prometheus 抓取 metrics） |

---

## 组件清单：作用与端口说明

### 1) etcd（服务发现 / 配置中心 / 元数据存储）
- **作用**
  - 强一致分布式 KV 存储：常用于服务发现、配置中心、分布式锁
  - 本栈中常作为 **Milvus 的元数据/协调依赖**（standalone 仍依赖 etcd）
- **端口**
  - `32379 -> 2379`：etcd Client API（组件/应用访问 etcd 的主要接口）

---

### 2) PostgreSQL（关系型数据库）
- **作用**
  - 结构化业务数据存储：用户/订单/配置/任务/审计等
- **端口**
  - `35432 -> 5432`：PostgreSQL 连接端口（psql/JDBC/ORM）
- **账号密码**
  - 用户：`admin`
  - 密码：`Sea-TryGo`
  - 数据库：`first_db`

---

### 3) Redis（缓存 / 键值存储）
- **作用**
  - 缓存、会话、分布式锁、计数器、轻量队列等
  - 本配置启用 `--appendonly yes`（AOF 持久化）
- **端口**
  - `36379 -> 6379`：Redis 客户端连接端口

---

### 4) RedisInsight（Redis 可视化管理）
- **作用**
  - Redis 官方 GUI：键空间浏览、命令执行、慢查询、监控等
- **端口**
  - `35540 -> 5540`：RedisInsight Web UI
- **账号密码**
  - 默认：**无**（首次进入可能引导创建本地账号，视版本/初始化流程而定）

---

### 5) Neo4j（图数据库）
- **作用**
  - 图数据存储与查询（Cypher）：知识图谱、关系网络、推荐、路径分析等
- **端口**
  - `37474 -> 7474`：Neo4j Browser（Web UI）
  - `37687 -> 7687`：Bolt 协议（应用驱动连接端口）
  - `32004 -> 2004`：Prometheus Metrics（需要 Neo4j 配置启用并暴露 metrics）
- **账号密码**
  - `neo4j / Sea-TryGo`

---

### 6) NeoDash（Neo4j 仪表盘）
- **作用**
  - 面向 Neo4j 的 Dashboard 工具：把 Cypher 查询做成图表/表格/页面
- **端口**
  - `35005 -> 5005`：NeoDash Web UI
- **账号密码**
  - 默认：**无**
  - 连接 Neo4j 时使用：`neo4j / Sea-TryGo`

---

### 7) Kafka（消息队列 / 事件流平台）
- **作用**
  - 事件总线、异步解耦、消息缓冲、流式数据管道
  - 常与 Flink 组合：Kafka 进、Flink 处理、下游落库/写 ES/写向量库等
- **端口**
  - `39092 -> 9092`：Kafka PLAINTEXT（Producer/Consumer 连接）
- **补充说明**
  - 本配置使用 KRaft（`broker,controller`），controller 监听 `9093`（未对外映射）

---

### 8) Kafka UI（Kafka 可视化管理）
- **作用**
  - 查看 Topic、消息、Consumer Group、Offset、Schema 等（依功能配置）
- **端口**
  - `38080 -> 8080`：Kafka UI Web
- **账号密码**
  - 默认：**无**（本 compose 未配置认证）

---

### 9) MinIO（对象存储，S3 兼容）
- **作用**
  - S3 兼容对象存储，适合保存大文件、模型文件、索引文件、向量库数据等
  - 本栈中 Milvus 依赖 MinIO 存储数据文件/索引
- **端口**
  - `39000 -> 9000`：S3 API（SDK/应用访问）
  - `39001 -> 9001`：MinIO Console（Web 管理台）
- **账号密码**
  - `minioadmin / minioadmin`

---

### 10) Milvus Standalone（向量数据库）
- **作用**
  - 向量数据存储与相似度检索（Embedding/ANN）
  - standalone 模式仍依赖：
    - **etcd**：元数据/协调
    - **MinIO**：对象存储
- **端口**
  - `19530 -> 19530`：Milvus gRPC（客户端主要连接端口）
  - `39091 -> 9091`：健康检查接口（`/healthz`）
- **依赖**
  - `depends_on`: `etcd`, `minio`

---

### 11) Elasticsearch（日志与搜索）
- **作用**
  - 日志/文档索引与检索；作为日志平台的存储与查询引擎
- **端口**
  - `39200 -> 9200`：Elasticsearch HTTP API
- **账号密码**
  - 默认：**无**（本配置 `xpack.security.enabled=false`，适合开发环境）

---

### 12) Kibana（Elasticsearch 可视化）
- **作用**
  - ES 数据检索、可视化图表、仪表盘与日志分析
- **端口**
  - `35601 -> 5601`：Kibana Web UI
- **账号密码**
  - 默认：**无**（对应 ES 未启用安全）

---

### 13) Flink JobManager（流计算管理节点）
- **作用**
  - Flink 的调度/管理节点：提交作业、查看运行状态、任务拓扑等
  - 已配置 Prometheus metrics reporter（容器内 `9249`）
- **端口**
  - `38081 -> 8081`：Flink Web UI（JobManager）

---

### 14) Flink TaskManager（流计算执行节点）
- **作用**
  - Flink 执行节点：实际运行 task/算子，提供计算资源（slot）
  - 同样配置 Prometheus metrics reporter（容器内 `9249`）
- **端口**
  - 无宿主机端口映射（通过 Docker 内部网络与 JobManager 通信）

---

## Observability（可观测性）组件

### 15) Prometheus（指标采集与查询）
- **作用**
  - 抓取各组件/exporter 指标，提供 PromQL 查询与告警规则能力
- **端口**
  - `39090 -> 9090`：Prometheus Web UI / HTTP API

---

### 16) Grafana（指标可视化）
- **作用**
  - 可视化平台：基于 Prometheus 等数据源构建监控大盘
- **端口**
  - `33000 -> 3000`：Grafana Web UI
- **账号密码**
  - `admin / admin`

---

### 17) Jaeger all-in-one（分布式追踪）
- **作用**
  - 采集与查询 trace/span，支持服务依赖图分析（适合开发/测试）
- **端口**
  - `16686 -> 16686`：Jaeger UI
  - `36831 -> 6831/udp`：Agent thrift compact
  - `36832 -> 6832/udp`：Agent thrift binary
  - `14268 -> 14268`：Collector HTTP
  - `14250 -> 14250`：Collector gRPC
  - `34317 -> 4317`：OTLP gRPC（OpenTelemetry）
  - `34318 -> 4318`：OTLP HTTP（OpenTelemetry）
- **账号密码**
  - 默认：**无**

---

### 18) node-exporter（宿主机指标）
- **作用**
  - 采集宿主机 CPU/内存/磁盘/网络等指标给 Prometheus
- **端口**
  - `39100 -> 9100`：node-exporter metrics

---

### 19) cAdvisor（容器指标）
- **作用**
  - 采集容器级资源使用指标（CPU/内存/IO/网络）给 Prometheus
- **端口**
  - `38082 -> 8080`：cAdvisor UI/metrics

---

## Exporters（把组件指标暴露给 Prometheus）

> 下面这些 exporter **没有对宿主机暴露端口**，通常由 Prometheus 在 Docker 网络中直接抓取。
> 如果你希望在宿主机也能访问 exporter（例如浏览 metrics），可以给它们补上 `ports:` 映射。

### 20) postgres-exporter（PostgreSQL 指标导出）
- **作用**
  - 将 PostgreSQL 运行指标转换为 Prometheus metrics
- **端口**
  - 默认容器内 metrics 端口：`9187`（本 compose 未映射到宿主机）

---

### 21) redis-exporter（Redis 指标导出）
- **作用**
  - 将 Redis 指标转换为 Prometheus metrics
- **端口**
  - 默认容器内 metrics 端口：`9121`（本 compose 未映射到宿主机）

---

### 22) kafka-exporter（Kafka 指标导出）
- **作用**
  - 导出 Kafka topic/partition/consumer group 等指标
- **端口**
  - 默认容器内 metrics 端口：`9308`（本 compose 未映射到宿主机）

---

### 23) elasticsearch-exporter（Elasticsearch 指标导出）
- **作用**
  - 导出 Elasticsearch 集群、节点、索引等指标
- **端口**
  - 默认容器内 metrics 端口：`9114`（本 compose 未映射到宿主机）

---

## 端口速查表（按宿主机端口）
| 宿主机端口 | 容器端口 | 组件 | 用途 |
|---:|---:|---|---|
| 32379 | 2379 | etcd | etcd Client API |
| 33000 | 3000 | grafana | Grafana Web UI |
| 35005 | 5005 | neodash | NeoDash Web UI |
| 35432 | 5432 | postgres | PostgreSQL 连接 |
| 35540 | 5540 | redisinsight | RedisInsight Web UI |
| 35601 | 5601 | kibana | Kibana Web UI |
| 36379 | 6379 | redis | Redis 连接 |
| 37474 | 7474 | neo4j | Neo4j Browser Web UI |
| 37687 | 7687 | neo4j | Bolt 协议 |
| 38080 | 8080 | kafka-ui | Kafka UI Web |
| 38081 | 8081 | flink-jobmanager | Flink Web UI |
| 38082 | 8080 | cadvisor | cAdvisor UI/metrics |
| 39000 | 9000 | minio | S3 API |
| 39001 | 9001 | minio | MinIO Console |
| 39090 | 9090 | prometheus | Prometheus Web UI |
| 39091 | 9091 | milvus | Healthz |
| 39092 | 9092 | kafka | Kafka PLAINTEXT |
| 39100 | 9100 | node-exporter | Host metrics |
| 39200 | 9200 | elasticsearch | ES HTTP API |
| 16686 | 16686 | jaeger | Jaeger UI |
| 14268 | 14268 | jaeger | Jaeger collector HTTP |
| 14250 | 14250 | jaeger | Jaeger collector gRPC |
| 34317 | 4317 | jaeger | OTLP gRPC |
| 34318 | 4318 | jaeger | OTLP HTTP |
| 36831/udp | 6831/udp | jaeger | agent thrift compact |
| 36832/udp | 6832/udp | jaeger | agent thrift binary |
| 19530 | 19530 | milvus | Milvus gRPC |

---

## 网络与数据卷（简要）
- **网络**
  - 默认网络命名：`sea-ridethewind`（所有服务加入同一 Docker 网络，容器间可用服务名互通）
- **数据卷（named volumes）**
  - `prometheus_data`：Prometheus 时序数据
  - `grafana_data`：Grafana 数据（数据源/仪表盘/用户等）
  - `redisinsight_data`：RedisInsight 数据
- **bind mounts（目录挂载）**
  - etcd/minio/milvus/neo4j 均有目录挂载，用于持久化数据与配置（见 compose 中 volumes 配置）

---
