package transfer

import "time"

type Direction string

type Status string

const (
	DirectionUpload   Direction = "upload"
	DirectionDownload Direction = "download"
)

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusVerifying Status = "verifying"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type Task struct {
	ID         string
	Direction  Direction
	FileName   string
	TotalBytes int64
	DoneBytes  int64
	Status     Status
	StartedAt  time.Time
	UpdatedAt  time.Time
	Err        string
	SHA256     string
	RelayID    string
}

type FileOfferPayload struct {
	TransferID string `json:"transfer_id"`
	RelayID    string `json:"relay_id"`
	FileName   string `json:"file_name"`
	FileSize   int64  `json:"file_size"`
	SHA256     string `json:"sha256"`
	MimeType   string `json:"mime_type,omitempty"`
	Transport  string `json:"transport"`
}

type UploadResult struct {
	RelayID  string `json:"relay_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	SHA256   string `json:"sha256"`
	MimeType string `json:"mime_type"`
}
