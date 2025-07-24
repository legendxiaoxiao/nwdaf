// src/nwdaf/handler/notification_handler.go
package handler

import (
    "github.com/gin-gonic/gin"
)

func HandleAmfNotification(c *gin.Context) {
	HandleUliNotification(c)
}
