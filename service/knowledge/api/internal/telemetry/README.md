# 知识服务观测装配

执行 OBS-2026-09-14-r1。工作区域：[W0] RTW 独立知识服务开发树；[W1] `service/knowledge/`、必要根依赖；[R1] Sea-Docs 和其他服务；[D1] go-zero 1.10.2、OTel 1.44.0 模块缓存；[G1] goctl routes/types 保持生成器维护；[X1] 仅任务专属 PostgreSQL/Collector；[N1] 原始 checkout、其他仓库及其修改；[T1] 独立验收目录。主职责 C6；跨区为领域提交、Outbox 关联和 HTTP 终态。

本包拥有唯一 logx Writer、Trace Provider 和指标 Registry。领域返回错误，由最接近提交边界的用例记录一次完整错误；HTTP 仅补请求终态。日志、Trace、Prometheus 均不替代知识状态或 Outbox。
