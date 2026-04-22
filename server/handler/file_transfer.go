package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/clipcascade/server/config"
	"github.com/clipcascade/server/model"
)

// FileTransferHandler 提供基于 HTTP 的文件中转上传/下载能力。
type FileTransferHandler struct {
	DB     *gorm.DB
	Config *config.Config
}

func NewFileTransferHandler(db *gorm.DB, cfg *config.Config) *FileTransferHandler {
	return &FileTransferHandler{DB: db, Config: cfg}
}

func (h *FileTransferHandler) Upload(c *fiber.Ctx) error {
	if !h.Config.FileRelayEnabled {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "file relay disabled"})
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing file"})
	}
	if fh.Size <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid file size"})
	}
	if h.Config.FileRelayMaxBytes > 0 && fh.Size > h.Config.FileRelayMaxBytes {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "file too large", "max_bytes": h.Config.FileRelayMaxBytes})
	}

	if err := os.MkdirAll(h.Config.FileRelayDir, 0o755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create relay dir"})
	}

	name := sanitizeUploadFilename(fh.Filename)
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid filename"})
	}
	username, _ := c.Locals("username").(string)
	relayID, err := randomID(16)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create relay id"})
	}

	tempPath := filepath.Join(h.Config.FileRelayDir, relayID+".part")
	finalPath := filepath.Join(h.Config.FileRelayDir, relayID+filepath.Ext(name))
	defer os.Remove(tempPath)

	file, err := fh.Open()
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "failed to open upload"})
	}
	defer file.Close()

	out, err := os.Create(tempPath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create relay file"})
	}

	hasher := sha256.New()
	written, copyErr := copyWithHash(out, file, hasher)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to store file"})
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to finalize upload"})
	}

	record := &model.FileTransfer{
		RelayID:       relayID,
		OwnerUsername: username,
		FileName:      name,
		FileSize:      written,
		SHA256:        hex.EncodeToString(hasher.Sum(nil)),
		MimeType:      detectMimeType(fh),
		StoragePath:   finalPath,
		ExpiresAt:     time.Now().Add(time.Duration(h.Config.FileRelayTTLSeconds) * time.Second),
	}
	if err := h.DB.Create(record).Error; err != nil {
		_ = os.Remove(finalPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to persist relay metadata"})
	}

	return c.JSON(fiber.Map{
		"relay_id":  record.RelayID,
		"file_name": record.FileName,
		"file_size": record.FileSize,
		"sha256":    record.SHA256,
		"mime_type": record.MimeType,
	})
}

func (h *FileTransferHandler) Download(c *fiber.Ctx) error {
	if !h.Config.FileRelayEnabled {
		return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{"error": "file relay disabled"})
	}

	relayID := strings.TrimSpace(c.Params("relayID"))
	if relayID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing relay id"})
	}

	var record model.FileTransfer
	if err := h.DB.Where("relay_id = ?", relayID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load relay metadata"})
	}
	if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
		_ = os.Remove(record.StoragePath)
		_ = h.DB.Delete(&record).Error
		return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": "file expired"})
	}

	if _, err := os.Stat(record.StoragePath); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file missing"})
	}

	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", record.FileName))
	if record.MimeType != "" {
		c.Set(fiber.HeaderContentType, record.MimeType)
		c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	}
	c.Set(fiber.HeaderContentLength, fmt.Sprintf("%d", record.FileSize))
	return c.SendFile(record.StoragePath)
}

func (h *FileTransferHandler) CleanupExpired() error {
	var expired []model.FileTransfer
	if err := h.DB.Where("expires_at > ? AND expires_at <= ?", time.Time{}, time.Now()).Find(&expired).Error; err != nil {
		return err
	}
	for _, record := range expired {
		_ = os.Remove(record.StoragePath)
		_ = h.DB.Delete(&record).Error
	}
	return nil
}

func (h *FileTransferHandler) RunCleanupLoop(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			_ = h.CleanupExpired()
		}
	}
}

func sanitizeUploadFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" || name == "." || name == string(os.PathSeparator) {
		return ""
	}
	return name
}

func detectMimeType(fh *multipart.FileHeader) string {
	if fh != nil {
		if ct := strings.TrimSpace(fh.Header.Get(fiber.HeaderContentType)); ct != "" {
			return ct
		}
	}
	return "application/octet-stream"
}

func randomID(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func copyWithHash(dst *os.File, src multipart.File, hasher hashWriter) (int64, error) {
	return io.CopyBuffer(io.MultiWriter(dst, hasher), src, make([]byte, 256*1024))
}

type hashWriter interface {
	Write(p []byte) (n int, err error)
	Sum(b []byte) []byte
}
