package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ccplant/ccplant/e2e/internal/catalog"
	"github.com/ccplant/ccplant/e2e/internal/domain"
	"github.com/ccplant/ccplant/e2e/internal/store"
)

type Server struct {
	store   *store.Store
	catalog *domain.Catalog
	mux     *http.ServeMux
	secrets sync.Map
}

func New(s *store.Store, c *domain.Catalog) *Server {
	x := &Server{store: s, catalog: c, mux: http.NewServeMux()}
	x.routes()
	return x
}
func (s *Server) Handler() http.Handler { return cors(s.mux) }
func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	s.mux.HandleFunc("GET /api/v1/scenarios", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.catalog) })
	s.mux.HandleFunc("POST /api/v1/runs", s.createRun)
	s.mux.HandleFunc("GET /api/v1/runs", s.listRuns)
	s.mux.HandleFunc("GET /api/v1/runs/{id}", s.getRun)
	s.mux.HandleFunc("POST /api/v1/runs/{id}/cancel", s.cancelRun)
	s.mux.HandleFunc("GET /api/v1/runs/{id}/events", s.streamEvents)
	s.mux.HandleFunc("POST /internal/v1/runs/claim", s.claim)
	s.mux.HandleFunc("POST /internal/v1/runs/{id}/heartbeat", s.heartbeat)
	s.mux.HandleFunc("POST /internal/v1/runs/{id}/events", s.addEvents)
	s.mux.HandleFunc("POST /internal/v1/runs/{id}/complete", s.complete)
}
func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ScenarioID  string         `json:"scenario_id"`
		Environment string         `json:"environment"`
		Parameters  map[string]any `json:"parameters"`
		Target      domain.Target  `json:"target"`
	}
	if !decode(w, r, &in) {
		return
	}
	sc, cat, ok := catalog.Find(s.catalog, in.ScenarioID)
	if !ok {
		problem(w, 400, "unknown scenario")
		return
	}
	if in.Environment == "" {
		in.Environment = "local"
	}
	if in.Target.AuthType == "" {
		in.Target.AuthType = "none"
	}
	if err := validateTarget(in.Target); err != nil {
		problem(w, 400, err.Error())
		return
	}
	run := domain.Run{ID: newID(), ScenarioID: sc.ID, ScenarioDescription: sc.Description, CategoryID: cat.ID, CategoryDescription: cat.Description, Kind: sc.Kind, Environment: in.Environment, TargetAPIURL: in.Target.APIURL, TargetFrontendURL: in.Target.FrontendURL, AuthType: in.Target.AuthType, Parameters: in.Parameters, Status: "queued", CreatedAt: time.Now().UTC()}
	if run.Parameters == nil {
		run.Parameters = map[string]any{}
	}
	if err := s.store.CreateRun(r.Context(), run); err != nil {
		problem(w, 500, err.Error())
		return
	}
	if in.Target.AuthValue != "" {
		s.secrets.Store(run.ID, in.Target.AuthValue)
		time.AfterFunc(5*time.Minute, func() {
			if _, ok := s.secrets.LoadAndDelete(run.ID); ok {
				_ = s.store.Cancel(context.Background(), run.ID)
			}
		})
	}
	write(w, 201, run)
}
func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	runs, err := s.store.ListRuns(r.Context(), q.Get("category"), q.Get("scenario_id"), q.Get("status"))
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	if runs == nil {
		runs = []domain.Run{}
	}
	write(w, 200, map[string]any{"runs": runs})
}
func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		dbError(w, err)
		return
	}
	events, err := s.store.Events(r.Context(), run.ID, 0)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	write(w, 200, domain.RunDetail{Run: run, Events: events})
}
func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Cancel(r.Context(), r.PathValue("id")); err != nil {
		dbError(w, err)
		return
	}
	s.secrets.Delete(r.PathValue("id"))
	write(w, 202, map[string]bool{"cancel_requested": true})
}
func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID string   `json:"worker_id"`
		Kinds    []string `json:"kinds"`
	}
	if !decode(w, r, &in) {
		return
	}
	run, err := s.store.Claim(r.Context(), in.WorkerID, 30*time.Second, in.Kinds)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(204)
		return
	}
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	sc, _, _ := catalog.Find(s.catalog, run.ScenarioID)
	target := domain.Target{APIURL: run.TargetAPIURL, FrontendURL: run.TargetFrontendURL, AuthType: run.AuthType}
	if value, ok := s.secrets.LoadAndDelete(run.ID); ok {
		target.AuthValue, _ = value.(string)
	}
	write(w, 200, map[string]any{"run": run, "scenario": sc, "target": target})
}
func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID string `json:"worker_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	cancel, err := s.store.Heartbeat(r.Context(), r.PathValue("id"), in.WorkerID, 30*time.Second)
	if err != nil {
		dbError(w, err)
		return
	}
	write(w, 200, map[string]bool{"cancel_requested": cancel})
}
func (s *Server) addEvents(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Events []domain.Event `json:"events"`
	}
	if !decode(w, r, &in) {
		return
	}
	for _, e := range in.Events {
		e.RunID = r.PathValue("id")
		if _, err := s.store.AddEvent(r.Context(), e); err != nil {
			problem(w, 500, err.Error())
			return
		}
	}
	w.WriteHeader(204)
}
func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID string `json:"worker_id"`
		Status   string `json:"status"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.store.Complete(r.Context(), r.PathValue("id"), in.WorkerID, in.Status); err != nil {
		dbError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem(w, 500, "stream unsupported")
		return
	}
	after, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		events, err := s.store.Events(r.Context(), r.PathValue("id"), after)
		if err != nil {
			return
		}
		for _, e := range events {
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Sequence, b)
			after = e.Sequence
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]string{"error": msg})
}
func dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "not found")
	} else {
		problem(w, 500, err.Error())
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		problem(w, 400, "invalid request: "+err.Error())
		return false
	}
	return true
}
func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return fmt.Sprintf("%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}
func validateTarget(t domain.Target) error {
	if t.AuthType == "" {
		t.AuthType = "none"
	}
	if t.AuthType != "none" && t.AuthType != "bearer" && t.AuthType != "api-key" {
		return fmt.Errorf("unsupported auth_type")
	}
	if t.AuthType != "none" && strings.TrimSpace(t.AuthValue) == "" {
		return fmt.Errorf("auth_value is required")
	}
	for name, raw := range map[string]string{"api_url": t.APIURL, "frontend_url": t.FrontendURL} {
		if raw == "" && name == "frontend_url" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return fmt.Errorf("invalid %s", name)
		}
	}
	return nil
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Last-Event-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var _ = context.Background
var _ = strings.TrimSpace
