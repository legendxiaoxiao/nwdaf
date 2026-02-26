package service

import (
	"fmt"
	"time"
	"os"
	"os/exec"

	"github.com/free5gc/nwdaf/internal/consumer"
	"github.com/free5gc/nwdaf/internal/context"
	"github.com/free5gc/nwdaf/internal/handler"
	"github.com/free5gc/nwdaf/internal/logger"
	"github.com/free5gc/nwdaf/pkg/factory"
	"github.com/free5gc/util/mongoapi"

	"github.com/gin-gonic/gin"
)

type NWDAF struct {
	Ctx        *context.NWDAFContext
	ppCmd      *exec.Cmd
	anomalyCmd *exec.Cmd
}

// Initialize 初始化 NWDAF 的配置、上下文与外部依赖（如 MongoDB 连接等）。
func (n *NWDAF) Initialize() {
	// Load config
	factory.InitConfigFactory("/home/ubuntu/free5gc/config/nwdafcfg.yaml")

	// Initialize context
	n.Ctx = context.InitNwdafContext()
	sbi := factory.NwdafConfigInstance.Configuration.Sbi
	if sbi.BindingIPv4 != "" { n.Ctx.RegisterIPv4 = sbi.BindingIPv4 }
	if sbi.Port != 0 { n.Ctx.SBIPort = sbi.Port }
	if v := os.Getenv("NWDAF_NRF_URI"); v != "" { n.Ctx.NrfUri = v }

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

// Start 启动 NWDAF：向 NRF 注册、启动 SBI 服务，并并行进行各 NF 发现/订阅与检测任务。
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
	// 启动乒乓切换实时检测
	go n.startPingPongDetector()
	// 启动异常检测（孤立森林）
	go n.startAnomalyDetector()
}

// startSbiServer 启动 NWDAF 的 SBI HTTP 服务，用于接收各类事件通知并提供查询接口/静态 UI。
func (n *NWDAF) startSbiServer() {
	router := gin.Default()
	router.Static("/ui", "/home/ubuntu/free5gc/NFs/nwdaf/web/ui")
	router.GET("/", func(c *gin.Context) { c.Redirect(302, "/ui/") })

	notificationGroup := router.Group("/nnwdaf-events/v1")
	notificationGroup.POST("/notifications", handler.HandleAmfNotification)
	notificationGroup.POST("/smf-notifications", handler.HandleSMFEventNotification)
	notificationGroup.POST("/security-notifications", handler.HandleSecurityEventNotification)
	notificationGroup.POST("/udm-ee-notifications", handler.HandleUdmEeNotification)
	notificationGroup.GET("/amf-reports", handler.HandleGetAmfReports)
	notificationGroup.GET("/smf-events", handler.HandleGetSmfEvents)
	notificationGroup.GET("/smf-usage", handler.HandleGetSmfUsage)
	notificationGroup.GET("/uli", handler.HandleGetUli)
	notificationGroup.GET("/security-report", handler.HandleGetSecurityReport)
	notificationGroup.GET("/behavior-analysis", handler.HandleGetBehaviorAnalysis)

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

// startPingPongDetector 启动乒乓切换实时检测脚本进程，并注入所需的环境变量（MongoDB 等）。
func (n *NWDAF) startPingPongDetector() {
	script := os.Getenv("NWDAF_PPP_SCRIPT")
	if script == "" {
		script = "/home/ubuntu/free5gc/NFs/nwdaf/internal/util/pingpang_handle.py"
	}
	py := os.Getenv("NWDAF_PY_BIN")
	if py == "" {
		py = "python3"
	}
	mongo := factory.NwdafConfigInstance.Configuration.Mongodb
	env := os.Environ()
	if mongo.Url != "" {
		env = append(env, fmt.Sprintf("MONGODB_URL=%s", mongo.Url))
	}
	if mongo.Name != "" {
		env = append(env, fmt.Sprintf("NWDAF_DB=%s", mongo.Name))
	}
	cmd := exec.Command(py, script)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		logger.InitLog.Printf("[ERROR] 启动乒乓切换检测失败: %v", err)
		return
	}
	n.ppCmd = cmd
	logger.InitLog.Printf("[INFO] 已启动乒乓切换检测: %s %s (PID=%d)", py, script, cmd.Process.Pid)
}

// startAnomalyDetector 启动异常检测脚本进程（Isolation Forest），并以循环模式持续运行。
func (n *NWDAF) startAnomalyDetector() {
	script := os.Getenv("NWDAF_ANOMALY_SCRIPT")
	if script == "" {
		script = "/home/ubuntu/free5gc/NFs/nwdaf/internal/util/isolation_forest_anomaly.py"
	}
	py := os.Getenv("NWDAF_PY_BIN")
	if py == "" {
		py = "python3"
	}
	mongo := factory.NwdafConfigInstance.Configuration.Mongodb
	env := os.Environ()
	if mongo.Url != "" {
		env = append(env, fmt.Sprintf("MONGODB_URL=%s", mongo.Url))
	}
	if mongo.Name != "" {
		env = append(env, fmt.Sprintf("NWDAF_DB=%s", mongo.Name))
	}
	env = append(env, "NWDAF_ANOMALY_LOOP=1")
	cmd := exec.Command(py, script)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		logger.InitLog.Printf("[ERROR] 启动异常检测失败: %v", err)
		return
	}
	n.anomalyCmd = cmd
	logger.InitLog.Printf("[INFO] 已启动异常检测: %s %s (PID=%d)", py, script, cmd.Process.Pid)
}

// discoverAndSubscribeToAmf 从 NRF 发现 AMF 并订阅事件通知；失败会指数退避重试并最终放弃。
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

// discoverAndSubscribeToSmf 从 NRF 发现 SMF 并订阅事件通知；失败会指数退避重试并最终放弃。
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

// discoverAndSubscribeToUdm 从 NRF 发现 UDM 并订阅 Nudm-EE 事件通知；失败会指数退避重试并最终放弃。
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

// Terminate 停止 NWDAF：结束外部检测进程、向 NRF 注销并释放资源。
func (n *NWDAF) Terminate() {
	logger.InitLog.Printf("[INFO] Terminating NWDAF...")
	if n.ppCmd != nil && n.ppCmd.Process != nil {
		_ = n.ppCmd.Process.Kill()
		logger.InitLog.Printf("[INFO] Stopped ping-pong detector process.")
	}
	if n.anomalyCmd != nil && n.anomalyCmd.Process != nil {
		_ = n.anomalyCmd.Process.Kill()
		logger.InitLog.Printf("[INFO] Stopped anomaly detector process.")
	}
	// Deregister from NRF
	consumer.SendDeregisterNFInstance()
	logger.InitLog.Printf("[INFO] NWDAF terminated.")
}
