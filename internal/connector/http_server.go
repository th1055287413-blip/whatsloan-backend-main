package connector

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"whatsapp_golang/internal/config"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// HTTPServer 提供 /health 和 /metrics 端點
type HTTPServer struct {
	server *http.Server
	pool   *Pool
	log    *zap.SugaredLogger
}

// NewHTTPServer 建立 HTTP 伺服器
func NewHTTPServer(addr string, pool *Pool, log *zap.SugaredLogger) *HTTPServer {
	s := &HTTPServer{pool: pool, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.healthHandler)
	mux.Handle("/metrics", promhttp.Handler())

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return s
}

// Start 啟動 HTTP 伺服器（非阻塞）
func (s *HTTPServer) Start() error {
	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return err
	}
	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Errorw("HTTP 伺服器異常", "error", err)
		}
	}()
	s.log.Infow("HTTP 伺服器已啟動", "addr", s.server.Addr)
	return nil
}

// Stop 停止 HTTP 伺服器
func (s *HTTPServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		s.log.Warnw("HTTP 伺服器關閉失敗", "error", err)
	}
}

func (s *HTTPServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"status":  "ok",
		"version": config.GetConnectorVersion(),
	}
	if s.pool != nil {
		resp["connectors"] = len(s.pool.GetActiveConnectorIDs())
		resp["accounts"] = s.pool.GetTotalAccountCount()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
