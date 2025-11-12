package handler

import (
	"net/http"
	"time"

	"github.com/free5gc/nwdaf/internal/logger"
	"github.com/gin-gonic/gin"
)

type AmfLocationReport struct {
	Supi     string `json:"supi"`
	Type     string `json:"type"`
	Location struct {
		NrLocation struct {
			Tai struct {
				PlmnId struct {
					Mcc string `json:"mcc"`
					Mnc string `json:"mnc"`
				} `json:"plmnId"`
				Tac string `json:"tac"`
			} `json:"tai"`
			Ncgi struct {
				PlmnId struct {
					Mcc string `json:"mcc"`
					Mnc string `json:"mnc"`
				} `json:"plmnId"`
				NrCellId string `json:"nrCellId"`
			} `json:"ncgi"`
		} `json:"nrLocation"`
	} `json:"location"`
}

type SMFEventReport struct {
	Supi      string    `json:"supi"`
	EventType string    `json:"eventType"`
	Timestamp time.Time `json:"timestamp"`
}

func HandleUliNotification(c *gin.Context) {
	var locReport AmfLocationReport
	if err := c.ShouldBindJSON(&locReport); err != nil {
		logger.AppLog.Printf("[ERROR] 解析 AMF 通知失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
		return
	}

	if locReport.Type != "LOCATION_REPORT" {
		logger.AppLog.Printf("[INFO] 收到非 LOCATION_REPORT，忽略。type=%s", locReport.Type)
		c.Status(http.StatusNoContent)
		return
	}

	logger.AppLog.Printf("[INFO] 收到 LOCATION_REPORT: UE=%s, TAC=%s, NRCellId=%s",
		locReport.Supi,
		locReport.Location.NrLocation.Tai.Tac,
		locReport.Location.NrLocation.Ncgi.NrCellId)

	c.Status(http.StatusNoContent)
}

func HandleSMFEventNotification(c *gin.Context) {
	var smfEvent SMFEventReport
	if err := c.ShouldBindJSON(&smfEvent); err != nil {
		logger.AppLog.Printf("[ERROR] 解析 SMF 事件失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}

	switch smfEvent.EventType {
	case "PDU_SESSION_ESTABLISHMENT", "PDU_SESSION_RELEASE":
		logger.AppLog.Printf("[INFO] 收到SMF事件: %s, SUPI=%s, ts=%s",
			smfEvent.EventType, smfEvent.Supi, smfEvent.Timestamp.Format(time.RFC3339))
		c.JSON(http.StatusOK, gin.H{"status": "SMF event received"})
	default:
		logger.AppLog.Printf("[INFO] 收到非期望SMF事件类型: %s, SUPI=%s", smfEvent.EventType, smfEvent.Supi)
		c.JSON(http.StatusOK, gin.H{"status": "ignored"})
	}
}

// 以下占位保证路由兼容，如需彻底删除可同步修改 nwdaf_service.go
func HandleGetUli(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "ULI query removed"})
}

func HandleSecurityEventNotification(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "security processing removed"})
}

func HandleGetSecurityReport(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "security report removed"})
}

func HandleGetBehaviorAnalysis(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "behavior analysis removed"})
}
