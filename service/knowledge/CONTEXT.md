# Knowledge 与 Wiki 人工质量

RTW Knowledge 管理不可变资料与 Wiki 修订、编辑 head 和管理员发布。人工事实评阅描述一个修订中的证据质量，不改写页面或发布状态。

## Language

**来源事实**：
管理员从一个不可变资料修订的真实段落中选定的事实主张。段落与原文摘录共同确定被评阅的事实。
_避免_：检索分块、模型主张

**Wiki 事实评阅**：
管理员对一个来源事实在某个不可变 Wiki 修订中的表现所作的判断。目标可以是 AI 接纳修订，也可以是随后的人手改稿。
_避免_：作业回执、模型置信度、检索相关性标签

**事实类别**：
来源事实与目标 Wiki 修订之间的关系：已覆盖、缺失、冲突或无法判断。
_避免_：发布批准

**覆盖等级**：
管理员对事实表达完整性与引用忠实度的四级评价。零级表示缺失或冲突；无法判断时没有等级。
_避免_：置信分、发布阈值

**评阅修订**：
针对同一 Wiki 修订和同一来源事实追加的一次重新判断。它独立于 Wiki 正文的编辑修订。
_避免_：Wiki 修订、Release 版本

**获准来源范围**：
RTW 从 AI 已接纳 Compile 的完整 SourceRevisionID 集，或从人工 Wiki 同页 base 链的当前可用 SourceRefs 与 AI 祖先完整资料集导出的范围。管理员申报的来源清单必须逐项等于这个范围；正式撤回的祖先资料只退出新范围，历史范围保持原字节。
_避免_：只摘当前 Wiki 引用、任意挑选若干事实形成高覆盖率

**FactSet 修订**：
管理员针对固定 Wiki 修订和获准来源范围追加的一份预期事实清单，记录各来源事实的原句 byte span、是否必需和矛盾分组。`facts_complete` 是管理员对所列范围已审全的声明，RTW 逐项校验来源，不能机械证明没有遗漏。
_避免_：把已到达的单事实评阅数当事实全集

**FactSet head**：
按模块、页面和获准来源范围独立 CAS 的最新清单指针。同范围的新人工 Wiki 清单可以推进它；历史 AI Wiki 的评测必须钉历史 FactSetRevisionID、目标 WikiRevisionID 与 JCS，不能读取当前 head 冒充旧版本。
_避免_：Wiki 编辑 head、已发布 Release

**编辑 head**：
某模块页面目前最新的可编辑 Wiki 修订。head 前移后，先前获接纳的修订仍可作为历史评阅目标。
_避免_：已发布 Release

**已发布 Release**：
管理员启用并交给检索与问答读者的知识版本。人工事实评阅不会启用它。
_避免_：编辑 head、已接纳 Compile

```text
固定AI Compile完整资料 / 人工Wiki同页当前可用base资料
  → RTW原Source UTF-8对象和段内引文byte span逐项核验
  → 管理员声明完整的FactSet修订 + 独立scope head CAS
  → 同事务原EventSpec Outbox与原字节/JCS回查表
  → DC连续offset → BTW同一消费者Catalog ODS落行后ACK
  → 历史FactSetRevisionID + 同目标WikiRevisionID + 每个必需Fact当前判断
  → 资格和真人事实复核齐备才可评估页面质量

Wiki编辑head与手动Release沿原有业务链，FactSet事务不推进它们。
```
