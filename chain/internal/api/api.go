// Package api exposes the chain's HTTP surface:
//   - POST /tx                — submit a signed transaction
//   - GET  /services          — list services
//   - GET  /services/{id}     — single service
//   - GET  /requests          — list requests (optional filter)
//   - GET  /requests/{id}     — single request
//   - GET  /accounts/{addr}   — balance + next nonce
//   - GET  /params            — chain params
//   - GET  /status            — { chain_id, height, time }
//   - GET  /events            — SSE stream of typed events
//   - GET  /health            — { status: "ok" }
//
// Phase 1 replaces this with CometBFT RPC + Cosmos SDK gRPC; the
// JSON shapes here are deliberately compatible with the proto definitions in
// /proto/aiservice/v1/.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/aios/aios/internal/state"
	"github.com/aios/aios/internal/types"
)

type Server struct {
	state  *state.State
	logger *zap.Logger
	mux    *http.ServeMux
}

func NewServer(s *state.State, logger *zap.Logger) *Server {
	srv := &Server{state: s, logger: logger, mux: http.NewServeMux()}
	srv.routes()
	return srv
}

func (s *Server) Handler() http.Handler {
	return withCORS(withLogging(s.mux, s.logger))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("GET /status", s.status)
	s.mux.HandleFunc("GET /params", s.params)
	s.mux.HandleFunc("POST /tx", s.submitTx)
	s.mux.HandleFunc("GET /services", s.listServices)
	s.mux.HandleFunc("GET /services/{id}", s.getService)
	s.mux.HandleFunc("GET /requests", s.listRequests)
	s.mux.HandleFunc("GET /requests/{id}", s.getRequest)
	s.mux.HandleFunc("GET /accounts/{addr}", s.getAccount)
	s.mux.HandleFunc("GET /events", s.events)

	// Phase 1: verification domain registry.
	s.mux.HandleFunc("GET /domains", s.listDomains)
	s.mux.HandleFunc("GET /domains/{id}", s.getDomain)
	s.mux.HandleFunc("GET /authority", s.getAuthority)

	// Demo-only: signs txs server-side using the embedded dev keyring.
	// Production chains never sign on behalf of users.
	s.mux.HandleFunc("POST /demo/seed", s.demoSeed)
	s.mux.HandleFunc("POST /demo/register-service", s.demoRegisterService)
	s.mux.HandleFunc("POST /demo/request-inference", s.demoRequestInference)
	s.mux.HandleFunc("POST /demo/register-domain", s.demoRegisterDomain)
	s.mux.HandleFunc("POST /demo/challenge", s.demoChallenge)
	// Phase 2 catch-up: lifecycle msgs.
	s.mux.HandleFunc("POST /demo/update-service", s.demoUpdateService)
	s.mux.HandleFunc("POST /demo/deactivate-service", s.demoDeactivateService)
	s.mux.HandleFunc("POST /demo/deactivate-domain", s.demoDeactivateDomain)
	s.mux.HandleFunc("POST /demo/resolve-challenge", s.demoResolveChallenge)
}

// ─── handlers ──────────────────────────────────────────────────────────────

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"chain_id": s.state.ChainID(),
		"height":   s.state.Height(),
		"time":     time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *Server) params(w http.ResponseWriter, _ *http.Request) {
	p, err := s.state.Params()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) submitTx(w http.ResponseWriter, r *http.Request) {
	var tx types.SignedTx
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode: %w", err))
		return
	}
	receipt, err := s.state.SubmitTx(tx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resp := map[string]any{
		"type": receipt.Type,
		"height": s.state.Height(),
	}
	if receipt.NewID != 0 {
		switch receipt.Type {
		case types.TxRegisterService:
			resp["service_id"] = receipt.NewID
		case types.TxRequestInference:
			resp["request_id"] = receipt.NewID
		}
	}
	if receipt.Type == types.TxSubmitResult {
		resp["finalized"] = receipt.Finalized
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) listServices(w http.ResponseWriter, _ *http.Request) {
	out, err := s.state.ListServices()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if out == nil {
		out = []types.Service{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	svc, err := s.state.GetService(id)
	if err != nil {
		if errors.Is(err, types.ErrServiceNotFound) {
			writeError(w, http.StatusNotFound, err)
		} else {
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

func (s *Server) listRequests(w http.ResponseWriter, r *http.Request) {
	st := types.RequestStatus(r.URL.Query().Get("status"))
	svcID, _ := strconv.ParseUint(r.URL.Query().Get("service_id"), 10, 64)
	out, err := s.state.ListRequests(st, svcID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if out == nil {
		out = []types.InferenceRequest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) getRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req, err := s.state.GetRequest(id)
	if err != nil {
		if errors.Is(err, types.ErrRequestNotFound) {
			writeError(w, http.StatusNotFound, err)
		} else {
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	addr := r.PathValue("addr")
	if !types.IsValidAddress(addr) {
		writeError(w, http.StatusBadRequest, types.ErrInvalidAddress)
		return
	}
	acc, err := s.state.Account(addr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"address": acc.Address,
		"balance": acc.Balance,
		"nonce":   s.state.AccountNonce(addr),
	})
}

func (s *Server) listDomains(w http.ResponseWriter, _ *http.Request) {
	out, err := s.state.ListDomains()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if out == nil {
		out = []types.VerificationDomain{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) getDomain(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	d, err := s.state.GetDomain(id)
	if err != nil {
		if errors.Is(err, types.ErrDomainNotFound) {
			writeError(w, http.StatusNotFound, err)
		} else {
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) getAuthority(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authority": s.state.Authority()})
}

// events streams all chain events as SSE.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.state.Subscribe()
	defer cancel()

	// Optional event-type filter via query param.
	filter := strings.TrimSpace(r.URL.Query().Get("types"))
	wanted := map[string]struct{}{}
	if filter != "" {
		for _, t := range strings.Split(filter, ",") {
			wanted[strings.TrimSpace(t)] = struct{}{}
		}
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if len(wanted) > 0 {
				if _, want := wanted[string(ev.Type)]; !want {
					continue
				}
			}
			bz, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, string(bz))
			flusher.Flush()
		}
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func withLogging(h http.Handler, logger *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip noisy paths
		if r.URL.Path != "/health" && r.URL.Path != "/events" {
			logger.Debug("http", zap.String("method", r.Method), zap.String("path", r.URL.Path))
		}
		h.ServeHTTP(w, r)
	})
}
