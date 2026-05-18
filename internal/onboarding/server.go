// Package onboarding serves the JSBridge onboarding page, issues per-device
// MQTT credentials to the RC Plus (§5), and exposes ingested telemetry. Every
// endpoint is gated behind an API key when one is configured.
package onboarding

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/searchandrescuegg/rescuestream-dji-bridge/internal/device"
	"github.com/searchandrescuegg/rescuestream-dji-bridge/web"
)

const (
	maxOnboardBody       = 4 << 10 // 4 KiB request body cap for /api/onboard
	maxConcurrentStreams = 64      // cap on simultaneous SSE clients
	sseWriteTimeout      = 10 * time.Second
)

// PageConfig holds the DJI JSBridge values injected into the onboarding page.
type PageConfig struct {
	AppID        string
	AppKey       string
	License      string
	PlatformName string
	WorkspaceID  string
}

// Server serves the JSBridge onboarding page, the credential-issuing API, and
// the telemetry API. Construct it with New plus With* options.
type Server struct {
	addr       string
	minter     *Minter
	registry   *device.Registry
	store      *device.Store
	publicHost string
	page       PageConfig
	apiKey     string
	allowlist  map[string]struct{}
	logger     *slog.Logger

	tmpl          *template.Template
	http          *http.Server
	streamCtx     context.Context
	cancelStream  context.CancelFunc
	activeStreams atomic.Int64
}

// Option configures a Server; see the With* constructors.
type Option func(*Server)

// WithMinter sets the JWT credential minter; without it onboarding is disabled.
func WithMinter(m *Minter) Option { return func(s *Server) { s.minter = m } }

// WithRegistry sets the device registry that records onboarded pairings.
func WithRegistry(r *device.Registry) Option { return func(s *Server) { s.registry = r } }

// WithDeviceStore sets the telemetry store backing the /api/devices endpoints.
func WithDeviceStore(store *device.Store) Option { return func(s *Server) { s.store = store } }

// WithPublicMQTTHost sets the broker host returned to onboarding devices.
func WithPublicMQTTHost(host string) Option { return func(s *Server) { s.publicHost = host } }

// WithPageConfig sets the DJI values injected into the onboarding page.
func WithPageConfig(p PageConfig) Option { return func(s *Server) { s.page = p } }

// WithAPIKey sets the API key required on every endpoint. When empty the
// server runs open (a startup warning should be logged by the caller).
func WithAPIKey(key string) Option { return func(s *Server) { s.apiKey = key } }

// WithSNAllowlist restricts which RC Plus serial numbers may be onboarded.
// An empty list allows any serial number.
func WithSNAllowlist(sns []string) Option {
	return func(s *Server) {
		s.allowlist = make(map[string]struct{}, len(sns))
		for _, sn := range sns {
			if sn = strings.TrimSpace(sn); sn != "" {
				s.allowlist[sn] = struct{}{}
			}
		}
	}
}

// WithLogger sets the structured logger (defaults to slog.Default()).
func WithLogger(l *slog.Logger) Option { return func(s *Server) { s.logger = l } }

// New creates an onboarding server listening on addr (e.g. ":8080").
func New(addr string, opts ...Option) (*Server, error) {
	streamCtx, cancelStream := context.WithCancel(context.Background())
	s := &Server{
		addr:         addr,
		logger:       slog.Default(),
		streamCtx:    streamCtx,
		cancelStream: cancelStream,
	}
	for _, opt := range opts {
		opt(s)
	}

	tmpl, err := template.New("onboarding").Parse(web.OnboardingPage)
	if err != nil {
		cancelStream()
		return nil, fmt.Errorf("parse onboarding page: %w", err)
	}
	s.tmpl = tmpl

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.requireAuth(s.handlePage))
	mux.HandleFunc("POST /api/onboard", s.requireAuth(s.handleOnboard))
	mux.HandleFunc("GET /api/devices", s.requireAuth(s.handleDevices))
	mux.HandleFunc("GET /api/devices/stream", s.requireAuth(s.handleDevicesStream))
	// The debug library is loaded via a <script> tag, which cannot carry the
	// API key, so it is intentionally not gated.
	mux.HandleFunc("GET /vconsole.min.js", s.handleVConsole)
	s.http = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: it would abort long-lived SSE streams. The SSE
		// handler sets a per-write deadline instead.
	}
	return s, nil
}

// Start runs the HTTP server until ctx is cancelled, then shuts it down.
func (s *Server) Start(ctx context.Context) error {
	defer s.cancelStream() // ends any active SSE streams on every exit path

	errc := make(chan error, 1)
	go func() {
		s.logger.Info("onboarding server listening", slog.String("addr", s.addr))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	case err := <-errc:
		return err
	}
}

