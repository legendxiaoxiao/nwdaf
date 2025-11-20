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

type UdmProfile struct {
	// Base path e.g. http://127.0.0.3:8000/nudm-ee/v1
	EventExposureBaseUrl string
}

// DiscoverUdmFromNrf: 这里直接返回一个硬编码的 UDM EE 前缀地址
// 可后续替换成NRF发现逻辑，类似AMF/SMF
func DiscoverUdmFromNrf(ctx *nwdaf_context.NWDAFContext) (*UdmProfile, error) {
	return &UdmProfile{
		EventExposureBaseUrl: "http://127.0.0.3:8000/nudm-ee/v1",
	}, nil
}

// 从NRF获取UDM访问令牌
func getAccessTokenForUdm(nwdafCtx *nwdaf_context.NWDAFContext) (string, error) {
	// 3GPP OAuth2 Client Credentials: targetNfType=UDM, scope=nudm-ee
	tokenReq := "grant_type=client_credentials&nfType=NWDAF&targetNfType=UDM&nfInstanceId=" + nwdafCtx.NfId + "&targetNfInstanceId=udm-1&scope=nudm-ee"

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

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("获取UDM访问令牌失败: %s, body=%s", resp.Status, string(respBody))
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

// SubscribeToUdmEeEvents: 向UDM创建Nudm-EE订阅，ueIdentity=anyUE
func SubscribeToUdmEeEvents(nwdafCtx *nwdaf_context.NWDAFContext, udm *UdmProfile) error {
	// 3GPP路径: {apiRoot}/nudm-ee/v1/{ueIdentity}/ee-subscriptions
	subscriptionUrl := fmt.Sprintf("%s/%s/ee-subscriptions", udm.EventExposureBaseUrl, "anyUE")

	// NWDAF回调地址，接收UDM EE通知
	callbackUri := fmt.Sprintf("%s://%s:%d/nnwdaf-events/v1/udm-ee-notifications",
		nwdafCtx.URIScheme, nwdafCtx.RegisterIPv4, nwdafCtx.SBIPort)

	// 构造订阅请求体（与AMF/SMF一致使用原生JSON）
	// 字段名依据3GPP TS 29.503 Nudm-EE的EeSubscription模型[4][1]：
	// - eventList: 列出需要的事件类型（示例字段名采用通用写法，具体枚举应根据实现支持调整）
	// - notifUri: NWDAF接收通知的回调地址
	// - nfInstanceId: NWDAF自身实例ID
	// - anyUE: 订阅范围为任意UE（与路径anyUE相匹配）
	subBody := map[string]interface{}{
		"eventList": []map[string]interface{}{
			// 示例事件类型，按需替换为UDM支持的枚举
			{"event": "SUBSCRIPTION_DATA_CHANGE"},
			{"event": "AMF_REGISTRATION_STATE"},
		},
		"notifUri":     callbackUri,
		"nfInstanceId": nwdafCtx.NfId,
		"anyUE":        true,
	}

	data, _ := json.Marshal(subBody)

	// OAuth2上下文（SDK使用），用于派生请求context
	ctx, _, err := nwdafCtx.GetTokenCtx(models.ServiceName_NUDM_EE, models.NrfNfManagementNfType_UDM)
	if err != nil {
		return fmt.Errorf("GetTokenCtx error: %+v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", subscriptionUrl, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("NF-Type", "NWDAF")
	req.Header.Set("NF-Instance-Id", nwdafCtx.NfId)

	// 获取访问令牌并加入鉴权头
	accessToken, err := getAccessTokenForUdm(nwdafCtx)
	if err != nil {
		return fmt.Errorf("getAccessTokenForUdm error: %+v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("UDM订阅失败: %s, body=%s", resp.Status, string(body))
	}
	return nil
}