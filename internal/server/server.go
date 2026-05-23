package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/MHMALEK/gcp-relay/internal/router"
)

type Server struct {
	router *router.Router
	log    *log.Logger
}

func New(r *router.Router, logger *log.Logger) *Server {
	return &Server{router: r, log: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /events/gcs", s.handleGCSEvent)
	mux.HandleFunc("POST /events/pubsub/{topic}", s.handlePubSubPush)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
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

	if err := s.router.DeliverGCS(r.Context(), body.Bucket, body.Name); err != nil {
		s.log.Printf("gcs delivery failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"delivered"}`))
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

	if err := s.router.HandlePubSubPush(r.Context(), topic, body); err != nil {
		s.log.Printf("pubsub delivery failed topic=%s: %v", topic, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusOK)
}
