// src/nwdaf/handler/notification_handler.go
package handler

import (
	"github.com/gin-gonic/gin"
)

func HandleAmfNotification(c *gin.Context) {
	HandleUliNotification(c)
}

// HandleUdmEeNotification: 处理UDM Nudm-EE通知
func HandleUdmEeNotification(c *gin.Context) {
	// 与HandleUliNotification风格保持一致，直接复用其处理逻辑，
	// 如需区分UDM EE通知可在uli_handler中添加专用存储/分析。
	HandleUliNotification(c)
}
