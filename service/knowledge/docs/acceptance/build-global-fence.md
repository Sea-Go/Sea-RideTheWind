# H04/H06：同一知识 Build 的跨 DC job fence

日期：2026-09-14。RTW 独立分支 `feat/knowledge-build-fence-20260914` 基于 `776d39b`，BTW 配套分支 `feat/build-fence-handoff-20260914` 基于 `8e1f56a`；DC 技术 job 服务采用现有 `e8c8cd68`，无 DC 源码变更。当前结果为**真实隔离三仓 H04/H06 子链通过，H06 整体仍 PARTIAL**。

RTW 是同一 `build_id` 的全局 lease epoch 唯一分配者。Worker 以 `ClaimBuild(lease_epoch=0,attempt_id,lease_expires_at,generation,manifest_hash,cancel_version)` 请求授权；RTW 在模块和 build 行锁下将新 attempt 分配为当前 build epoch 的严格下一值。相同且仍活跃的 attempt 重投不加代，expiry 可在原 fence 上单调延长；缩短、过期复活、跳号及旧 attempt 的 READY/FAILED 结果均拒绝。新 DC job 的 epoch 1 不直接充当 RTW epoch；旧正数请求仅作严格当前/下一代兼容，不能自行跳代。新授权可以抢占旧的准备/索引 attempt，使随后到达的旧结果在 `AcceptBuild` 按当前 fence 拒收，无需让 Prepare→Index 两个独立 DC job 等待原技术 lease 自然耗尽。

BTW 把 DC job 租约和 RTW build fence 分别持久保存：RTW 分配值进入本地内容 Build、Graph 与交接 Outbox，DC 原 attempt/epoch 只进入技术 CompleteJob。交接顺序保持本地三路 READY/outbox→RTW 当前 fence 下同 Ref READY→DC 当前技术 job ACK→outbox delivered。RTW 同 Ref READY 的新 DC 技术 job可在不重写不可变索引的前提下重投；新的 RTW fence 必须清除旧接纳标记并再次核对，不能双签。RTW 当前 `knowledge_publications` 仍只由管理员激活产生，READY 不自动发布。

按以下 opt-in 在本 RTW 独立树运行真实 User Center/RTW PG/BTW IndexWorker/DC jobs 联验：

```bash
KNOWLEDGE_KEEP_EVIDENCE=1 KNOWLEDGE_REAL_USER_GATE=1 \
SEA_BTW_INDEX_CONSUMER_ROOT=<BTW配套独立工作树绝对路径> \
SEA_DC_JOB_PLATFORM_ROOT=<DC独立工作树绝对路径> \
bash service/knowledge/scripts/acceptance.sh
```

最终完整脚本**退出码 0**：真实 User Center 与 gRPC 替身场景均 PASS，测试父进程直接核 RTW `knowledge_builds`/唯一 Outbox、DC 专属 PG `jobs.job`/`jobs.attempt`、相同 IndexManifest sha256、DC epoch 1 和 RTW build epoch 2。DC 真 `cmd/platform -migrate`、RTW 真 go-zero HTTP、BTW 真 IndexWorker/Graph 在不同进程运行并正常退出。RTW `ClaimBuild` 隔离 PG/race 单元还覆盖同 attempt 续租/丢回执、缩短/过期复活、两个新 job 并发递增与只有最终 fence 可接纳 READY；BTW content/worker 隔离脚本分别通过。

DC jobs 与其 PG 是真实技术任务服务，三路表示仍是固定 2 维 HTTP 夹具，local-exact 不是 Milvus；本次没有在同一三仓场景运行正式 `cmd/worker` CLI 或 DC 真 prepare job，也没有真实 BGE-M3、生产对象存储或 Collector。索引 READY 仅为隔离子合同，不扩大成 H06/全产品验收。第一次三仓测试仅因父测试误用 DC JSON `ref.hash` 而在业务成功后读取 NULL，已改为权威 `result_ref.sha256` 并按同一门禁复验通过；先前 ENOSPC 发生在 User Center race 链接期，未进入业务。
