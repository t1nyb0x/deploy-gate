package webhook

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/t1nyb0x/deploy-gate/internal/config"
	"github.com/t1nyb0x/deploy-gate/internal/signature"
)

const maxBodySize = 1 << 20 // 1MB

// Runner executes a deploy script and returns its combined output.
type Runner func(script string) (string, error)

type deployResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type pushPayload struct {
	Ref     string `json:"ref"`
	Deleted bool   `json:"deleted"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func Deploy(secret string, route config.Route, run Runner) http.HandlerFunc {
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

		var payload pushPayload
		if err := json.Unmarshal(body, &payload); err != nil {
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

		go func() {
			output, err := run(route.Script)
			if err != nil {
				log.Printf("deploy failed: script=%s error=%v output=%s", route.Script, err, output)
				return
			}
			log.Printf("deploy succeeded: script=%s output=%s", route.Script, output)
		}()

		writeJSON(w, http.StatusAccepted, deployResponse{Status: "accepted"})
	}
}
