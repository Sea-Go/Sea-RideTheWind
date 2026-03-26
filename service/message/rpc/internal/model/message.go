package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrRecordNotFound = gorm.ErrRecordNotFound

type MessageModel struct {
	conn *gorm.DB
}

func NewMessageModel(db *gorm.DB) *MessageModel {
	return &MessageModel{conn: db}
}

func (m *MessageModel) CreateNotifications(ctx context.Context, items []*Notification) error {
	if len(items) == 0 {
		return nil
	}
	return m.conn.WithContext(ctx).CreateInBatches(items, 200).Error
}

func (m *MessageModel) CountNotifications(ctx context.Context, recipientID int64, unreadOnly bool) (int64, error) {
	db := m.conn.WithContext(ctx).Model(&Notification{}).Where("recipient_id = ?", recipientID)
	if unreadOnly {
		db = db.Where("is_read = ?", false)
	}
	var count int64
	err := db.Count(&count).Error
	return count, err
}

func (m *MessageModel) ListNotifications(ctx context.Context, recipientID int64, offset, limit int32, unreadOnly bool) ([]Notification, int64, int64, error) {
	query := m.conn.WithContext(ctx).Model(&Notification{}).Where("recipient_id = ?", recipientID)
	if unreadOnly {
		query = query.Where("is_read = ?", false)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}

	var unread int64
	if err := m.conn.WithContext(ctx).Model(&Notification{}).
		Where("recipient_id = ? AND is_read = ?", recipientID, false).
		Count(&unread).Error; err != nil {
		return nil, 0, 0, err
	}

	var items []Notification
	db := query.Order("created_at desc")
	if offset > 0 {
		db = db.Offset(int(offset))
	}
	if limit > 0 {
		db = db.Limit(int(limit))
	}
	if err := db.Find(&items).Error; err != nil {
		return nil, 0, 0, err
	}

	return items, total, unread, nil
}

func (m *MessageModel) MarkNotificationRead(ctx context.Context, recipientID, notificationID int64) error {
	now := time.Now()
	res := m.conn.WithContext(ctx).Model(&Notification{}).
		Where("id = ? AND recipient_id = ?", notificationID, recipientID).
		Updates(map[string]interface{}{"is_read": true, "read_at": &now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRecordNotFound
	}
	return nil
}

func (m *MessageModel) MarkAllNotificationsRead(ctx context.Context, recipientID int64) error {
	now := time.Now()
	return m.conn.WithContext(ctx).Model(&Notification{}).
		Where("recipient_id = ? AND is_read = ?", recipientID, false).
		Updates(map[string]interface{}{"is_read": true, "read_at": &now}).Error
}

func (m *MessageModel) FindConversationByID(ctx context.Context, conversationID int64) (*Conversation, error) {
	var item Conversation
	if err := m.conn.WithContext(ctx).Where("id = ?", conversationID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *MessageModel) FindConversationByParticipants(ctx context.Context, role1 int32, id1 int64, role2 int32, id2 int64) (*Conversation, error) {
	var item Conversation
	if err := m.conn.WithContext(ctx).
		Where(
			"participant1_role = ? AND participant1_id = ? AND participant2_role = ? AND participant2_id = ?",
			role1, id1, role2, id2,
		).
		First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *MessageModel) ListConversations(ctx context.Context, role int32, id int64, offset, limit int32) ([]Conversation, int64, error) {
	query := m.conn.WithContext(ctx).Model(&Conversation{}).Where(
		"(participant1_role = ? AND participant1_id = ?) OR (participant2_role = ? AND participant2_id = ?)",
		role, id, role, id,
	)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []Conversation
	db := query.Order("last_message_at desc").Order("updated_at desc")
	if offset > 0 {
		db = db.Offset(int(offset))
	}
	if limit > 0 {
		db = db.Limit(int(limit))
	}
	if err := db.Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (m *MessageModel) CountUnreadConversationMessages(ctx context.Context, role int32, id int64) (int64, error) {
	var count int64
	err := m.conn.WithContext(ctx).Model(&ConversationMessage{}).
		Where("recipient_role = ? AND recipient_id = ? AND is_read = ?", role, id, false).
		Count(&count).Error
	return count, err
}

func (m *MessageModel) CountUnreadMessagesByConversation(ctx context.Context, conversationID int64, role int32, id int64) (int64, error) {
	var count int64
	err := m.conn.WithContext(ctx).Model(&ConversationMessage{}).
		Where(
			"conversation_id = ? AND recipient_role = ? AND recipient_id = ? AND is_read = ?",
			conversationID, role, id, false,
		).
		Count(&count).Error
	return count, err
}

func (m *MessageModel) ListConversationMessages(ctx context.Context, conversationID int64, offset, limit int32) ([]ConversationMessage, error) {
	var items []ConversationMessage
	db := m.conn.WithContext(ctx).Model(&ConversationMessage{}).
		Where("conversation_id = ?", conversationID).
		Order("created_at asc")
	if offset > 0 {
		db = db.Offset(int(offset))
	}
	if limit > 0 {
		db = db.Limit(int(limit))
	}
	if err := db.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (m *MessageModel) MarkConversationRead(ctx context.Context, conversationID int64, role int32, id int64) error {
	now := time.Now()
	return m.conn.WithContext(ctx).Model(&ConversationMessage{}).
		Where(
			"conversation_id = ? AND recipient_role = ? AND recipient_id = ? AND is_read = ?",
			conversationID, role, id, false,
		).
		Updates(map[string]interface{}{"is_read": true, "read_at": &now}).Error
}

func (m *MessageModel) CreateConversationWithMessageTx(ctx context.Context, conversation *Conversation, message *ConversationMessage) error {
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(conversation).Error; err != nil {
			return err
		}
		message.ConversationID = conversation.ID
		return tx.Create(message).Error
	})
}

func (m *MessageModel) CreateOrSendMessageTx(
	ctx context.Context,
	conversation *Conversation,
	message *ConversationMessage,
	statusAfterSend int32,
	pendingSenderRole int32,
	pendingSenderID int64,
	preview string,
) error {
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := message.CreatedAt
		if now.IsZero() {
			now = time.Now()
			message.CreatedAt = now
		}

		if conversation == nil {
			return errors.New("conversation is nil")
		}

		if err := tx.Model(&Conversation{}).Where("id = ?", conversation.ID).Updates(map[string]interface{}{
			"status":               statusAfterSend,
			"pending_sender_role":  pendingSenderRole,
			"pending_sender_id":    pendingSenderID,
			"last_message_id":      message.ID,
			"last_message_preview": preview,
			"last_message_at":      now,
			"updated_at":           now,
		}).Error; err != nil {
			return err
		}

		message.ConversationID = conversation.ID
		return tx.Create(message).Error
	})
}

func (m *MessageModel) TrimPreview(content string) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) <= 80 {
		return content
	}
	return string(runes[:80])
}
