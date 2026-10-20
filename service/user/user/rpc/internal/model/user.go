package model

import (
	"context"
	"github.com/Sea-Go/Sea-RideTheWind/service/common/paging"
	"strings"

	"gorm.io/gorm"
)

type UserModel struct {
	conn *gorm.DB
}

func NewUserModel(db *gorm.DB) *UserModel {
	return &UserModel{
		conn: db,
	}
}

func (m *UserModel) FindOneByUserName(ctx context.Context, username string) (*User, error) {
	var user User
	err := m.conn.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err == nil {
		return &user, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *UserModel) FindOneByUid(ctx context.Context, uid int64) (*User, error) {
	var user User
	err := m.conn.WithContext(ctx).Where("uid = ?", uid).First(&user).Error
	if err == nil {
		return &user, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *UserModel) UpdateUserById(ctx context.Context, uid int64, newUser *User) error {
	err := m.conn.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Updates(newUser).Error
	return err
}

func (m *UserModel) Insert(ctx context.Context, user *User) error {
	err := m.conn.WithContext(ctx).Create(user).Error
	return err
}

func (m *UserModel) DeleteUserByUid(ctx context.Context, uid int64) error {
	err := m.conn.WithContext(ctx).Where("uid = ?", uid).Delete(&User{}).Error
	return err
}

func (m *UserModel) CreateAvatarHistory(
	ctx context.Context,
	uid int64,
	historyID string,
	avatarURL string,
	contentType string,
	sizeBytes int64,
) (*UserAvatarHistory, *User, error) {
	var (
		user    User
		history UserAvatarHistory
	)

	err := m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("uid = ?", uid).First(&user).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrorNotFound
			}
			return err
		}

		if err := tx.Model(&UserAvatarHistory{}).
			Where("uid = ? AND is_current = ?", uid, true).
			Update("is_current", false).Error; err != nil {
			return err
		}

		history = UserAvatarHistory{
			HistoryID:   historyID,
			Uid:         uid,
			AvatarURL:   avatarURL,
			ContentType: contentType,
			SizeBytes:   sizeBytes,
			IsCurrent:   true,
		}
		if err := tx.Create(&history).Error; err != nil {
			return err
		}

		if user.ExtraInfo == nil {
			user.ExtraInfo = make(map[string]string)
		}
		user.ExtraInfo["avatar"] = avatarURL
		return tx.Model(&User{}).Where("uid = ?", uid).Updates(&User{
			ExtraInfo: user.ExtraInfo,
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}

	user.ExtraInfo["avatar"] = avatarURL
	return &history, &user, nil
}

func (m *UserModel) ListAvatarHistory(ctx context.Context, uid int64, limit int) ([]UserAvatarHistory, error) {
	limit = paging.Normalize(limit, 30, 30)

	var histories []UserAvatarHistory
	err := m.conn.WithContext(ctx).
		Where("uid = ?", uid).
		Order("create_time DESC").
		Limit(limit).
		Find(&histories).Error
	return histories, err
}

func (m *UserModel) SelectAvatarHistory(ctx context.Context, uid int64, historyID string) (*UserAvatarHistory, *User, error) {
	var (
		user    User
		history UserAvatarHistory
	)

	err := m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("uid = ?", uid).First(&user).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrorNotFound
			}
			return err
		}

		err := tx.Where("uid = ? AND history_id = ?", uid, historyID).First(&history).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrorAvatarHistoryNotFound
			}
			return err
		}

		if err := tx.Model(&UserAvatarHistory{}).
			Where("uid = ? AND is_current = ?", uid, true).
			Update("is_current", false).Error; err != nil {
			return err
		}

		if err := tx.Model(&UserAvatarHistory{}).
			Where("uid = ? AND history_id = ?", uid, historyID).
			Update("is_current", true).Error; err != nil {
			return err
		}

		if user.ExtraInfo == nil {
			user.ExtraInfo = make(map[string]string)
		}
		user.ExtraInfo["avatar"] = history.AvatarURL
		return tx.Model(&User{}).Where("uid = ?", uid).Updates(&User{
			ExtraInfo: user.ExtraInfo,
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}

	history.IsCurrent = true
	user.ExtraInfo["avatar"] = history.AvatarURL
	return &history, &user, nil
}

func NormalizeAvatarContentType(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "image/png"
	}
	return trimmed
}
