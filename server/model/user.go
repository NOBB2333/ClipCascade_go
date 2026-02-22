// Package model 为 ClipCascade server 定义数据库模型。
package model

import (
	"time"

	"gorm.io/gorm"
)

// User 代表一个注册的 user 账户。
type User struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	Username  string         `gorm:"uniqueIndex;size:50;not null" json:"username"`
	Password  string         `gorm:"size:255;not null" json:"-"` // BCrypt hash
	Role      string         `gorm:"size:20;not null;default:USER" json:"role"`
	Enabled   bool           `gorm:"not null;default:true" json:"enabled"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// UserInfo 存储 login 期间提供的每个 user 的加密设置。
type UserInfo struct {
	ID         uint   `gorm:"primarykey" json:"id"`
	UserID     uint   `gorm:"uniqueIndex;not null" json:"user_id"`
	Salt       string `gorm:"size:255" json:"salt"`
	HashRounds int    `gorm:"default:100000" json:"hash_rounds"`
	MaxSize    int64  `gorm:"default:0" json:"max_size"` // client 请求的最大大小，0 = server 默认值
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IsAdmin 如果 user 具有 admin 角色，则返回 true。
func (u *User) IsAdmin() bool {
	return u.Role == "ADMIN"
}

// InitDB 初始化数据库连接并自动迁移模型。
func InitDB(db *gorm.DB) error {
	return db.AutoMigrate(&User{}, &UserInfo{})
}
