package util

import (
	"fmt"
	"github.com/free5gc/openapi/models"
)

func GetNfamfClient(amfUri string) interface{} { return nil }

func SearchNFServiceUri(p *models.NrfNfDiscoveryNfProfile, name models.ServiceName) string {
	if p.NfServices != nil {
		for _, s := range p.NfServices {
			if s.ServiceName == name && s.NfServiceStatus == models.NfServiceStatus_REGISTERED {
				if p.Fqdn != "" {
					return p.Fqdn
				}
				if s.Fqdn != "" {
					return s.Fqdn
				}
				if s.ApiPrefix != "" {
					return s.ApiPrefix
				}
				if len(s.IpEndPoints) > 0 {
					ip := s.IpEndPoints[0].Ipv4Address
					port := s.IpEndPoints[0].Port
					if ip == "" && len(p.Ipv4Addresses) > 0 {
						ip = p.Ipv4Addresses[0]
					}
					scheme := "http"
					if s.Scheme == models.UriScheme_HTTPS {
						scheme = "https"
					}
					if port != 0 {
						return fmt.Sprintf("%s://%s:%d", scheme, ip, port)
					}
					if scheme == "https" {
						return fmt.Sprintf("https://%s:443", ip)
					}
					return fmt.Sprintf("http://%s:80", ip)
				}
			}
		}
	}
	return ""
}