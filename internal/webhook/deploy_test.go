package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/t1nyb0x/deploy-gate/internal/config"
)

const testSecret = "test-secret"

func sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type fakeDeployer struct {
	calls  int
	queued bool
	err    error
}

func (f *fakeDeployer) Trigger() (bool, error) {
	f.calls++
	return f.queued, f.err
}

func doRequest(t *testing.T, h http.HandlerFunc, method, event string, body []byte, sig string) (*httptest.ResponseRecorder, deployResponse) {
	t.Helper()
	req := httptest.NewRequest(method, "/deploy/test", strings.NewReader(string(body)))
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	rec := httptest.NewRecorder()
	h(rec, req)

	var resp deployResponse
	if rec.Header().Get("Content-Type") == "application/json" {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return rec, resp
}

func TestDeploy(t *testing.T) {
	route := config.Route{Path: "/deploy/test", Script: "/scripts/test.sh", Branch: "main"}
	pushMain := []byte(`{"ref":"refs/heads/main","deleted":false}`)

	tests := []struct {
		name       string
		route      config.Route
		method     string
		event      string
		body       []byte
		sig        string
		wantCode   int
		wantStatus string
		wantDeploy bool
	}{
		{
			name:     "non-POST is rejected",
			route:    route,
			method:   http.MethodGet,
			event:    "push",
			body:     pushMain,
			sig:      sign(pushMain),
			wantCode: http.StatusForbidden,
		},
		{
			name:     "invalid signature is rejected",
			route:    route,
			method:   http.MethodPost,
			event:    "push",
			body:     pushMain,
			sig:      "sha256=deadbeef",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "invalid signature is rejected before event check",
			route:    route,
			method:   http.MethodPost,
			event:    "ping",
			body:     []byte(`{}`),
			sig:      "sha256=deadbeef",
			wantCode: http.StatusForbidden,
		},
		{
			name:       "ping does not deploy",
			route:      route,
			method:     http.MethodPost,
			event:      "ping",
			body:       []byte(`{"zen":"hello"}`),
			sig:        sign([]byte(`{"zen":"hello"}`)),
			wantCode:   http.StatusOK,
			wantStatus: "pong",
		},
		{
			name:       "non-push event is ignored",
			route:      route,
			method:     http.MethodPost,
			event:      "pull_request",
			body:       pushMain,
			sig:        sign(pushMain),
			wantCode:   http.StatusOK,
			wantStatus: "ignored",
		},
		{
			name:       "missing event header is ignored",
			route:      route,
			method:     http.MethodPost,
			event:      "",
			body:       pushMain,
			sig:        sign(pushMain),
			wantCode:   http.StatusOK,
			wantStatus: "ignored",
		},
		{
			name:       "push to other branch is ignored",
			route:      route,
			method:     http.MethodPost,
			event:      "push",
			body:       []byte(`{"ref":"refs/heads/feature/x"}`),
			sig:        sign([]byte(`{"ref":"refs/heads/feature/x"}`)),
			wantCode:   http.StatusOK,
			wantStatus: "ignored",
		},
		{
			name:       "push of tag with same name is ignored",
			route:      route,
			method:     http.MethodPost,
			event:      "push",
			body:       []byte(`{"ref":"refs/tags/main"}`),
			sig:        sign([]byte(`{"ref":"refs/tags/main"}`)),
			wantCode:   http.StatusOK,
			wantStatus: "ignored",
		},
		{
			name:       "branch deletion is ignored",
			route:      route,
			method:     http.MethodPost,
			event:      "push",
			body:       []byte(`{"ref":"refs/heads/main","deleted":true}`),
			sig:        sign([]byte(`{"ref":"refs/heads/main","deleted":true}`)),
			wantCode:   http.StatusOK,
			wantStatus: "ignored",
		},
		{
			name:     "malformed push payload is rejected",
			route:    route,
			method:   http.MethodPost,
			event:    "push",
			body:     []byte(`not json`),
			sig:      sign([]byte(`not json`)),
			wantCode: http.StatusBadRequest,
		},
		{
			name:       "push to configured branch deploys",
			route:      route,
			method:     http.MethodPost,
			event:      "push",
			body:       pushMain,
			sig:        sign(pushMain),
			wantCode:   http.StatusAccepted,
			wantStatus: "accepted",
			wantDeploy: true,
		},
		{
			name:       "push to any branch deploys when branch is not configured",
			route:      config.Route{Path: "/deploy/test", Script: "/scripts/test.sh"},
			method:     http.MethodPost,
			event:      "push",
			body:       []byte(`{"ref":"refs/heads/feature/x"}`),
			sig:        sign([]byte(`{"ref":"refs/heads/feature/x"}`)),
			wantCode:   http.StatusAccepted,
			wantStatus: "accepted",
			wantDeploy: true,
		},
		{
			name:       "branch deletion is ignored even when branch is not configured",
			route:      config.Route{Path: "/deploy/test", Script: "/scripts/test.sh"},
			method:     http.MethodPost,
			event:      "push",
			body:       []byte(`{"ref":"refs/heads/feature/x","deleted":true}`),
			sig:        sign([]byte(`{"ref":"refs/heads/feature/x","deleted":true}`)),
			wantCode:   http.StatusOK,
			wantStatus: "ignored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeDeployer{}
			h := Deploy(testSecret, tt.route, d)

			res, resp := doRequest(t, h, tt.method, tt.event, tt.body, tt.sig)

			if res.Code != tt.wantCode {
				t.Fatalf("code = %d, want %d (body=%q)", res.Code, tt.wantCode, res.Body.String())
			}
			if tt.wantStatus != "" && resp.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", resp.Status, tt.wantStatus)
			}

			wantCalls := 0
			if tt.wantDeploy {
				wantCalls = 1
			}
			if d.calls != wantCalls {
				t.Errorf("deploy triggered %d times, want %d", d.calls, wantCalls)
			}
		})
	}
}

