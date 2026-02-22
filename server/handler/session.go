package handler

import (
	"github.com/gofiber/fiber/v2/middleware/session"
)

// Store 是 server 的全局 Fiber session store。
// 在 main.go 中初始化。
var Store *session.Store
