package model

import "time"

// FileTransfer 保存 HTTP 文件中转的元数据。
type FileTransfer struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	RelayID       string    `gorm:"uniqueIndex;size:64;not null" json:"relay_id"`
	OwnerUsername string    `gorm:"index;size:50;not null" json:"owner_username"`
	FileName      string    `gorm:"size:255;not null" json:"file_name"`
	FileSize      int64     `gorm:"not null" json:"file_size"`
	SHA256        string    `gorm:"size:64;not null" json:"sha256"`
	MimeType      string    `gorm:"size:255" json:"mime_type"`
	StoragePath   string    `gorm:"size:1024;not null" json:"storage_path"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `gorm:"index" json:"expires_at"`
}
