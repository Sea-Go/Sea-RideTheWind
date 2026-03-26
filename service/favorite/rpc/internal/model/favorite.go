package model

import (
	"context"

	"gorm.io/gorm"
)

type FavoriteModel struct {
	conn *gorm.DB
}

func NewFavoriteModel(db *gorm.DB) *FavoriteModel {
	return &FavoriteModel{conn: db}
}

func (m *FavoriteModel) InsertFolder(ctx context.Context, folder *FavoriteFolder) error {
	return m.conn.WithContext(ctx).Create(folder).Error
}

func (m *FavoriteModel) FindFoldersByUserId(ctx context.Context, userId int64) ([]FavoriteFolder, error) {
	var folders []FavoriteFolder
	err := m.conn.WithContext(ctx).
		Where("user_id = ?", userId).
		Order("create_time desc").
		Find(&folders).Error
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (m *FavoriteModel) FindFolderByFolderId(ctx context.Context, folderId int64) (*FavoriteFolder, error) {
	var folder FavoriteFolder
	err := m.conn.WithContext(ctx).Where("folder_id = ?", folderId).First(&folder).Error
	if err == nil {
		return &folder, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *FavoriteModel) FindFolderByUserIdAndName(ctx context.Context, userId int64, name string) (*FavoriteFolder, error) {
	var folder FavoriteFolder
	err := m.conn.WithContext(ctx).
		Where("user_id = ? AND name = ?", userId, name).
		First(&folder).Error
	if err == nil {
		return &folder, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *FavoriteModel) UpdateFolderNameByFolderId(ctx context.Context, folderId int64, name string) error {
	return m.conn.WithContext(ctx).
		Model(&FavoriteFolder{}).
		Where("folder_id = ?", folderId).
		Update("name", name).Error
}

func (m *FavoriteModel) DeleteFolderByFolderId(ctx context.Context, folderId int64) error {
	return m.conn.WithContext(ctx).Where("folder_id = ?", folderId).Delete(&FavoriteFolder{}).Error
}

func (m *FavoriteModel) DeleteFolderCascade(ctx context.Context, folderId int64) error {
	return m.conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("folder_id = ?", folderId).Delete(&FavoriteItem{}).Error; err != nil {
			return err
		}
		return tx.Where("folder_id = ?", folderId).Delete(&FavoriteFolder{}).Error
	})
}

func (m *FavoriteModel) InsertFavorite(ctx context.Context, favorite *FavoriteItem) error {
	return m.conn.WithContext(ctx).Create(favorite).Error
}

func (m *FavoriteModel) FindFavoritesByFolderId(ctx context.Context, folderId int64) ([]FavoriteItem, error) {
	var favorites []FavoriteItem
	err := m.conn.WithContext(ctx).
		Where("folder_id = ?", folderId).
		Order("create_time desc").
		Find(&favorites).Error
	if err != nil {
		return nil, err
	}
	return favorites, nil
}

func (m *FavoriteModel) FindFavoriteByFavoriteId(ctx context.Context, favoriteId int64) (*FavoriteItem, error) {
	var favorite FavoriteItem
	err := m.conn.WithContext(ctx).Where("favorite_id = ?", favoriteId).First(&favorite).Error
	if err == nil {
		return &favorite, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *FavoriteModel) FindFavoriteByFolderTarget(ctx context.Context, folderId int64, targetId, targetType string) (*FavoriteItem, error) {
	var favorite FavoriteItem
	err := m.conn.WithContext(ctx).
		Where("folder_id = ? AND target_id = ? AND target_type = ?", folderId, targetId, targetType).
		First(&favorite).Error
	if err == nil {
		return &favorite, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, ErrorNotFound
	}
	return nil, err
}

func (m *FavoriteModel) DeleteFavoriteByFavoriteId(ctx context.Context, favoriteId int64) error {
	return m.conn.WithContext(ctx).Where("favorite_id = ?", favoriteId).Delete(&FavoriteItem{}).Error
}

func (m *FavoriteModel) DeleteFavoritesByFolderId(ctx context.Context, folderId int64) error {
	return m.conn.WithContext(ctx).Where("folder_id = ?", folderId).Delete(&FavoriteItem{}).Error
}
