# citation

C-4 引用接纳（citation acceptance）的纯函数域层：把答案的候选引用变成对冻结修订结构树验证过、去重后的接纳回执（Receipt）。

## 职责

- `Accept(tree, source, req)`：纯函数校验并接纳一批引用，任一失败即整体报错、全部通过才产出 `Receipt`。校验项：
  - `AnswerID` / `SearchID` 非空；引用数 1..`MaxCitations`（32）；
  - 每条 `RevisionID` 必须等于 `tree.RevisionID`（防跨修订错配）；
  - `DocKey` 必须匹配 `(source|page|summary):<uuid>@<rev>`，rev 非空且不含 `@`/空白（`ValidateDocKey`）；
  - quote 经 `structure.Anchor` 验证锚定到段落字符区间（委托语义校验，不重复实现）。
  - 错误信息含 1-based 引用序号 + 原因。
- 去重：按 `(Anchored.NodeID, CharStart, CharEnd)` 三元组合并指向同一证据的重复引用，不报错，保留首次出现顺序。
- `TreeSHA = hex(sha256(canonical tree JSON))`：canonical 序列化为本包自实现——每条记录一行紧凑 JSON（先 revision 头、再按文档序每节点一行）、字段固定顺序、无缩进、`\n` 换行、最小字符串转义（仅转义引号/反斜杠/控制字符，不做 HTML 转义）；不依赖 `encoding/json` 的实现行为（map 序、HTML 转义默认值），保证指纹永远可复算。

## 边界（不能做什么）

- 不做持久化：Receipt 落库与 DB 装配在后续里程碑完成，本包只产出内存值。
- 不接触网络/时钟/随机源：纯域层，唯一依赖是 `structure`。
- 不重新派生结构树：只消费调用方传入的 `tree` 与派生它的同一份 `source` 字节。
- 不校验 DocKey 内嵌 `<rev>` 与 `Citation.RevisionID` 的一致性（两者是独立字段，规格未要求联动）。

## 确定性声明

同 `(tree, source, req)` 永远产出深相等的 `Receipt`（含 `TreeSHA`）；`TreeSHA` 只由 tree 决定，任何人可用同一棵树复算，作为验收记录的抗抵赖指纹。

## 验收

`GOCACHE=/tmp/gocache-s1 go test -race -count=1 ./service/knowledge/internal/citation/...`
