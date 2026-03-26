package model

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AdminModel struct {
	conn *gorm.DB
}

func NewAdminModel(db *gorm.DB) *AdminModel {
	return &AdminModel{
		conn: db,
	}
}

func (m *AdminModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.conn.WithContext(ctx).Transaction(fn)
}

func (m *AdminModel) FindOneUserByUid(ctx context.Context, uid int64) (*User, error) {
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

func (m *AdminModel) FindOneUserByUsername(ctx context.Context, username string) (*User, error) {
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

func (m *AdminModel) DeleteOneUserByUid(ctx context.Context, uid int64) error {
	result := m.conn.WithContext(ctx).Where("uid = ?", uid).Delete(&User{})
	if result.RowsAffected == 0 {
		return ErrorNotFound
	}
	return result.Error
}

func (m *AdminModel) UpdateOneUserByUid(ctx context.Context, uid int64, newUser *User) error {
	err := m.conn.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Updates(newUser).Error
	return err
}

func (m *AdminModel) FindOneAdminByUid(ctx context.Context, uid int64) (*Admin, error) {
	var admin Admin
	err := m.conn.WithContext(ctx).Where("uid = ?", uid).First(&admin).Error
	if err == nil {
		return &admin, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *AdminModel) FindOneAdminByUsername(ctx context.Context, username string) (*Admin, error) {
	var admin Admin
	err := m.conn.WithContext(ctx).Where("username = ?", username).First(&admin).Error
	if err == nil {
		return &admin, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *AdminModel) FindOneAdminByUsernameTx(tx *gorm.DB, username string) (*Admin, error) {
	var admin Admin
	err := tx.Where("username = ?", username).First(&admin).Error
	if err == nil {
		return &admin, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *AdminModel) UpdateOneAdminByUid(ctx context.Context, uid int64, newAdmin *Admin) error {
	err := m.conn.WithContext(ctx).Model(&Admin{}).Where("uid = ?", uid).Updates(newAdmin).Error
	return err
}

func (m *AdminModel) UpdateUserStatusByUid(ctx context.Context, uid int64, status int64) error {
	result := m.conn.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Update("status", status)
	if result.RowsAffected == 0 {
		return ErrorNotFound
	}
	return result.Error
}

func (m *AdminModel) UpdateUserPasswordByUid(ctx context.Context, uid int64, newPassword string) error {
	err := m.conn.WithContext(ctx).Model(&User{}).Where("uid = ?", uid).Update("password", newPassword).Error
	return err
}

func (m *AdminModel) InsertOneAdmin(ctx context.Context, admin *Admin) error {
	err := m.conn.WithContext(ctx).Create(admin).Error
	return err
}

func (m *AdminModel) InsertOneAdminTx(tx *gorm.DB, admin *Admin) error {
	return tx.Create(admin).Error
}

func (m *AdminModel) InsertOneAdminInvite(ctx context.Context, invite *AdminInvite) error {
	return m.conn.WithContext(ctx).Create(invite).Error
}

func (m *AdminModel) FindAdminInviteByCodeForUpdate(tx *gorm.DB, code string) (*AdminInvite, error) {
	var invite AdminInvite
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", code).First(&invite).Error
	if err == nil {
		return &invite, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *AdminModel) UpdateAdminInviteUsedTx(tx *gorm.DB, inviteID uint64, usedByUid int64, usedAt time.Time) error {
	return tx.Model(&AdminInvite{}).Where("id = ?", inviteID).Updates(map[string]any{
		"used_by_uid": usedByUid,
		"used_at":     usedAt,
	}).Error
}

func (m *AdminModel) FindUserListByKeyword(ctx context.Context, page int64, pageSize int64, keyword string) ([]*User, int64, error) {
	var users []*User
	var total int64
	db := m.conn.WithContext(ctx).Model(&User{})
	if len(keyword) > 0 {
		keyWord := "%" + keyword + "%"
		db = db.Where("username LIKE ? OR email LIKE ?", keyWord, keyWord)
	}
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	err := db.Order("id desc").Offset(int(offset)).Limit(int(pageSize)).Find(&users).Error
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}
