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
	
	// 4. Discover SMF and subscribe to events
	go n.discoverAndSubscribeToSmf()
	// 新增：Discover UDM并订阅Nudm-EE事件
	go n.discoverAndSubscribeToUdm()
}

func (n *NWDAF) startSbiServer() {
	router := gin.Default()

	// This is the endpoint for AMF notifications
	notificationGroup := router.Group("/nnwdaf-events/v1")
	notificationGroup.POST("/notifications", handler.HandleAmfNotification)
	notificationGroup.GET("/uli", handler.HandleGetUli) // 新增GET接口
	
	// 新增SMF事件通知接口******
	notificationGroup.POST("/smf-notifications", handler.HandleSMFEventNotification)
	
	// 新增安全事件通知接口******
	notificationGroup.POST("/security-notifications", handler.HandleSecurityEventNotification)
	
	// 新增安全报告获取接口******
	notificationGroup.GET("/security-report", handler.HandleGetSecurityReport)
	
	// 新增行为分析获取接口******
	notificationGroup.GET("/behavior-analysis", handler.HandleGetBehaviorAnalysis)
	// 新增UDM EE事件通知接口
	notificationGroup.POST("/udm-ee-notifications", handler.HandleUdmEeNotification)

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

func (n *NWDAF) discoverAndSubscribeToSmf() {
	logger.InitLog.Printf("[INFO] Starting SMF discovery and subscription process...")
	
	// 等待一段时间让SMF注册到NRF
	logger.InitLog.Printf("[INFO] Waiting for SMF to register to NRF...")
	time.Sleep(2 * time.Second)
	
	// Use NRF consumer logic to find an SMF instance that supports 'nsmf-event-exposure'
	logger.InitLog.Printf("[INFO] Discovering SMF instances from NRF...")
	smfProfile, err := consumer.DiscoverSmfFromNrf(n.Ctx)
	if err != nil {
		logger.InitLog.Printf("[ERROR] Failed to discover SMF: %v", err)
		return
	}
	logger.InitLog.Printf("[INFO] SMF discovered successfully: %s", smfProfile.EventExposureUrl)

	// Found an SMF. Now subscribe for SMF events.
	logger.InitLog.Printf("[INFO] Subscribing to SMF events...")
	err = consumer.SubscribeToSmfEvents(n.Ctx, smfProfile)
	if err != nil {
		logger.InitLog.Printf("[ERROR] Failed to subscribe to SMF events: %v", err)
	} else {
		logger.InitLog.Printf("[INFO] Successfully subscribed to SMF for events.")
	}
}

func (n *NWDAF) discoverAndSubscribeToUdm() {
	// 发现UDM的Nudm-EE服务（当前为硬编码，后续可替换为NRF发现）
	udmProfile, err := consumer.DiscoverUdmFromNrf(n.Ctx)
	if err != nil {
		logger.InitLog.Printf("[ERROR] Failed to discover UDM: %v", err)
		return
	}
	logger.InitLog.Printf("[INFO] UDM discovered successfully: %s", udmProfile.EventExposureBaseUrl)

	// 订阅UDM Nudm-EE事件
	if err := consumer.SubscribeToUdmEeEvents(n.Ctx, udmProfile); err != nil {
		logger.InitLog.Printf("[ERROR] Failed to subscribe to UDM EE: %v", err)
	} else {
		logger.InitLog.Printf("[INFO] Successfully subscribed to UDM EE.")
	}
}

func (n *NWDAF) Terminate() {
	logger.InitLog.Printf("[INFO] Terminating NWDAF...")
	// Deregister from NRF
	consumer.SendDeregisterNFInstance()
	logger.InitLog.Printf("[INFO] NWDAF terminated.")
}
