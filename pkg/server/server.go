package server

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"sync"
	"time"

	fiber "github.com/gofiber/fiber/v3"
	recovermiddleware "github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/galpt/cake-stats/pkg/history"
	"github.com/galpt/cake-stats/pkg/log"
	"github.com/galpt/cake-stats/pkg/parser"
	"github.com/galpt/cake-stats/pkg/types"
)

//go:embed index.html
var indexHTML string

const sseBufSize = 4

// Server encapsulates the Fiber app, polling state, SSE client registry and
// history store.  It is safe for concurrent use.
type Server struct {
	app          *fiber.App
	statsMu      sync.RWMutex
	stats        []types.CakeStats
	ssesMu       sync.Mutex
	clients      map[chan []byte]struct{}
	pollInterval time.Duration
	history      *history.HistoryStore
	stopOnce     sync.Once
}

func New(addr string, interval time.Duration, histCap int) *Server {
	s := &Server{
		clients:      make(map[chan []byte]struct{}),
		pollInterval: interval,
		history:      history.NewHistoryStore(histCap),
	}

	app := fiber.New(fiber.Config{
		ServerHeader: "cake-stats",
	})
	app.Use(recovermiddleware.New())

	app.Get("/", s.handleIndex)
	app.Get("/api/stats", s.handleAPIStats)
	app.Get("/api/history", s.handleAPIHistory)
	app.Get("/events", s.handleSSE)

	s.app = app
	return s
}

func (s *Server) Run(ctx context.Context, addr string) error {
	s.forcePoll()
	go s.runPoller(ctx)
	go func() {
		<-ctx.Done()
		_ = s.app.Shutdown()
	}()
	log.Logger.Info().Str("addr", addr).Dur("interval", s.pollInterval).Msg("listening")
	return s.app.Listen(addr)
}

func (s *Server) forcePoll() {
	defer func() {
		if r := recover(); r != nil {
			log.Logger.Error().Interface("panic", r).Msg("poller recovered")
		}
	}()
	stats, err := parser.CollectStats(context.Background())
	if err != nil {
		log.Logger.Warn().Err(err).Msg("tc poll failed")
		return
	}
	s.history.Record(stats, s.pollInterval)
	s.statsMu.Lock()
	s.stats = stats
	s.statsMu.Unlock()
	s.broadcast(stats)
}

func (s *Server) runPoller(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.forcePoll()
		}
	}
}

func (s *Server) broadcast(stats []types.CakeStats) {
	resp := types.StatsResponse{Interfaces: stats, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	payload, _ := resp.MarshalJSON()
	event := buildSSEEvent(payload)

	s.ssesMu.Lock()
	defer s.ssesMu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- event:
		default:
		}
	}
}

var sseBufPool = sync.Pool{New: func() any { b := make([]byte, 0, 1024); return &b }}

func buildSSEEvent(payload []byte) []byte {
	buf := sseBufPool.Get().(*[]byte)
	*buf = (*buf)[:0]
	*buf = append(*buf, "retry: 2000\ndata: "...)
	*buf = append(*buf, payload...)
	*buf = append(*buf, "\n\n"...)
	out := make([]byte, len(*buf))
	copy(out, *buf)
	sseBufPool.Put(buf)
	return out
}

func (s *Server) handleIndex(c fiber.Ctx) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	c.Set("Cache-Control", "no-store")
	return c.SendString(indexHTML)
}

func (s *Server) handleAPIStats(c fiber.Ctx) error {
	s.statsMu.RLock()
	snapshot := s.stats
	s.statsMu.RUnlock()
	resp := types.StatsResponse{Interfaces: snapshot, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	c.Set("Content-Type", "application/json; charset=utf-8")
	b, _ := resp.MarshalJSON()
	return c.Send(b)
}

func (s *Server) handleAPIHistory(c fiber.Ctx) error {
	snap := s.history.Snapshot()
	c.Set("Content-Type", "application/json; charset=utf-8")
	b, _ := json.Marshal(snap)
	return c.Send(b)
}

func (s *Server) handleSSE(c fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	ch := make(chan []byte, sseBufSize)

	s.ssesMu.Lock()
	s.clients[ch] = struct{}{}
	s.ssesMu.Unlock()

	// Capture initial snapshot before entering the stream writer.
	s.statsMu.RLock()
	snapshot := s.stats
	s.statsMu.RUnlock()

	c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer func() {
			s.ssesMu.Lock()
			delete(s.clients, ch)
			s.ssesMu.Unlock()
		}()

		// Send the current snapshot immediately so the page isn't blank.
		if len(snapshot) > 0 {
			resp := types.StatsResponse{
				Interfaces: snapshot,
				UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
			}
			if payload, err := resp.MarshalJSON(); err == nil {
				if _, err = w.Write(buildSSEEvent(payload)); err != nil {
					return
				}
				_ = w.Flush()
			}
		}

		for event := range ch {
			if _, err := w.Write(event); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	})
	return nil
}
