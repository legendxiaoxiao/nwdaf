package consumer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	nwdaf_context "github.com/free5gc/nwdaf/internal/context"
	"github.com/free5gc/nwdaf/internal/util"

	"github.com/free5gc/openapi/models"
	Nnrf_NFDiscovery "github.com/free5gc/openapi/nrf/NFDiscovery"
)

type AmfProfile struct {
	EventExposureUrl string
}

func DiscoverAmfFromNrf(ctx *nwdaf_context.NWDAFContext) (*AmfProfile, error) {
	cfg := Nnrf_NFDiscovery.NewConfiguration()
	cfg.SetBasePath(ctx.NrfUri)
	client := Nnrf_NFDiscovery.NewAPIClient(cfg)
	c, _, err := ctx.GetTokenCtx(models.ServiceName_NNRF_DISC, models.NrfNfManagementNfType_NRF)
	if err != nil { return nil, err }
	req := Nnrf_NFDiscovery.SearchNFInstancesRequest{ServiceNames: []models.ServiceName{models.ServiceName_NAMF_EVTS}}
	t := models.NrfNfManagementNfType_AMF
	r := models.NrfNfManagementNfType_NWDAF
	req.TargetNfType = &t
	req.RequesterNfType = &r
	res, err := client.NFInstancesStoreApi.SearchNFInstances(c, &req)
	if err != nil || res == nil || len(res.SearchResult.NfInstances) == 0 { return nil, fmt.Errorf("discover AMF failed: %+v", err) }
	var base string
	for i := range res.SearchResult.NfInstances {
		base = util.SearchNFServiceUri(&res.SearchResult.NfInstances[i], models.ServiceName_NAMF_EVTS)
		if base != "" { break }
	}
	if base == "" { return nil, fmt.Errorf("no NAMF_EVTS service found") }
	if !strings.Contains(base, "/namf-evts") { base = strings.TrimRight(base, "/") + "/namf-evts/v1" }
	url := strings.TrimRight(base, "/") + "/subscriptions"
	return &AmfProfile{EventExposureUrl: url}, nil
}



// 从NRF获取访问令牌（AMF）
func getAccessToken(nwdafCtx *nwdaf_context.NWDAFContext) (string, error) {
	// 构造获取访问令牌的请求体（使用form格式）
	tokenReq := "grant_type=client_credentials&nfType=NWDAF&targetNfType=AMF&nfInstanceId=" + nwdafCtx.NfId + "&targetNfInstanceId=amf-1&scope=namf-evts"

	fmt.Printf("[DEBUG] Token请求体: %s\n", tokenReq)

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

	fmt.Printf("[DEBUG] NRF返回状态: %d\n", resp.StatusCode)
	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("[DEBUG] NRF返回内容: %s\n", string(respBody))

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("获取访问令牌失败: %s", resp.Status)
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

// 向AMF订阅事件
func SubscribeToAmfEvents(nwdafCtx *nwdaf_context.NWDAFContext, amfProfile *AmfProfile) error {
	// 构造订阅请求体
	subBody := map[string]interface{}{
		"subscription": map[string]interface{}{
			"eventList": []map[string]interface{}{
				{"type": "REGISTRATION_STATE_REPORT"},
				{"type": "LOCATION_REPORT"},
				{"type": "REACHABILITY_REPORT"},
				{"type": "REG_REQUEST_COUNT"},
				{"type": "ACTIVE_UE_COUNT"},
			},
			"eventNotifyUri": fmt.Sprintf("%s://%s:%d/nnwdaf-events/v1/notifications",
				nwdafCtx.URIScheme, nwdafCtx.RegisterIPv4, nwdafCtx.SBIPort),
			"anyUE": true,
		},
	}

	data, _ := json.Marshal(subBody)

	// 获取OAuth2令牌上下文
	ctx, _, err := nwdafCtx.GetTokenCtx(models.ServiceName_NAMF_EVTS, models.NrfNfManagementNfType_AMF)
	if err != nil {
		return fmt.Errorf("GetTokenCtx error: %+v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", amfProfile.EventExposureUrl, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("NF-Type", "NWDAF")
	req.Header.Set("NF-Instance-Id", "nwdaf-1")

	// 获取 accessToken
	accessToken, err := getAccessToken(nwdafCtx)
	if err != nil {
		return fmt.Errorf("getAccessToken error: %+v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return fmt.Errorf("AMF订阅失败: %s", resp.Status)
	}
	return nil
}

func SendRegisterNFInstance(nrfUri, nfId string, profile interface{}) error {
	// 构造NWDAF的NF Profile
	nfProfile := map[string]interface{}{
		"nfInstanceId":   nfId,
		"nfType":         "NWDAF",
		"nfStatus":       "REGISTERED",
		"heartBeatTimer": 10,
		"nfServices": []map[string]interface{}{
			{
				"serviceInstanceId": "1",
				"serviceName":       "nnwdaf-events",
				"versions": []map[string]interface{}{
					{
						"apiVersionInUri": "v1",
						"apiFullVersion":  "1.0.0",
					},
				},
				"scheme":          "http",
				"nfServiceStatus": "REGISTERED",
				"ipEndPoints": []map[string]interface{}{
					{
						"ipv4Address": "127.0.0.1",
						"transport":   "TCP",
						"port":        8001,
					},
				},
			},
		},
		"customInfo": map[string]interface{}{
			"oauth2": true,
		},
	}

	data, _ := json.Marshal(nfProfile)
	req, err := http.NewRequest("PUT", nrfUri+"/nnrf-nfm/v1/nf-instances/"+nfId, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	// 自动获取OAuth2 token并加到header
	token, err := getAccessToken(&nwdaf_context.NWDAFContext{
		NrfUri: nrfUri,
		NfId:   nfId,
	})
	if err == nil && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return fmt.Errorf("NRF注册失败: %s", resp.Status)
	}

	return nil
}

func SendDeregisterNFInstance() {
	// 实现注销逻辑
}
