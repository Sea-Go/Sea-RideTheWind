package types

type KafkaLikeMsg struct {
	MsgID      string `json:"msg_id"`
	UserId     int64  `json:"user_id"`
	TargetType string `json:"target_type"`
	TargetId   string `json:"target_id"`
	AuthorID   int64  `json:"author_id"`
	State      int32  `json:"state"`
	IsFirst    bool   `json:"is_first"`
	CreatedAt  int64  `json:"created_at"`
}

type ArticleHotEvent struct {
	ArticleID string `json:"article_id"`
	Type      string `json:"type"`
	UserId    string `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
	IsFirst   bool   `json:"is_first"`
}

type TaskArticleProgressMsg struct {
	UserID string `json:"user_id"`
	Cur    int64  `json:"cur"`
}
