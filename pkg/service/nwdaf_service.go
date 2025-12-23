package service

import (
	"fmt"
	"time"

	"github.com/free5gc/nwdaf/internal/consumer"
	"github.com/free5gc/nwdaf/internal/context"
	"github.com/free5gc/nwdaf/internal/handler"
	"github.com/free5gc/nwdaf/internal/logger"
	"github.com/free5gc/nwdaf/pkg/factory"
	"github.com/free5gc/util/mongoapi"

	"github.com/gin-gonic/gin"
)

type NWDAF struct {
	Ctx *context.NWDAFContext
}

func (n *NWDAF) Initialize() {
	// Load config
	factory.InitConfigFactory("/home/ubuntu/free5gc/config/nwdafcfg.yaml")

	// Initialize context
	n.Ctx = context.InitNwdafContext()

	mongo := factory.NwdafConfigInstance.Configuration.Mongodb
	if mongo.Name != "" && mongo.Url != "" {
		if err := mongoapi.SetMongoDB(mongo.Name, mongo.Url); err != nil {
			logger.InitLog.Printf("[ERROR] MongoDB connect failed: %v", err)
		} else {
			logger.InitLog.Printf("[INFO] MongoDB connected: db=%s url=%s", mongo.Name, mongo.Url)
		}
	} else {
		logger.InitLog.Printf("[WARN] MongoDB config missing, using default connection")
	}

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
	const maxAttempts = 5
	delay := 2 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		amfProfile, err := consumer.DiscoverAmfFromNrf(n.Ctx)
		if err != nil {
			logger.InitLog.Printf("[ERROR] Failed to discover AMF (attempt %d/%d): %v", attempt, maxAttempts, err)
		} else {
			err = consumer.SubscribeToAmfEvents(n.Ctx, amfProfile)
			if err == nil {
				logger.InitLog.Printf("[INFO] Successfully subscribed to AMF for UE location events.")
				return
			}
			logger.InitLog.Printf("[WARN] Subscribe to AMF events failed (attempt %d/%d): %v", attempt, maxAttempts, err)
		}
		time.Sleep(delay)
		if delay < 30*time.Second {
			delay *= 2
		}
	}
	logger.InitLog.Printf("[ERROR] Giving up AMF subscription after %d attempts", maxAttempts)
}

func (n *NWDAF) discoverAndSubscribeToSmf() {
	logger.InitLog.Printf("[INFO] Starting SMF discovery and subscription process...")
	const maxAttempts = 5
	delay := 2 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt == 1 {
			logger.InitLog.Printf("[INFO] Waiting for SMF to register to NRF...")
			time.Sleep(2 * time.Second)
		}
		logger.InitLog.Printf("[INFO] Discovering SMF instances from NRF (attempt %d/%d)...", attempt, maxAttempts)
		smfProfile, err := consumer.DiscoverSmfFromNrf(n.Ctx)
		if err != nil {
			logger.InitLog.Printf("[ERROR] Failed to discover SMF: %v", err)
		} else {
			logger.InitLog.Printf("[INFO] SMF discovered successfully: %s", smfProfile.EventExposureUrl)
			logger.InitLog.Printf("[INFO] Subscribing to SMF events (attempt %d/%d)...", attempt, maxAttempts)
			err = consumer.SubscribeToSmfEvents(n.Ctx, smfProfile)
			if err == nil {
				logger.InitLog.Printf("[INFO] Successfully subscribed to SMF for events.")
				return
			}
			logger.InitLog.Printf("[WARN] Failed to subscribe to SMF events: %v", err)
		}
		time.Sleep(delay)
		if delay < 30*time.Second {
			delay *= 2
		}
	}
	logger.InitLog.Printf("[ERROR] Giving up SMF subscription after %d attempts", maxAttempts)
}

func (n *NWDAF) discoverAndSubscribeToUdm() {
	const maxAttempts = 5
	delay := 2 * time.Second
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		udmProfile, err := consumer.DiscoverUdmFromNrf(n.Ctx)
		if err != nil {
			logger.InitLog.Printf("[ERROR] Failed to discover UDM (attempt %d/%d): %v", attempt, maxAttempts, err)
		} else {
			err = consumer.SubscribeToUdmEeEvents(n.Ctx, udmProfile)
			if err == nil {
				logger.InitLog.Printf("[INFO] Successfully subscribed to UDM EE.")
				return
			}
			logger.InitLog.Printf("[WARN] Failed to subscribe to UDM EE (attempt %d/%d): %v", attempt, maxAttempts, err)
		}
		time.Sleep(delay)
		if delay < 30*time.Second {
			delay *= 2
		}
	}
	logger.InitLog.Printf("[ERROR] Giving up UDM EE subscription after %d attempts", maxAttempts)
}

func (n *NWDAF) Terminate() {
	logger.InitLog.Printf("[INFO] Terminating NWDAF...")
	// Deregister from NRF
	consumer.SendDeregisterNFInstance()
	logger.InitLog.Printf("[INFO] NWDAF terminated.")
}
