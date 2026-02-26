// src/nwdaf/handler/notification_handler.go
package handler

import (
	"encoding/json"
	"io"
	"time"

	"github.com/gin-gonic/gin"
)

func HandleAmfNotification(c *gin.Context) {
	b, _ := io.ReadAll(c.Request.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	t, _ := m["type"].(string)
	switch t {
	case "LOCATION_REPORT":
		var loc AmfLocationReport
		if err := json.Unmarshal(b, &loc); err != nil {
			c.JSON(400, gin.H{"error": "bad json"})
			return
		}
		ensureAmfPool()
		select {
		case amfQueue <- loc:
			c.JSON(202, gin.H{"status": "queued"})
		default:
			c.JSON(503, gin.H{"error": "queue full"})
		}
	case "REG_REQUEST_COUNT", "ACTIVE_UE_COUNT":
		var ts time.Time
		if v, ok := m["timestamp"].(string); ok {
			if tt, err := time.Parse(time.RFC3339, v); err == nil {
				ts = tt
			}
		}
		if ts.IsZero() { ts = time.Now().UTC() }
		var cnt int64
		switch cv := m["count"].(type) {
		case float64:
			cnt = int64(cv)
		case int64:
			cnt = cv
		case int:
			cnt = int64(cv)
		default:
			if v, ok := m["value"].(float64); ok { cnt = int64(v) }
		}
		processAmfMetric(t, ts, cnt)
		c.JSON(202, gin.H{"status": "accepted"})
	default:
		c.JSON(200, gin.H{"status": "ignored"})
	}
}

// HandleUdmEeNotification: 处理UDM Nudm-EE通知
func HandleUdmEeNotification(c *gin.Context) {
	// 与HandleUliNotification风格保持一致，直接复用其处理逻辑，
	// 如需区分UDM EE通知可在uli_handler中添加专用存储/分析。
	HandleUliNotification(c)
}
