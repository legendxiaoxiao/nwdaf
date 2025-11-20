package consumer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	nwdaf_context "github.com/free5gc/nwdaf/internal/context"
)

func TestGetAccessToken_Success(t *testing.T) {
	token := "t123"
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
	got, err := getAccessToken(ctx)
	if err != nil {
		t.Fatalf("getAccessToken error: %v", err)
	}
	if got != token {
		t.Fatalf("unexpected token: %s", got)
	}
}

func TestSubscribeToAmfEvents_Success(t *testing.T) {
	token := "t123"
	var capturedAuth string
	var capturedBody map[string]interface{}

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"`+token+`","token_type":"Bearer","expires_in":1000}`)
			return
		case "/namf-evts/v1/subscriptions":
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
		URIScheme:     "http",
		RegisterIPv4:  "127.0.0.1",
		SBIPort:       8001,
		NrfUri:        s.URL,
		NfId:          "nwdaf-1",
		OAuth2Required: false,
	}
	amf := &AmfProfile{EventExposureUrl: s.URL + "/namf-evts/v1/subscriptions"}

	err := SubscribeToAmfEvents(ctx, amf)
	if err != nil {
		t.Fatalf("SubscribeToAmfEvents error: %v", err)
	}

	if capturedAuth != "Bearer "+token {
		t.Fatalf("unexpected Authorization header: %s", capturedAuth)
	}

	sub, ok := capturedBody["subscription"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing subscription in body")
	}
	uri, _ := sub["eventNotifyUri"].(string)
	expectURI := fmt.Sprintf("%s://%s:%d/nnwdaf-events/v1/notifications", ctx.URIScheme, ctx.RegisterIPv4, ctx.SBIPort)
	if uri != expectURI {
		t.Fatalf("unexpected eventNotifyUri: %s", uri)
	}
	eventList, _ := sub["eventList"].([]interface{})
	if len(eventList) != 3 {
		t.Fatalf("unexpected eventList length: %d", len(eventList))
	}
}

func TestSendRegisterNFInstance_Success(t *testing.T) {
	var putBody bytes.Buffer
	var gotMethod, gotPath string
	var gotContentType string

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"x","token_type":"Bearer","expires_in":1000}`)
			return
		case r.Method == http.MethodPut && r.URL.Path == "/nnrf-nfm/v1/nf-instances/nwdaf-1":
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotContentType = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			putBody.Write(b)
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `{}`)
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()

	err := SendRegisterNFInstance(s.URL, "nwdaf-1", nil)
	if err != nil {
		t.Fatalf("SendRegisterNFInstance error: %v", err)
	}
	if gotMethod != "PUT" {
		t.Fatalf("unexpected method: %s", gotMethod)
	}
	if gotPath != "/nnrf-nfm/v1/nf-instances/nwdaf-1" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected content-type: %s", gotContentType)
	}
	if putBody.Len() == 0 {
		t.Fatalf("empty body")
	}
}