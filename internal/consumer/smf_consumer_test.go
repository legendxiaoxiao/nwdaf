package consumer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	nwdaf_context "github.com/free5gc/nwdaf/internal/context"
)

func TestGetAccessTokenForSmf_Success(t *testing.T) {
	token := "smf_token"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"`+token+`","token_type":"Bearer","expires_in":1000}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer s.Close()

	ctx := &nwdaf_context.NWDAFContext{NrfUri: s.URL, NfId: "nwdaf-1"}
	got, err := getAccessTokenForSmf(ctx)
	if err != nil {
		t.Fatalf("getAccessTokenForSmf error: %v", err)
	}
	if got != token {
		t.Fatalf("unexpected token: %s", got)
	}
}

func TestSubscribeToSmfEvents_Success(t *testing.T) {
	token := "smf_token"
	var capturedAuth string
	var capturedBody map[string]interface{}

	subPath := "/nsmf_event-exposure/v1/subscriptions"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"`+token+`","token_type":"Bearer","expires_in":1000}`)
			return
		case r.Method == http.MethodPost && r.URL.Path == subPath:
			capturedAuth = r.Header.Get("Authorization")
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &capturedBody)
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{}`)
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	ctx := &nwdaf_context.NWDAFContext{
		URIScheme:      "http",
		RegisterIPv4:   "127.0.0.1",
		SBIPort:        8001,
		NrfUri:         s.URL,
		NfId:           "nwdaf-1",
		OAuth2Required: false,
	}
	smf := &SmfProfile{EventExposureUrl: s.URL + subPath}

	if err := SubscribeToSmfEvents(ctx, smf); err != nil {
		t.Fatalf("SubscribeToSmfEvents error: %v", err)
	}
	if capturedAuth != "Bearer "+token {
		t.Fatalf("unexpected Authorization: %s", capturedAuth)
	}
	notifUri, _ := capturedBody["notifUri"].(string)
	expectUri := "http://127.0.0.1:8001/nnwdaf-events/v1/smf-notifications"
	if notifUri != expectUri {
		t.Fatalf("unexpected notifUri: %s", notifUri)
	}
	ev, _ := capturedBody["eventList"].([]interface{})
	if len(ev) != 2 {
		t.Fatalf("unexpected eventList length: %d", len(ev))
	}
}