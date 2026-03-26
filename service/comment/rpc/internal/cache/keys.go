package cache

import "fmt"

func SubjectKey(subjectID string) string {
	return fmt.Sprintf("comment:subject:%s", subjectID)
}

func ReplyContentKey(commentID int64) string {
	return fmt.Sprintf("comment:content:%d", commentID)
}

func ReplyIndexPageKey(targetType, targetID string, rootID int64, sort string, offset, limit int) string {
	return fmt.Sprintf("comment:index:%s:%s:%d:%s:%d:%d",
		targetType, targetID, rootID, sort, offset, limit)
}

func CommentIndexKey(commentID int64) string {
	return fmt.Sprintf("comment:index:item:%d", commentID)
}
