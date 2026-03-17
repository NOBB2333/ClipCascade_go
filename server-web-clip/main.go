package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"gorm.io/gorm"
)

//go:embed web/templates
var templatesFS embed.FS

const (
	pageSize       = 10
	nameMaxRunes   = 20
	maxUploadBytes = 500 << 20 // 500MB
	csrfCookieName = "webclip_csrf"
	defaultName    = "Anonymous"
)

var db *gorm.DB

type Message struct {
	ID        uint           `gorm:"primaryKey"`
	Name      string         `gorm:"size:20"`
	Body      string         `gorm:"type:text"`
	Timestamp time.Time      `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func initDB(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	var err error
	db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	return db.AutoMigrate(&Message{})
}

func parsePage(raw string) int {
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func calcTotalPages(total int64) int {
	pages := int(math.Ceil(float64(total) / float64(pageSize)))
	if pages < 1 {
		return 1
	}
	return pages
}

func listUploadFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func loadMessages(page int) ([]Message, int64, int, error) {
	var total int64
	if err := db.Model(&Message{}).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	var msgs []Message
	if err := db.Order("timestamp desc").Offset((page-1)*pageSize).Limit(pageSize).Find(&msgs).Error; err != nil {
		return nil, 0, 0, err
	}
	return msgs, total, calcTotalPages(total), nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ensureCSRFToken(c *fiber.Ctx) (string, error) {
	token := c.Cookies(csrfCookieName)
	if token != "" {
		return token, nil
	}
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	c.Cookie(&fiber.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		MaxAge:   86400 * 30,
		Path:     "/",
		HTTPOnly: true,
		SameSite: "Lax",
	})
	return token, nil
}

func validateCSRF(c *fiber.Ctx) (bool, error) {
	cookieToken := c.Cookies(csrfCookieName)
	if cookieToken == "" {
		return false, c.Status(fiber.StatusForbidden).SendString("missing csrf cookie")
	}
	formToken := c.FormValue("csrf_token")
	if formToken == "" || subtle.ConstantTimeCompare([]byte(cookieToken), []byte(formToken)) != 1 {
		return false, c.Status(fiber.StatusForbidden).SendString("invalid csrf token")
	}
	return true, nil
}

func normalizeName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return defaultName
	}
	if len([]rune(name)) > nameMaxRunes {
		return string([]rune(name)[:nameMaxRunes])
	}
	return name
}

func normalizeBody(raw string) (string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return "", errors.New("body is required")
	}
	return body, nil
}

func uniqueUploadPath(dir, fileName string) string {
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	candidate := fileName
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return filepath.Join(dir, candidate)
		}
		candidate = fmt.Sprintf("%s_%d%s", base, i, ext)
	}
}

func main() {
	// 默认路径基于二进制所在目录，避免相对路径混乱
	exeDir := func() string {
		exe, err := os.Executable()
		if err != nil {
			return "."
		}
		return filepath.Dir(exe)
	}()

	dbPath := strings.TrimSpace(os.Getenv("WEBCLIP_DB_PATH"))
	if dbPath == "" {
		dbPath = filepath.Join(exeDir, "data.db")
	}
	uploadDir := strings.TrimSpace(os.Getenv("WEBCLIP_UPLOAD_DIR"))
	if uploadDir == "" {
		uploadDir = filepath.Join(exeDir, "uploads")
	}
	startPort := 8090
	if p := strings.TrimSpace(os.Getenv("WEBCLIP_PORT")); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			startPort = n
		}
	}

	if err := initDB(dbPath); err != nil {
		log.Fatal("init db:", err)
	}
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		log.Fatal("create upload dir:", err)
	}

	fmt.Printf("\n  database : %s\n", dbPath)
	fmt.Printf("  uploads  : %s\n\n", uploadDir)

	engine := html.NewFileSystem(http.FS(templatesFS), ".html")
	engine.AddFunc("add", func(a, b int) int { return a + b })
	engine.AddFunc("sub", func(a, b int) int { return a - b })

	app := fiber.New(fiber.Config{
		Views:     engine,
		BodyLimit: maxUploadBytes,
	})

	app.Static("/uploads", uploadDir)

	// ── 首页：列表 + 提交 ──
	app.Get("/", func(c *fiber.Ctx) error {
		page := parsePage(c.Query("page", "1"))
		msgs, total, totalPages, err := loadMessages(page)
		if err != nil {
			return c.Status(500).SendString("failed to load messages")
		}
		files, err := listUploadFiles(uploadDir)
		if err != nil {
			return c.Status(500).SendString("failed to list files")
		}
		csrfToken, err := ensureCSRFToken(c)
		if err != nil {
			return c.Status(500).SendString("csrf error")
		}
		return c.Render("web/templates/index", fiber.Map{
			"messages":   msgs,
			"page":       page,
			"totalPages": totalPages,
			"total":      total,
			"fileNames":  files,
			"csrfToken":  csrfToken,
		})
	})

	app.Post("/", func(c *fiber.Ctx) error {
		ok, err := validateCSRF(c)
		if !ok {
			return err
		}
		body, err := normalizeBody(c.FormValue("body"))
		if err != nil {
			return c.Status(400).SendString(err.Error())
		}
		msg := Message{
			Name:      normalizeName(c.FormValue("name")),
			Body:      body,
			Timestamp: time.Now(),
		}
		if err := db.Create(&msg).Error; err != nil {
			return c.Status(500).SendString("save failed")
		}
		return c.Redirect("/")
	})

	// ── 文件上传 ──
	app.Post("/upload", func(c *fiber.Ctx) error {
		ok, err := validateCSRF(c)
		if !ok {
			return err
		}
		fh, err := c.FormFile("file")
		if err != nil {
			return c.Status(400).SendString("invalid file")
		}
		if fh.Size <= 0 || fh.Size > maxUploadBytes {
			return c.Status(400).SendString("invalid file size")
		}
		name := filepath.Base(fh.Filename)
		if name == "" || name == "." {
			return c.Status(400).SendString("invalid filename")
		}
		if err := c.SaveFile(fh, uniqueUploadPath(uploadDir, name)); err != nil {
			return c.Status(500).SendString("upload failed")
		}
		fmt.Printf("[upload] %s  size=%d bytes  from=%s\n", name, fh.Size, c.IP())
		return c.Redirect("/")
	})

	// ── 管理页 ──
	app.Get("/manage", func(c *fiber.Ctx) error {
		page := parsePage(c.Query("page", "1"))
		msgs, _, totalPages, err := loadMessages(page)
		if err != nil {
			return c.Status(500).SendString("failed to load messages")
		}
		files, err := listUploadFiles(uploadDir)
		if err != nil {
			return c.Status(500).SendString("failed to list files")
		}
		csrfToken, err := ensureCSRFToken(c)
		if err != nil {
			return c.Status(500).SendString("csrf error")
		}
		return c.Render("web/templates/manage", fiber.Map{
			"messages":   msgs,
			"page":       page,
			"totalPages": totalPages,
			"fileNames":  files,
			"csrfToken":  csrfToken,
		})
	})

	app.Post("/manage/create", func(c *fiber.Ctx) error {
		ok, err := validateCSRF(c)
		if !ok {
			return err
		}
		body, err := normalizeBody(c.FormValue("body"))
		if err != nil {
			return c.Status(400).SendString(err.Error())
		}
		msg := Message{
			Name:      normalizeName(c.FormValue("name")),
			Body:      body,
			Timestamp: time.Now(),
		}
		if err := db.Create(&msg).Error; err != nil {
			return c.Status(500).SendString("save failed")
		}
		return c.Redirect("/manage?page=1")
	})

	handleManageAction := func(c *fiber.Ctx) error {
		ok, err := validateCSRF(c)
		if !ok {
			return err
		}
		page := parsePage(c.FormValue("page"))
		action := c.FormValue("action")

		switch action {
		case "bulk_delete":
			ids := c.Request().PostArgs().PeekMulti("delete_ids")
			if len(ids) > 0 {
				strIDs := make([]string, len(ids))
				for i, id := range ids {
					strIDs[i] = string(id)
				}
				db.Where("id IN ?", strIDs).Delete(&Message{})
			}
		case "delete_one":
			id, err := strconv.ParseUint(c.FormValue("delete_id"), 10, 32)
			if err != nil {
				return c.Status(400).SendString("invalid id")
			}
			db.Where("id = ?", id).Delete(&Message{})
		case "edit":
			id, err := strconv.ParseUint(c.FormValue("edit_id"), 10, 32)
			if err != nil {
				return c.Status(400).SendString("invalid id")
			}
			rawID := c.FormValue("edit_id")
			newBody, err := normalizeBody(c.FormValue("new_body_" + rawID))
			if err != nil {
				return c.Status(400).SendString(err.Error())
			}
			db.Model(&Message{}).Where("id = ?", id).Update("body", newBody)
		default:
			return c.Status(400).SendString("invalid action")
		}

		var count int64
		db.Model(&Message{}).Count(&count)
		maxPage := calcTotalPages(count)
		if page > maxPage {
			page = maxPage
		}
		return c.Redirect("/manage?page=" + strconv.Itoa(page))
	}

	app.Post("/manage", handleManageAction)

	app.Post("/file/delete", func(c *fiber.Ctx) error {
		ok, err := validateCSRF(c)
		if !ok {
			return err
		}
		name := filepath.Base(c.FormValue("filename"))
		if name == "" || name == "." {
			return c.Status(400).SendString("invalid filename")
		}
		if err := os.Remove(filepath.Join(uploadDir, name)); err != nil {
			return c.Status(500).SendString("delete failed")
		}
		return c.Redirect("/manage")
	})

	// 端口自增：如果目标端口被占用，自动往后找
	port := startPort
	var ln net.Listener
	for {
		l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln = l
			break
		}
		fmt.Printf("[warn] port %d in use, trying %d\n", port, port+1)
		port++
		if port > startPort+100 {
			log.Fatal("no available port found")
		}
	}

	fmt.Printf("  ➜  http://localhost:%d/\n\n", port)
	log.Fatal(app.Listener(ln))
}
