package consumer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	nwdaf_context "github.com/free5gc/nwdaf/internal/context"
	"github.com/free5gc/openapi/models"
)

// SMF相关的消费者函数
type SmfProfile struct {
	EventExposureUrl string
}

// DiscoverSmfFromNrf: 发现SMF实例(别的网元是否能发现，云原生实现)
func DiscoverSmfFromNrf(ctx *nwdaf_context.NWDAFContext) (*SmfProfile, error) {
	// 根据 smfcfg.yaml 配置，SMF 事件订阅接口为 127.0.0.2:8000
	return &SmfProfile{
		EventExposureUrl: "http://127.0.0.2:8000/nsmf_event-exposure/v1/subscriptions",
	}, nil
}

// SubscribeToSmfEvents: 向SMF订阅事件
func SubscribeToSmfEvents(nwdafCtx *nwdaf_context.NWDAFContext, smfProfile *SmfProfile) error {
	// 构造SMF事件订阅请求体，仅订阅 PDU_SESSION_MODIFICATION
	subBody := map[string]interface{}{
			"notifUri": fmt.Sprintf("%s://%s:%d/nnwdaf-events/v1/smf-notifications",
				nwdafCtx.URIScheme, nwdafCtx.RegisterIPv4, nwdafCtx.SBIPort),
			"eventList": []string{
				"PDU_SESSION_ESTABLISHMENT",
				"PDU_SESSION_MODIFICATION",
				"PDU_SESSION_RELEASE",
			},
		}

	data, _ := json.Marshal(subBody)

	// 打印订阅请求体，便于调试
	fmt.Printf("SMF订阅请求体: %s\n", string(data))

	// 获取OAuth2令牌上下文
	ctx, _, err := nwdafCtx.GetTokenCtx(models.ServiceName_NSMF_EVENT_EXPOSURE, models.NrfNfManagementNfType_SMF)
	if err != nil {
		return fmt.Errorf("GetTokenCtx error: %+v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", smfProfile.EventExposureUrl, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("NF-Type", "NWDAF")
	req.Header.Set("NF-Instance-Id", "nwdaf-1")

	// 获取 accessToken
	accessToken, err := getAccessTokenForSmf(nwdafCtx)
	if err != nil {
		return fmt.Errorf("getAccessTokenForSmf error: %+v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	fmt.Printf("SMF订阅token: %s\n", accessToken)
	fmt.Printf("SMF订阅请求头: %v\n", req.Header)
	fmt.Printf("SMF订阅URL: %s\n", smfProfile.EventExposureUrl)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return fmt.Errorf("SMF订阅失败: %s", resp.Status)
	}
	return nil
}

// 从NRF获取SMF访问令牌
func getAccessTokenForSmf(nwdafCtx *nwdaf_context.NWDAFContext) (string, error) {
	// 构造获取访问令牌的请求体（使用form格式）
	tokenReq := "grant_type=client_credentials&nfType=NWDAF&targetNfType=SMF&nfInstanceId=" + nwdafCtx.NfId + "&targetNfInstanceId=smf-1&scope=nsmf-event-exposure"

	fmt.Printf("[DEBUG] SMF Token请求体: %s\n", tokenReq)

	req, err := http.NewRequest("POST", nwdafCtx.NrfUri+"/oauth2/token", strings.NewReader(tokenReq))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	fmt.Printf("[DEBUG] SMF NRF返回状态: %d\n", resp.StatusCode)
	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("[DEBUG] SMF NRF返回内容: %s\n", string(respBody))

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("获取SMF访问令牌失败: %s", resp.Status)
	}

	var tokenResp map[string]interface{}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", err
	}

	accessToken, ok := tokenResp["access_token"].(string)
	if !ok {
		return "", fmt.Errorf("响应中没有access_token")
	}

	return accessToken, nil
}