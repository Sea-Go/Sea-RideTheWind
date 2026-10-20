// Package paging 提供列表分页参数的唯一归一实现（C46 单源：此前
// message/follow/like×2/user 各持一份同构拷贝）。
//
// 语义：limit 非正回退 def；超过 max 截断到 max（max<=0 表示不设上限）。
package paging

// Normalize 返回钳制在 (0, max] 内的 limit。
func Normalize[T ~int32 | ~int64 | ~int](limit, def, max T) T {
	if limit <= 0 {
		limit = def
	}
	if max > 0 && limit > max {
		limit = max
	}
	return limit
}

// NormalizeOffset 把负偏移归零（列表查询游标约定）。
func NormalizeOffset[T ~int32 | ~int64 | ~int](offset T) T {
	if offset < 0 {
		return 0
	}
	return offset
}
