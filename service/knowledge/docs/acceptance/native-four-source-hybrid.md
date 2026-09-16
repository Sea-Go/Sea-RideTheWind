# Native Hybrid 四来源发布与橙色独有发现同父物理验收

日期：2026-09-16。RTW 分支 `test/search-native-four-source-holder-20260916`（基知识开发 `a07cd67`），BTW 固定源 `feat/search-native-three-lane-api-20260916@f6d9ea959d896d6b0d4d7cbc2d8fece379d9c4a0`（工作树洁净），DC 生产开发 `feat/platform-observability-20260914@ebea5f8`。本子链 **`INTEGRATED`**（受控本机物理轮）；正式 Standalone 三 Milvus、真人 qrel、生产部署另签，H02/H06/H07 整体仍 **`PARTIAL`**。

这是 Milvus Lite 3.2.1 稀疏 BM25 红证（总账第191节）之后的**新 Hybrid 真父轮**：稠密/token 多向量走同一任务 Lite 3.2.1 的两个独立 HNSW Collection，学习型稀疏走内容寻址 Shard 的真实冻结 weighted posting lists 按 IP 召回，不再让 Lite 先按 BM25 裁候选。

## 链路

1. DC 集成树 `BGE_HOLD_FOR_CONSUMER=1 bash scripts/test-bge-representations.sh` 起隔离 PG16 与锁定 BGE-M3 Provider，控制面注册 dense/sparse/token_matrix 三个真实配置并经网关完成 6 次表示调用后，写出 0600 `runtime.json` 并保活等待消费者 release file。
2. `bash service/knowledge/scripts/native-four-source-hybrid-acceptance.sh` 自起新 RTW PG16，`SEA_RTW_NATIVE_FOUR_SOURCE=1` 驱动 `TestRealHTTPNativeFourSourceWorkflow`：创建独立模块、四个授权 SourceRevision（apple/banana/orange/coffee 固定合成文）、四源 Release（`realBGEProfiles` 取自 DC runtime 三个配置）、Build 认领。
3. 固定 Python（uv 缓存环境，milvus-lite==3.2.1 + faiss-cpu==1.15.0，包根 SHA=`d713528aff96e15310ec764e153d4e046da93644820b7eafdfbb2623a571524c`）启动 BTW `native_lite_owner.py`，runtime.json 0600 发布后由 `readRealNativeLiteRuntime` 逐字段核验（目录经 `filepath.EvalSymlinks` 对齐 owner 的 resolve 语义）。
4. RTW 用四源原字节 chunk manifest 调 BTW build-only 子进程：真 DC BGE 编码四 chunk，Dense/Multi 投影到两个 Lite HNSW Collection，Sparse 只写冻结 posting Shard，产出 `test_projection_before_RTW_READY` 收据与三路不可变 Ref。
5. RTW 核 manifest/Ref/Probe 后收 READY，管理员显式激活发布指针 revision 1，Worker snapshot 确认 4 个有效修订与三 Ref 一致。
6. BTW 子进程 `TestRTWNativePublishedIndependentMultiDiscovery` 按本轮 RevisionID/ChunkID 动态映射，经同一 Lite 双 Collection 与同 Release 冻结 Sparse Ref Load/Query：query `apple pie recipe` 各路 TopK2 得 Dense=[apple,banana]、Sparse=[apple]、Multi=[apple,orange]——**Orange 不入 Dense/Sparse TopK、仅 Multi 命中**；Orange 原文经 RTW Worker 同修订 SourceReader 与当前撤回位核验后写出 0600 见证收据。
7. 父级对账双收据：builder `native_projection` 与已发布见证的 `settings_jcs_sha256` 同为 `46f2b838dc846dd1be55bfd8a7156fc266a17829549c8287783f3391f45cec28`（RTW 侧独立 JCS 重算），`sparse_backend=frozen_ip_postings`、`sparse_examined_postings=1` 同轮核验后写入父证据。
8. 正式 `cmd/api` 以 `BTW_SEARCH_MODE=native-hybrid` 独立编译启动（race），启动门禁核 Lite runtime 与投影收据的 Runtime/EnginePackage/SettingsJCS 三 SHA 同源；`fast/low` 产品搜索经签名范围转发到正式 socket，答案/引用入 RTW PG，同键重放与固定 GET 一致，BTW 仅收到首次调用；legacy 模块发布指针在整段交接前后不变。

## 证据

- 父脚本退出码 0；`go test -mod=readonly -race -p=1 -count=1` PASS（63.82s），`go vet`、`git diff --check` 顶层 0。
- test.log SHA256=`2fd541bf31d963cfdca3332d7f0d7ee3be7489f10442420655cc1152ab455536`；父证据 `native-four-source-parent.json` SHA256=`bc3ea450361a39e4026cbdccf802d5289a0421940fe042bf3856784a5749891d`；见证 `native-independent-result.json` SHA256=`baeac18057baf45d386bea0c0a1b987eabee636c4ae6228b68b9c7641c11c2b3`（与父报告 `witness_sha256` 一致）；构建收据 `native-index-result.json` SHA256=`a29244502eecd9c2862390278152b6c74dd9c5503bd01a17b2d0a3510892ad73`。
- 橙色原文 quote SHA=`ef2b6aa59460b2e22c73f04362e82b87ea615b6ac007db832f7eebedc986a971` 与原始对象 SHA 一致，locator `paragraph:1`；产品 search=`search_7903be9a-cf84-462f-a2ff-7ff6358248da`、answer=`answer_6a3feb56-6326-45d6-aec1-07ee9994ff20`。
- 纯夹具契约测试 `TestNativeFourSourceSelectorRequiresOneFixedBuilderAndPrivateBGE`/`TestNativeLiteRuntimeRequiresTheV2PackageOwner`/`TestNativeSettingsUseTheFormalLowercaseLiteContract` race PASS。
- DC 保活进程经 release file 释放，原 DC 真实模型测试退出码 0、Provider/PG 等待退出。

## 本轮发现并修复的 Holder 缺口

- 四源模式强制同一 BTW 树作 builder 与正式 API 源，但 legacy 流程的正式 socket 门禁未排除四源模式（该模式 legacy 模块刻意保留结构化索引），已在 `SEA_BTW_SEARCH_API_SOCKET_ROOT` 门禁上排除 `nativeFourSource`。
- 四源交接曾假设 legacy 指针停在首个 release；实际 readers 校验会发布第二 release 并激活。现改为交接开始时自取当前指针作基线，仅断言整段交接前后不变。
- macOS `TMPDIR` 符号链接使 Python owner 以 `/private/var/...` 发布 runtime 目录，Go 校验用未解析路径失配；现以 `filepath.EvalSymlinks` 对齐后再读。
- 四源模式跳过 legacy 流程末尾"停用户 RPC 断言 503"的降级阶段（该行为由普通 `TestRealHTTPKnowledgeWorkflow` 覆盖），否则产品搜索在用户校验处 503。

## 边界

`physical_qualified=false`、`qrel_evaluable=false`、`production_verified=false` 继续如实记录：合成四来源不是真人语料，见证据不宣相关性；Milvus Standalone 三路、全站语料、正式三路物理资格、详搜/高档十二格、Collector 下钻、规模成本/SLO 与生产部署仍需独立交接。测试 Admin/产品主体经测试 User RPC，非实时 UserCenter UID。
