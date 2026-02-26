package handler

import (
	"bytes"
	"context"
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
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"github.com/gin-gonic/gin"
)

// AmfLocationReport 表示从 AMF 上报的 UE 位置报告结构。
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

// SMFEventReport 表示从 SMF 上报的会话状态、用量等事件信息。
type SMFEventReport struct {
    Supi           string                 `json:"supi"`
    EventType      string                 `json:"eventType"`
    Timestamp      time.Time              `json:"timestamp"`
    EventDetails   map[string]interface{} `json:"eventDetails"`
    PduSessionId   int32                  `json:"pduSessionId"`
}

// supi2pdu 维护 SUPI 与当前 PDU 会话 ID 的映射，用于后续策略控制。
var supi2pdu sync.Map

var amfInitOnce sync.Once
var amfQueueCap = 1024
var amfWorkerCount = 8
var amfQueue chan AmfLocationReport

// InitAmfWorkerPool 初始化 AMF 位置报告的 worker 池与队列容量配置。
func InitAmfWorkerPool(workers int, queueCap int) {
	if workers > 0 {
		amfWorkerCount = workers
	}
	if queueCap > 0 {
		amfQueueCap = queueCap
	}
	ensureAmfPool()
}

// ensureAmfPool 确保 AMF worker 池和消息队列只初始化一次。
func ensureAmfPool() {
	amfInitOnce.Do(func() {
		amfQueue = make(chan AmfLocationReport, amfQueueCap)
		for i := 0; i < amfWorkerCount; i++ {
			go amfWorker()
		}
		logger.AppLog.Printf("[INFO] AMF worker pool started: workers=%d, queueCap=%d", amfWorkerCount, amfQueueCap)
	})
}

// amfWorker 从队列中消费 AMF 位置报告并调用处理逻辑。
func amfWorker() {
	for ev := range amfQueue {
		processAmfLocation(ev)
	}
}

// processAmfLocation 处理单条 AMF 位置报告，落库并在特定条件下触发 PCF 删除策略。
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
	if tacNum == 0 {
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

// InitSmfWorkerPool 初始化 SMF 事件的 worker 池与队列容量配置。
func InitSmfWorkerPool(workers int, queueCap int) {
	if workers > 0 {
		smfWorkerCount = workers
	}
	if queueCap > 0 {
		smfQueueCap = queueCap
	}
	ensureSmfPool()
}

// ensureSmfPool 确保 SMF worker 池和消息队列只初始化一次。
func ensureSmfPool() {
	smfInitOnce.Do(func() {
		smfQueue = make(chan SMFEventReport, smfQueueCap)
		for i := 0; i < smfWorkerCount; i++ {
			go smfWorker()
		}
		logger.AppLog.Printf("[INFO] SMF worker pool started: workers=%d, queueCap=%d", smfWorkerCount, smfQueueCap)
	})
}

// smfWorker 从队列中消费 SMF 事件并调用处理逻辑。
func smfWorker() {
	for ev := range smfQueue {
		processSmfEvent(ev)
	}
}

// processSmfEvent 处理单条 SMF 事件，根据类型更新会话映射并将事件/用量写入 MongoDB。
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

// HandleUliNotification 处理来自 AMF 的 ULI/位置通知，将其入队由后台 worker 异步处理。
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

// HandleSMFEventNotification 处理来自 SMF 的事件通知，将其入队由后台 worker 异步处理。
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

// HandleGetUli 占位接口，ULI 查询能力已移除，统一返回未实现。
func HandleGetUli(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "ULI query removed"})
}

// HandleSecurityEventNotification 占位接口，安全事件处理能力已移除，统一返回未实现。
func HandleSecurityEventNotification(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "security processing removed"})
}

// HandleGetSecurityReport 占位接口，安全报告查询能力已移除，统一返回未实现。
func HandleGetSecurityReport(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "security report removed"})
}

// HandleGetBehaviorAnalysis 占位接口，行为分析能力已移除，统一返回未实现。
func HandleGetBehaviorAnalysis(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "behavior analysis removed"})
}