func TestDeployQueuedWhileRunning(t *testing.T) {
	route := config.Route{Path: "/deploy/test", Script: "/scripts/test.sh", Branch: "main"}
	body := []byte(`{"ref":"refs/heads/main"}`)
	d := &fakeDeployer{queued: true}

	res, resp := doRequest(t, Deploy(testSecret, route, d), http.MethodPost, "push", body, sign(body))

	if res.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want %d", res.Code, http.StatusAccepted)
	}
	if resp.Status != "queued" {
		t.Errorf("status = %q, want %q", resp.Status, "queued")
	}
	if d.calls != 1 {
		t.Errorf("deploy triggered %d times, want 1", d.calls)
	}
}

func TestDeployUnavailableWhenShuttingDown(t *testing.T) {
	route := config.Route{Path: "/deploy/test", Script: "/scripts/test.sh", Branch: "main"}
	body := []byte(`{"ref":"refs/heads/main"}`)
	d := &fakeDeployer{err: errors.New("closed")}

	res, _ := doRequest(t, Deploy(testSecret, route, d), http.MethodPost, "push", body, sign(body))

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
}

// formBody encodes a JSON payload the way GitHub does for the
// application/x-www-form-urlencoded content type (the GitHub default).
func formBody(payload string) []byte {
	return []byte(url.Values{"payload": {payload}}.Encode())
}

func TestDeployContentTypes(t *testing.T) {
	route := config.Route{Path: "/deploy/test", Script: "/scripts/test.sh", Branch: "main"}

	tests := []struct {
		name        string
		contentType string
		body        []byte
		wantCode    int
		wantStatus  string
		wantDeploy  bool
	}{
		{
			name:        "json push deploys",
			contentType: "application/json",
			body:        []byte(`{"ref":"refs/heads/main"}`),
			wantCode:    http.StatusAccepted,
			wantStatus:  "accepted",
			wantDeploy:  true,
		},
		{
			name:        "form push deploys",
			contentType: "application/x-www-form-urlencoded",
			body:        formBody(`{"ref":"refs/heads/main"}`),
			wantCode:    http.StatusAccepted,
			wantStatus:  "accepted",
			wantDeploy:  true,
		},
		{
			name:        "form content type with parameters",
			contentType: "application/x-www-form-urlencoded; charset=utf-8",
			body:        formBody(`{"ref":"refs/heads/main"}`),
			wantCode:    http.StatusAccepted,
			wantStatus:  "accepted",
			wantDeploy:  true,
		},
		{
			name:        "form push to other branch is ignored",
			contentType: "application/x-www-form-urlencoded",
			body:        formBody(`{"ref":"refs/heads/feature/x"}`),
			wantCode:    http.StatusOK,
			wantStatus:  "ignored",
		},
		{
			name:        "form branch deletion is ignored",
			contentType: "application/x-www-form-urlencoded",
			body:        formBody(`{"ref":"refs/heads/main","deleted":true}`),
			wantCode:    http.StatusOK,
			wantStatus:  "ignored",
		},
		{
			name:        "form without payload field is rejected",
			contentType: "application/x-www-form-urlencoded",
			body:        []byte(`foo=bar`),
			wantCode:    http.StatusBadRequest,
		},
		{
			name:        "form with malformed payload is rejected",
			contentType: "application/x-www-form-urlencoded",
			body:        formBody(`not json`),
			wantCode:    http.StatusBadRequest,
		},
		{
			name:        "missing content type is treated as json",
			contentType: "",
			body:        []byte(`{"ref":"refs/heads/main"}`),
			wantCode:    http.StatusAccepted,
			wantStatus:  "accepted",
			wantDeploy:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeDeployer{}
			req := httptest.NewRequest(http.MethodPost, route.Path, strings.NewReader(string(tt.body)))
			req.Header.Set("X-GitHub-Event", "push")
			req.Header.Set("X-Hub-Signature-256", sign(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()

			Deploy(testSecret, route, d)(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("code = %d, want %d (body=%q)", rec.Code, tt.wantCode, rec.Body.String())
			}
			if tt.wantStatus != "" {
				var resp deployResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if resp.Status != tt.wantStatus {
					t.Errorf("status = %q, want %q", resp.Status, tt.wantStatus)
				}
			}
			wantCalls := 0
			if tt.wantDeploy {
				wantCalls = 1
			}
			if d.calls != wantCalls {
				t.Errorf("deploy triggered %d times, want %d", d.calls, wantCalls)
			}
		})
	}
}
