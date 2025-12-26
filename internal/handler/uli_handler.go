package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/free5gc/nwdaf/internal/logger"
	"github.com/free5gc/nwdaf/pkg/factory"
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

var supi2pdu sync.Map

var amfInitOnce sync.Once
var amfQueueCap = 1024
var amfWorkerCount = 8
var amfQueue chan AmfLocationReport

func InitAmfWorkerPool(workers int, queueCap int) {
	if workers > 0 {
		amfWorkerCount = workers
	}
	if queueCap > 0 {
		amfQueueCap = queueCap
	}
	ensureAmfPool()
}

func ensureAmfPool() {
	amfInitOnce.Do(func() {
		amfQueue = make(chan AmfLocationReport, amfQueueCap)
		for i := 0; i < amfWorkerCount; i++ {
			go amfWorker()
		}
		logger.AppLog.Printf("[INFO] AMF worker pool started: workers=%d, queueCap=%d", amfWorkerCount, amfQueueCap)
	})
}

func amfWorker() {
	for ev := range amfQueue {
		processAmfLocation(ev)
	}
}

func processAmfLocation(locReport AmfLocationReport) {
	if locReport.Type != "LOCATION_REPORT" {
		return
	}

	logger.AppLog.Printf("[INFO] 收到 LOCATION_REPORT: UE=%s, TAC=%s, NRCellId=%s, ts=%s",
		locReport.Supi,
		locReport.Location.NrLocation.Tai.Tac,
		locReport.Location.NrLocation.Ncgi.NrCellId,
		time.Now().Format(time.RFC3339))

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

	tac := locReport.Location.NrLocation.Tai.Tac
	tacNum, _ := strconv.Atoi(tac)
	if tacNum == 2 {
		if v, ok := supi2pdu.Load(locReport.Supi); ok {
			pduID := v.(int32)
			smPolicyId := fmt.Sprintf("%s-%d", locReport.Supi, pduID)
			prefix := factory.NwdafConfigInstance.Configuration.Pcf.ApiPrefix
			if prefix == "" {
				prefix = "http://127.0.0.4:8000/npcf-smpolicycontrol/v1"
			}
			url := fmt.Sprintf("%s/sm-policies/%s/delete", prefix, smPolicyId)
			req, _ := http.NewRequest("POST", url, bytes.NewReader([]byte{}))
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{}
			resp, err := client.Do(req)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusNoContent {
					logger.AppLog.Printf("[INFO] 已触发PCF删除SM策略: smPolicyId=%s", smPolicyId)
				} else {
					logger.AppLog.Printf("[WARN] PCF删除SM策略返回状态: %s", resp.Status)
				}
			} else {
				logger.AppLog.Printf("[ERROR] 调用PCF删除SM策略失败: %v", err)
			}
		} else {
			logger.AppLog.Printf("[WARN] 未找到UE的PDU会话ID，跳过断开: UE=%s", locReport.Supi)
		}
	}
}

var smfInitOnce sync.Once
var smfQueueCap = 1024
var smfWorkerCount = 8
var smfQueue chan SMFEventReport

func InitSmfWorkerPool(workers int, queueCap int) {
	if workers > 0 {
		smfWorkerCount = workers
	}
	if queueCap > 0 {
		smfQueueCap = queueCap
	}
	ensureSmfPool()
}

func ensureSmfPool() {
	smfInitOnce.Do(func() {
		smfQueue = make(chan SMFEventReport, smfQueueCap)
		for i := 0; i < smfWorkerCount; i++ {
			go smfWorker()
		}
		logger.AppLog.Printf("[INFO] SMF worker pool started: workers=%d, queueCap=%d", smfWorkerCount, smfQueueCap)
	})
}

func smfWorker() {
	for ev := range smfQueue {
		processSmfEvent(ev)
	}
}

func processSmfEvent(smfEvent SMFEventReport) {
	switch smfEvent.EventType {
	case "PDU_SESSION_ESTABLISHMENT", "PDU_SESSION_MODIFICATION", "PDU_SESSION_RELEASE":
		if smfEvent.EventType == "PDU_SESSION_RELEASE" {
			supi2pdu.Delete(smfEvent.Supi)
		} else {
			supi2pdu.Store(smfEvent.Supi, smfEvent.PduSessionId)
		}
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
	case "USAGE_REPORT":
		usage := smfEvent.EventDetails["usageData"]
		logger.AppLog.Printf("[INFO] 收到SMF用量报告: SUPI=%s, PDU=%d, usage=%v, ts=%s",
			smfEvent.Supi, smfEvent.PduSessionId, usage, smfEvent.Timestamp.Format(time.RFC3339))
		coll := "nwdaf.smf.usage"
		filter := bson.M{"supi": smfEvent.Supi, "pduSessionId": smfEvent.PduSessionId, "timestamp": smfEvent.Timestamp}
		putData := bson.M{"supi": smfEvent.Supi, "pduSessionId": smfEvent.PduSessionId, "eventType": smfEvent.EventType, "timestamp": smfEvent.Timestamp, "usageData": usage}
		_, _ = mongoapi.RestfulAPIPutOne(coll, filter, putData)
	default:
		logger.AppLog.Printf("[INFO] 收到非期望SMF事件类型: %s, SUPI=%s", smfEvent.EventType, smfEvent.Supi)
	}
}

func HandleUliNotification(c *gin.Context) {
	var locReport AmfLocationReport
	if err := c.ShouldBindJSON(&locReport); err != nil {
		logger.AppLog.Printf("[ERROR] 解析 AMF 通知失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
		return
	}
	ensureAmfPool()
	select {
	case amfQueue <- locReport:
		c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "queue full"})
	}
}

func HandleSMFEventNotification(c *gin.Context) {
	var smfEvent SMFEventReport
	if err := c.ShouldBindJSON(&smfEvent); err != nil {
		logger.AppLog.Printf("[ERROR] 解析 SMF 事件失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}
	ensureSmfPool()
	select {
	case smfQueue <- smfEvent:
		c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "queue full"})
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