// processAmfMetric 将 AMF 侧统计指标按类型写入对应的 MongoDB 集合。
func processAmfMetric(metricType string, ts time.Time, count int64) {
	var coll string
	switch metricType {
	case "REG_REQUEST_COUNT":
		coll = "nwdaf.amf.reg_request_count"
	case "ACTIVE_UE_COUNT":
		coll = "nwdaf.amf.active_ue_count"
	default:
		return
	}
	filter := bson.M{"type": metricType, "timestamp": ts}
	put := bson.M{"type": metricType, "timestamp": ts, "count": count}
	_, _ = mongoapi.RestfulAPIPutOne(coll, filter, put)
}

// HandleGetAmfReports 查询 AMF 位置报告记录，支持按 SUPI、时间窗口与数量 limit 过滤。
func HandleGetAmfReports(c *gin.Context) {
	url, db := mongoCfg()
	supi := c.Query("supi")
	lim := getLimit(c)
	since, has := parseSince(c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := mongo.Connect(ctx, options.Client().ApplyURI(url))
	if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo connect"}); return }
	defer func() { _ = cli.Disconnect(context.Background()) }()
	f := bson.M{}
	if supi != "" { f["supi"] = supi }
	if has { f["_id"] = bson.M{"$gt": primitive.NewObjectIDFromTimestamp(since)} }
	opt := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(lim))
	cur, err := cli.Database(db).Collection("nwdaf.amf.locationReport").Find(ctx, f, opt)
	if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo find"}); return }
	var out []bson.M
	if err := cur.All(ctx, &out); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo decode"}); return }
	c.JSON(http.StatusOK, out)
}

// HandleGetSmfEvents 查询 SMF 事件记录，支持按 SUPI、时间窗口与数量 limit 过滤。
func HandleGetSmfEvents(c *gin.Context) {
	url, db := mongoCfg()
	supi := c.Query("supi")
	lim := getLimit(c)
	since, has := parseSince(c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := mongo.Connect(ctx, options.Client().ApplyURI(url))
	if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo connect"}); return }
	defer func() { _ = cli.Disconnect(context.Background()) }()
	f := bson.M{}
	if supi != "" { f["supi"] = supi }
	if has { f["timestamp"] = bson.M{"$gt": since} }
	opt := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}}).SetLimit(int64(lim))
	cur, err := cli.Database(db).Collection("nwdaf.smf.events").Find(ctx, f, opt)
	if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo find"}); return }
	var out []bson.M
	if err := cur.All(ctx, &out); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo decode"}); return }
	c.JSON(http.StatusOK, out)
}

// HandleGetSmfUsage 查询 SMF 用量记录，支持按 SUPI、时间窗口与数量 limit 过滤。
func HandleGetSmfUsage(c *gin.Context) {
	url, db := mongoCfg()
	supi := c.Query("supi")
	lim := getLimit(c)
	since, has := parseSince(c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := mongo.Connect(ctx, options.Client().ApplyURI(url))
	if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo connect"}); return }
	defer func() { _ = cli.Disconnect(context.Background()) }()
	f := bson.M{}
	if supi != "" { f["supi"] = supi }
	if has { f["timestamp"] = bson.M{"$gt": since} }
	opt := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}}).SetLimit(int64(lim))
	cur, err := cli.Database(db).Collection("nwdaf.smf.usage").Find(ctx, f, opt)
	if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo find"}); return }
	var out []bson.M
	if err := cur.All(ctx, &out); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "mongo decode"}); return }
	c.JSON(http.StatusOK, out)
}

// mongoCfg 从配置中解析 MongoDB 连接地址和数据库名，并提供默认值回退。
func mongoCfg() (string, string) {
	m := factory.NwdafConfigInstance.Configuration.Mongodb
	url := m.Url
	name := m.Name
	if url == "" { url = "mongodb://127.0.0.1:27017" }
	if name == "" { name = "nwdaf" }
	return url, name
}

// parseSince 解析查询参数 since 为 RFC3339 时间戳，解析失败时返回 false。
func parseSince(c *gin.Context) (time.Time, bool) {
	s := c.Query("since")
	if s == "" { return time.Time{}, false }
	t, err := time.Parse(time.RFC3339, s)
	if err != nil { return time.Time{}, false }
	return t, true
}

// getLimit 解析查询参数 limit，并做范围校验与默认值处理。
func getLimit(c *gin.Context) int {
	v := c.Query("limit")
	if v == "" { return 200 }
	n, err := strconv.Atoi(v)
	if err != nil { return 200 }
	if n < 1 { n = 1 }
	if n > 2000 { n = 2000 }
	return n
}
