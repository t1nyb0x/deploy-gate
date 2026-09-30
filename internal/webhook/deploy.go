package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"

	"github.com/t1nyb0x/deploy-gate/internal/config"
	"github.com/t1nyb0x/deploy-gate/internal/signature"
)

const maxBodySize = 1 << 20 // 1MB

// Deployer starts a deploy. Trigger reports whether the request was queued
// behind a deploy already in progress, or an error if deploys are no longer accepted.
type Deployer interface {
	Trigger() (queued bool, err error)
}

type deployResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type pushPayload struct {
	Ref     string `json:"ref"`
	Deleted bool   `json:"deleted"`
}

// parsePushPayload decodes a push payload sent as either application/json or
// application/x-www-form-urlencoded (GitHub's default), where the JSON is in the "payload" field.
func parsePushPayload(contentType string, body []byte) (pushPayload, error) {
	raw := body
	if mediaType, _, _ := mime.ParseMediaType(contentType); mediaType == "application/x-www-form-urlencoded" {
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return pushPayload{}, fmt.Errorf("parse form: %w", err)
		}
		if !values.Has("payload") {
			return pushPayload{}, errors.New("form has no payload field")
		}
		raw = []byte(values.Get("payload"))
	}

	var payload pushPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return pushPayload{}, fmt.Errorf("decode payload: %w", err)
	}
	return payload, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func Deploy(secret string, route config.Route, d Deployer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		defer r.Body.Close()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		sig := r.Header.Get("X-Hub-Signature-256")
		if !signature.Validate(body, sig, secret) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		switch event := r.Header.Get("X-GitHub-Event"); event {
		case "ping":
			writeJSON(w, http.StatusOK, deployResponse{Status: "pong"})
			return
		case "push":
		default:
			log.Printf("ignored: path=%s event=%q", route.Path, event)
			writeJSON(w, http.StatusOK, deployResponse{Status: "ignored", Reason: "unsupported event"})
			return
		}

		payload, err := parsePushPayload(r.Header.Get("Content-Type"), body)
		if err != nil {
			log.Printf("bad request: path=%s error=%v", route.Path, err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if payload.Deleted {
			log.Printf("ignored: path=%s ref=%s reason=deleted", route.Path, payload.Ref)
			writeJSON(w, http.StatusOK, deployResponse{Status: "ignored", Reason: "ref deleted"})
			return
		}

		if route.Branch != "" && payload.Ref != "refs/heads/"+route.Branch {
			log.Printf("ignored: path=%s ref=%s branch=%s", route.Path, payload.Ref, route.Branch)
			writeJSON(w, http.StatusOK, deployResponse{Status: "ignored", Reason: "branch mismatch"})
			return
		}

		queued, err := d.Trigger()
		if err != nil {
			log.Printf("rejected: path=%s error=%v", route.Path, err)
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		if queued {
			log.Printf("queued: path=%s script=%s reason=deploy in progress", route.Path, route.Script)
			writeJSON(w, http.StatusAccepted, deployResponse{Status: "queued"})
			return
		}

		writeJSON(w, http.StatusAccepted, deployResponse{Status: "accepted"})
	}
}
