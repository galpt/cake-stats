package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ─────────────────────────── Server ────────────────────────────────────────

// Server holds the HTTP mux, the latest parsed stats snapshot, an SSE
// client registry, and the historical time-series store.
// All fields that cross goroutine boundaries are protected.
type Server struct {
	mux     *http.ServeMux
	httpSrv *http.Server

	// Latest snapshot — guarded by mu
	mu    sync.RWMutex
	stats []CakeStats

	// SSE client registry — guarded by ssesMu
	ssesMu  sync.Mutex
	clients map[chan []byte]struct{}

	pollInterval time.Duration

	// Historical ring-buffer store — safe for concurrent reads via Snapshot().
	history *HistoryStore
}

// NewServer constructs a Server that will serve on addr and poll tc every
// interval.  histCap controls how many samples per interface the history
// ring buffer retains (e.g. 300 = 5 min at 1 s interval).
func NewServer(addr string, interval time.Duration, histCap int) *Server {
	s := &Server{
		mux:          http.NewServeMux(),
		clients:      make(map[chan []byte]struct{}),
		pollInterval: interval,
		history:      NewHistoryStore(histCap),
	}

	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/stats", s.handleAPIStats)
	s.mux.HandleFunc("/api/history", s.handleAPIHistory)
	s.mux.HandleFunc("/events", s.handleSSE)

	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // SSE connections must not time out on writes
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Run starts the tc poller and the HTTP server.  It blocks until ctx is
// cancelled, at which point it performs a graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	// Perform an initial poll before accepting connections so the first
	// page load already has data.
	s.poll()

	go s.runPoller(ctx)

	// Shut down gracefully when ctx is done
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpSrv.Shutdown(shutCtx); err != nil {
			log.Printf("[WARN] HTTP shutdown: %v", err)
		}
	}()

	log.Printf("[INFO] Listening on %s", s.httpSrv.Addr)
	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http listen: %w", err)
	}
	return nil
}

// ─────────────────────────── Poller ────────────────────────────────────────

func (s *Server) runPoller(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.poll()
		}
	}
}

// poll executes tc, parses its output, updates the snapshot, and pushes an
// SSE event to all connected clients.
func (s *Server) poll() {
	raw, err := runTC()
	if err != nil {
		log.Printf("[WARN] tc: %v", err)
		return
	}

	parsed := ParseTCOutput(raw)

	// Record computes delta fields (TxBytesPerS, etc.) in-place and appends
	// to the ring buffers — must happen before we store/broadcast.
	s.history.Record(parsed, s.pollInterval)

	s.mu.Lock()
	s.stats = parsed
	s.mu.Unlock()

	s.broadcast(parsed)
}

// ─────────────────────────── SSE ────────────────────────────────────────────

// broadcast encodes stats to JSON and sends them to every SSE client.
func (s *Server) broadcast(stats []CakeStats) {
	payload, err := json.Marshal(map[string]interface{}{
		"interfaces": stats,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		log.Printf("[WARN] marshal: %v", err)
		return
	}

	event := buildSSEEvent(payload)

	s.ssesMu.Lock()
	defer s.ssesMu.Unlock()
	for ch := range s.clients {
		// Non-blocking send: if the channel is full the client is slow and we
		// simply drop that single frame rather than block the poller.
		select {
		case ch <- event:
		default:
		}
	}
}

// buildSSEEvent wraps a JSON payload in a proper SSE message frame.
func buildSSEEvent(payload []byte) []byte {
	return []byte(fmt.Sprintf("retry: 2000\ndata: %s\n\n", payload))
}

// handleSSE is the /events endpoint.  Each connected browser client receives
// a continuous stream of newline-delimited SSE frames.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Ensure the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	// Register this client
	ch := make(chan []byte, 4)
	s.ssesMu.Lock()
	s.clients[ch] = struct{}{}
	s.ssesMu.Unlock()

	defer func() {
		s.ssesMu.Lock()
		delete(s.clients, ch)
		s.ssesMu.Unlock()
	}()

	// Send the current snapshot immediately upon connection
	s.mu.RLock()
	snapshot := s.stats
	s.mu.RUnlock()
	if len(snapshot) > 0 {
		payload, err := json.Marshal(map[string]interface{}{
			"interfaces": snapshot,
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		})
		if err == nil {
			_, _ = w.Write(buildSSEEvent(payload))
			flusher.Flush()
		}
	}

	// Keep the connection open and stream updates
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(event); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ─────────────────────────── HTTP handlers ──────────────────────────────────

// handleIndex serves the embedded HTML application.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(indexHTML))
}

// handleAPIStats returns a JSON snapshot of the current CAKE stats.
// Useful for curl / scripted polling outside the browser.
func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	snapshot := s.stats
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]interface{}{
		"interfaces": snapshot,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// handleAPIHistory returns the full ring-buffer history for all interfaces
// as a JSON object keyed by interface name.  Used by the web UI on first load
// to seed client-side sparklines and charts.
func (s *Server) handleAPIHistory(w http.ResponseWriter, r *http.Request) {
	snap := s.history.Snapshot()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(snap)
}
