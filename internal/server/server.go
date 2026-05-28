package server

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/MHMALEK/gcp-relay/internal/bootstrap"
	"github.com/MHMALEK/gcp-relay/internal/history"
	"github.com/MHMALEK/gcp-relay/internal/router"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	router  *router.Router
	history *history.Store
	log     *log.Logger
}

func New(r *router.Router, store *history.Store, logger *log.Logger) *Server {
	return &Server{router: r, history: store, log: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /events", s.handleListEvents)
	mux.HandleFunc("GET /events/{id}", s.handleGetEvent)
	mux.HandleFunc("POST /events/{id}/replay", s.handleReplayEvent)
	mux.HandleFunc("POST /events/gcs", s.handleGCSEvent)
	mux.HandleFunc("POST /hooks/pubsub/{topic}", s.handlePubSubPush)
	mux.HandleFunc("POST /admin/bootstrap", s.handleAdminBootstrap)

	staticFS, _ := fs.Sub(staticFiles, "static")
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListEvents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.history.List())
}

func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.history.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleReplayEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.history.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	replayed, err := s.router.Replay(r.Context(), rec)
	if err != nil {
		s.log.Printf("replay failed id=%s: %v", id, err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"status": "error", "error": err.Error(), "record": replayed})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "delivered", "record": replayed})
}

func (s *Server) handleGCSEvent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bucket string `json:"bucket"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.Bucket == "" || body.Name == "" {
		http.Error(w, "bucket and name are required", http.StatusBadRequest)
		return
	}

	rec, err := s.router.DeliverGCS(r.Context(), body.Bucket, body.Name)
	if err != nil {
		s.log.Printf("gcs delivery failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"status": "error", "error": err.Error(), "record": rec})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "delivered", "record": rec})
}

func (s *Server) handlePubSubPush(w http.ResponseWriter, r *http.Request) {
	topic := strings.TrimPrefix(r.PathValue("topic"), "/")
	if topic == "" {
		http.Error(w, "topic required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	rec, err := s.router.HandlePubSubPush(r.Context(), topic, body)
	if err != nil {
		s.log.Printf("pubsub delivery failed topic=%s: %v", topic, err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"status": "error", "error": err.Error(), "record": rec})
		return
	}

	w.WriteHeader(http.StatusOK)
}

// adminBootstrapRequest is the request body for POST /admin/bootstrap.
// Any omitted field falls back to the relay's defaults (env vars or
// hard-coded fallbacks in internal/bootstrap.DefaultOptions).
type adminBootstrapRequest struct {
	ProjectID  string `json:"project_id,omitempty"`
	Topic      string `json:"topic,omitempty"`
	Bucket     string `json:"bucket,omitempty"`
	PushURL    string `json:"push_url,omitempty"`
	PubSubHost string `json:"pubsub_host,omitempty"`
	GCSHost    string `json:"gcs_host,omitempty"`
}

func (s *Server) handleAdminBootstrap(w http.ResponseWriter, r *http.Request) {
	opts := bootstrap.DefaultOptions()

	if r.ContentLength > 0 {
		var req adminBootstrapRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.ProjectID != "" {
			opts.ProjectID = req.ProjectID
		}
		if req.Topic != "" {
			opts.Topic = req.Topic
		}
		if req.Bucket != "" {
			opts.Bucket = req.Bucket
		}
		if req.PushURL != "" {
			opts.PushRelayURL = req.PushURL
		}
		if req.PubSubHost != "" {
			opts.PubSubHost = req.PubSubHost
		}
		if req.GCSHost != "" {
			opts.GCSHost = req.GCSHost
		}
	}

	if err := bootstrap.Run(opts); err != nil {
		s.log.Printf("admin bootstrap failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"status": "error", "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"project_id": opts.ProjectID,
		"topic":      opts.Topic,
		"bucket":     opts.Bucket,
		"push_url":   opts.PushRelayURL,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
