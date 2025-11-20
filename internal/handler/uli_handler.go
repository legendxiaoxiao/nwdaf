package handler

import (
	"net/http"
	"time"

	"github.com/free5gc/nwdaf/internal/logger"
	"github.com/free5gc/util/mongoapi"
	"go.mongodb.org/mongo-driver/bson"
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
	Supi           string                 `json:"supi"`
	EventType      string                 `json:"eventType"`
	Timestamp      time.Time              `json:"timestamp"`
	EventDetails   map[string]interface{} `json:"eventDetails"`
	PduSessionId   int32                  `json:"pduSessionId"`
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

	// 将AMF位置信息写入MongoDB
	coll := "nwdaf.amf.locationReport"
	filter := bson.M{"supi": locReport.Supi, "nrCellId": locReport.Location.NrLocation.Ncgi.NrCellId}
	putData := bson.M{
		"supi":     locReport.Supi,
		"type":     locReport.Type,
		"tac":      locReport.Location.NrLocation.Tai.Tac,
		"nrCellId": locReport.Location.NrLocation.Ncgi.NrCellId,
		"plmnId":   bson.M{"mcc": locReport.Location.NrLocation.Tai.PlmnId.Mcc, "mnc": locReport.Location.NrLocation.Tai.PlmnId.Mnc},
	}
	_, _ = mongoapi.RestfulAPIPutOne(coll, filter, putData)

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
	case "PDU_SESSION_ESTABLISHMENT", "PDU_SESSION_MODIFICATION", "PDU_SESSION_RELEASE":
		pduState, _ := smfEvent.EventDetails["pduSessionState"].(string)
		qfiRaw, _ := smfEvent.EventDetails["qfiList"].([]interface{})
		qfiList := make([]int, 0, len(qfiRaw))
		for _, v := range qfiRaw {
			if n, ok := v.(float64); ok {
				qfiList = append(qfiList, int(n))
			}
		}
		logger.AppLog.Printf("[INFO] 收到SMF事件: %s, SUPI=%s, PDU=%d, state=%s, qfi=%v, ts=%s",
			smfEvent.EventType, smfEvent.Supi, smfEvent.PduSessionId, pduState, qfiList, smfEvent.Timestamp.Format(time.RFC3339))
		coll := "nwdaf.smf.events"
		filter := bson.M{"supi": smfEvent.Supi, "pduSessionId": smfEvent.PduSessionId, "eventType": smfEvent.EventType, "timestamp": smfEvent.Timestamp}
		putData := bson.M{"supi": smfEvent.Supi, "pduSessionId": smfEvent.PduSessionId, "eventType": smfEvent.EventType, "timestamp": smfEvent.Timestamp, "pduSessionState": pduState, "qfiList": qfiList}
		_, _ = mongoapi.RestfulAPIPutOne(coll, filter, putData)
		c.JSON(http.StatusOK, gin.H{"status": "SMF event received"})
	default:
		logger.AppLog.Printf("[INFO] 收到非期望SMF事件类型: %s, SUPI=%s", smfEvent.EventType, smfEvent.Supi)
		c.JSON(http.StatusOK, gin.H{"status": "ignored"})
	}
}

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
