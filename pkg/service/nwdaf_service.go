package service

import (
	"fmt"
	"time"

	"github.com/free5gc/nwdaf/internal/consumer"
	"github.com/free5gc/nwdaf/internal/context"
	"github.com/free5gc/nwdaf/internal/handler"
	"github.com/free5gc/nwdaf/internal/logger"
	"github.com/free5gc/nwdaf/pkg/factory"

	"github.com/gin-gonic/gin"
)

type NWDAF struct {
	Ctx *context.NWDAFContext
}

func (n *NWDAF) Initialize() {
	// Load config
	factory.InitConfigFactory("../../config/nwdafcfg.yaml")

	// Initialize context
	n.Ctx = context.InitNwdafContext()

	// ... other initializations
	// 移除自动拉取AMF ULI的调用
}

func (n *NWDAF) Start() {
	// 1. Register to NRF
	err := consumer.SendRegisterNFInstance(n.Ctx.NrfUri, n.Ctx.NfId, n.Ctx.GetNFProfile())
	if err != nil {
		logger.InitLog.Printf("[ERROR] Failed to register to NRF: %v", err)
		return
	}
	logger.InitLog.Printf("[INFO] NWDAF registered to NRF successfully.")

	// 2. Start SBI server to receive notifications from AMF
	go n.startSbiServer()

	// Give NFs some time to register
	time.Sleep(1 * time.Second)

	// 3. Discover AMF and subscribe to events
	go n.discoverAndSubscribeToAmf()
}

func (n *NWDAF) startSbiServer() {
	router := gin.Default()

	// This is the endpoint for AMF notifications
	notificationGroup := router.Group("/nnwdaf-events/v1")
	notificationGroup.POST("/notifications", handler.HandleAmfNotification)
	notificationGroup.GET("/uli", handler.HandleGetUli) // 新增GET接口

	sbiConfig := factory.NwdafConfigInstance.Configuration.Sbi
	if sbiConfig.Port == 0 {
		sbiConfig.Port = 8001
	}
	addr := fmt.Sprintf("%s:%d", sbiConfig.BindingIPv4, sbiConfig.Port)

	logger.InitLog.Printf("[INFO] Starting NWDAF SBI server at %s", addr)
	err := router.Run(addr)
	if err != nil {
		logger.InitLog.Printf("[ERROR] Failed to start SBI server: %v", err)
	}
}

func (n *NWDAF) discoverAndSubscribeToAmf() {
	// Use NRF consumer logic to find an AMF instance that supports 'namf-eventexposure'
	amfProfile, err := consumer.DiscoverAmfFromNrf(n.Ctx)
	if err != nil {
		logger.InitLog.Printf("[ERROR] Failed to discover AMF: %v", err)
		return
	}

	// Found an AMF. Now subscribe for UE location events.
	err = consumer.SubscribeToAmfEvents(n.Ctx, amfProfile)
	if err != nil {
		logger.InitLog.Printf("[ERROR] Failed to subscribe to AMF events: %v", err)
	} else {
		logger.InitLog.Printf("[INFO] Successfully subscribed to AMF for UE location events.")
	}
}

func (n *NWDAF) Terminate() {
	logger.InitLog.Printf("[INFO] Terminating NWDAF...")
	// Deregister from NRF
	consumer.SendDeregisterNFInstance()
	logger.InitLog.Printf("[INFO] NWDAF terminated.")
}
