package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/hainuoli/latest-changes/internal/model"
)

const maxBodyBytes = 1_000_000

var validDeliveryID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type (
	enqueuer interface {
		Enqueue(id string, event model.Event) error
	}

	handler struct {
		secret  string
		version string
		queue   enqueuer
	}
)

// NewHandler registers the health and signed GitHub webhook endpoints.
func NewHandler(secret, version string, queue enqueuer) http.Handler {
	h := &handler{
		secret:  secret,
		version: version,
		queue:   queue,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.handleRoot)
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("POST /webhooks/github", h.handleWebhook)

	return mux
}

// VerifySignature compares the SHA-256 HMAC of the unmodified request body.
func VerifySignature(body []byte, signature, secret string) bool {
	if secret == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)

	return hmac.Equal(provided, mac.Sum(nil))
}

func (h *handler) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    "latest-changes",
		"version": h.version,
	})
}

func (h *handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "webhook payload too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "could not read payload", http.StatusBadRequest)
		}
		return
	}
	if !VerifySignature(body, r.Header.Get("X-Hub-Signature-256"), h.secret) {
		http.Error(w, "invalid webhook signature", http.StatusForbidden)
		return
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	eventType := r.Header.Get("X-GitHub-Event")
	if !validDeliveryID.MatchString(deliveryID) || eventType == "" {
		http.Error(w, "missing or invalid GitHub headers", http.StatusBadRequest)
		return
	}
	if eventType != "pull_request" {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "skipped",
		})
		return
	}

	var event model.Event
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid webhook JSON", http.StatusBadRequest)
		return
	}
	if err := event.Validate(); err != nil {
		http.Error(w, "invalid pull request payload", http.StatusBadRequest)
		return
	}
	if err := h.queue.Enqueue(deliveryID, event); err != nil {
		slog.Error("Could not persist webhook", "delivery", deliveryID, "error", err)
		http.Error(w, "webhook queue unavailable", http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "queued",
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
