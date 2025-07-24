package handler

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/free5gc/nwdaf/internal/logger"

	"sync"

	"github.com/gin-gonic/gin"
)

type UliNotification struct {
	UeId string `json:"ueId"`
	Uli  struct {
		Tai struct {
			PlmnId struct {
				Mcc string `json:"mcc"`
				Mnc string `json:"mnc"`
			} `json:"plmnId"`
			Tac string `json:"tac"`
		} `json:"tai"`
		Ecgi struct {
			PlmnId struct {
				Mcc string `json:"mcc"`
				Mnc string `json:"mnc"`
			} `json:"plmnId"`
			Eci string `json:"eci"`
		} `json:"ecgi"`
	} `json:"uli"`
}

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

// 全局变量和锁
var (
	uliStore     []UliNotification
	uliStoreLock sync.Mutex
)

func HandleUliNotification(c *gin.Context) {
	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		logger.AppLog.Printf("[ERROR] 读取通知体失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	// 先尝试解析AMF LOCATION_REPORT
	var locReport AmfLocationReport
	if err := json.Unmarshal(body, &locReport); err == nil && locReport.Type == "LOCATION_REPORT" {
		logger.AppLog.Printf("[INFO] 收到 LOCATION_REPORT: UE=%s, TAC=%s, ECI=%s",
			locReport.Supi,
			locReport.Location.NrLocation.Tai.Tac,
			locReport.Location.NrLocation.Ncgi.NrCellId)
		c.Status(http.StatusNoContent)
		return
	}

	// 否则按原ULI结构处理
	var uliData UliNotification
	if err := json.Unmarshal(body, &uliData); err != nil {
		logger.AppLog.Printf("[ERROR] 解析 ULI JSON 失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
		return
	}
	logger.AppLog.Printf("[INFO] 收到 ULI: UE=%s, TAC=%s, ECI=%s", uliData.UeId, uliData.Uli.Tai.Tac, uliData.Uli.Ecgi.Eci)

	uliStoreLock.Lock()
	uliStore = append(uliStore, uliData)
	uliStoreLock.Unlock()

	c.Status(http.StatusNoContent)
}

// 新增GET接口
func HandleGetUli(c *gin.Context) {
	uliStoreLock.Lock()
	defer uliStoreLock.Unlock()
	c.JSON(http.StatusOK, uliStore)
}