// requireAuth wraps h with API-key authentication and baseline security
// headers. When no API key is configured the check passes (open mode).
func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !s.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// authorized reports whether r carries the configured API key, accepted via
// the X-API-Key header, an Authorization: Bearer header, or a ?key= query
// parameter (the last lets the WebView page be opened with a key in its URL).
func (s *Server) authorized(r *http.Request) bool {
	if s.apiKey == "" {
		return true
	}
	provided := r.Header.Get("X-API-Key")
	if provided == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			provided = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if provided == "" {
		provided = r.URL.Query().Get("key")
	}
	return provided != "" &&
		subtle.ConstantTimeCompare([]byte(provided), []byte(s.apiKey)) == 1
}

// snAllowed reports whether sn may be onboarded. An empty allowlist allows all.
func (s *Server) snAllowed(sn string) bool {
	if len(s.allowlist) == 0 {
		return true
	}
	_, ok := s.allowlist[sn]
	return ok
}

func (s *Server) handlePage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The onboarding page must never be cached — Pilot 2's WebView otherwise
	// serves a stale copy after the bridge is updated.
	w.Header().Set("Cache-Control", "no-store")
	if err := s.tmpl.Execute(w, s.page); err != nil {
		s.logger.Error("render onboarding page", slog.String("error", err.Error()))
	}
}

// handleVConsole serves the bundled vConsole debug library.
func (s *Server) handleVConsole(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(web.VConsoleJS)
}

type onboardRequest struct {
	RCSN       string `json:"rc_sn"`
	AircraftSN string `json:"aircraft_sn"`
}

type onboardResponse struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleOnboard(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOnboardBody)
	var req onboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.RCSN == "" {
		http.Error(w, "rc_sn is required", http.StatusBadRequest)
		return
	}
	if !s.snAllowed(req.RCSN) {
		s.logger.Warn("rejected onboard for non-allowlisted serial", slog.String("rc_sn", req.RCSN))
		http.Error(w, "serial number not authorized for onboarding", http.StatusForbidden)
		return
	}
	if s.minter == nil {
		http.Error(w, "credential issuance not configured", http.StatusServiceUnavailable)
		return
	}

	token, err := s.minter.Mint(req.RCSN)
	if err != nil {
		s.logger.Error("mint credentials", slog.String("error", err.Error()))
		http.Error(w, "could not issue credentials", http.StatusInternalServerError)
		return
	}
	if s.registry != nil && req.AircraftSN != "" {
		s.registry.Link(req.RCSN, req.AircraftSN)
	}

	s.logger.Info("onboarded rc plus",
		slog.String("rc_sn", req.RCSN),
		slog.String("aircraft_sn", req.AircraftSN))
	writeJSON(w, http.StatusOK, onboardResponse{
		Host:     s.publicHost,
		Username: req.RCSN,
		Password: token,
	})
}

// handleDevices returns the latest telemetry snapshot for every device.
func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusOK, []device.Snapshot{})
		return
	}
	writeJSON(w, http.StatusOK, s.store.List())
}

// handleDevicesStream streams telemetry updates as Server-Sent Events.
func (s *Server) handleDevicesStream(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "telemetry store not configured", http.StatusServiceUnavailable)
		return
	}
	if s.activeStreams.Add(1) > maxConcurrentStreams {
		s.activeStreams.Add(-1)
		http.Error(w, "too many concurrent streams", http.StatusServiceUnavailable)
		return
	}
	defer s.activeStreams.Add(-1)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	rc := http.NewResponseController(w)

	updates, cancel := s.store.Subscribe()
	defer cancel()

	// Send the current state of every device before streaming live updates.
	for _, snap := range s.store.List() {
		if !s.writeSSE(rc, w, snap) {
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.streamCtx.Done():
			return
		case snap := <-updates:
			if !s.writeSSE(rc, w, snap) {
				return
			}
		}
	}
}

// writeSSE writes one SSE event under a write deadline so a dead client cannot
// wedge the handler goroutine. It returns false when the stream should close.
func (s *Server) writeSSE(rc *http.ResponseController, w http.ResponseWriter, v any) bool {
	data, err := json.Marshal(v)
	if err != nil {
		s.logger.Error("marshal sse event", slog.String("error", err.Error()))
		return true // skip this event, keep the stream open
	}
	_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		s.logger.Debug("sse write failed, closing stream", slog.String("error", err.Error()))
		return false
	}
	if err := rc.Flush(); err != nil {
		s.logger.Debug("sse flush failed, closing stream", slog.String("error", err.Error()))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
