package context

import (
	"context"

	"github.com/free5gc/openapi/models"
	"github.com/free5gc/openapi/oauth"
)

type NWDAFContext struct {
	URIScheme       string
	RegisterIPv4    string
	SBIPort         int
	NrfUri          string
	NfId            string
	NrfCertPem      string
	OAuth2Required  bool
}

func InitNwdafContext() *NWDAFContext {
	return &NWDAFContext{
		URIScheme:      "http",
		RegisterIPv4:   "127.0.0.1",
		SBIPort:        8001,
		NrfUri:         "http://127.0.0.10:8000",
		NfId:           "nwdaf-1",
		NrfCertPem:     "",
		OAuth2Required: false,
	}
}

func (c *NWDAFContext) GetNFProfile() interface{} {
	return nil
}

func (c *NWDAFContext) GetTokenCtx(serviceName models.ServiceName, targetNF models.NrfNfManagementNfType) (
	context.Context, *models.ProblemDetails, error,
) {
	if !c.OAuth2Required {
		return context.TODO(), nil, nil
	}
	return oauth.GetTokenCtx(models.NrfNfManagementNfType_NWDAF, targetNF,
		c.NfId, c.NrfUri, string(serviceName))
}

func (c *NWDAFContext) AuthorizationCheck(token string, serviceName models.ServiceName) error {
	if !c.OAuth2Required {
		return nil
	}
	return oauth.VerifyOAuth(token, string(serviceName), c.NrfCertPem)
}
