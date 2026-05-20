// Demo endpoints for the Phase 2 lifecycle messages. Same shape as the other
// demo handlers: sign with a named dev key + delegate to state.SubmitTx.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/aios/aios/internal/types"
)

// POST /demo/update-service
type demoUpdateServiceReq struct {
	ServiceID   uint64 `json:"service_id"`
	Description string `json:"description"`
	PriceAmount uint64 `json:"price_amount"`
	Key         string `json:"key"`
}

func (s *Server) demoUpdateService(w http.ResponseWriter, r *http.Request) {
	var req demoUpdateServiceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		req.Key = "bob"
	}
	if req.PriceAmount == 0 {
		req.PriceAmount = 100
	}
	_, err := s.signAndSubmit(req.Key, types.TxUpdateService, types.MsgUpdateService{
		Owner:       resolveAddr(s, req.Key),
		ServiceID:   req.ServiceID,
		Description: req.Description,
		Price:       types.Coin{Denom: "aios", Amount: req.PriceAmount},
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /demo/deactivate-service
type demoDeactivateServiceReq struct {
	ServiceID uint64 `json:"service_id"`
	Key       string `json:"key"`
}

func (s *Server) demoDeactivateService(w http.ResponseWriter, r *http.Request) {
	var req demoDeactivateServiceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		req.Key = "bob"
	}
	_, err := s.signAndSubmit(req.Key, types.TxDeactivateService, types.MsgDeactivateService{
		Owner:     resolveAddr(s, req.Key),
		ServiceID: req.ServiceID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /demo/deactivate-domain
type demoDeactivateDomainReq struct {
	DomainID uint64 `json:"domain_id"`
	Key      string `json:"key"`
}

func (s *Server) demoDeactivateDomain(w http.ResponseWriter, r *http.Request) {
	var req demoDeactivateDomainReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		req.Key = "bob"
	}
	_, err := s.signAndSubmit(req.Key, types.TxDeactivateDomain, types.MsgDeactivateDomain{
		Authority: resolveAddr(s, req.Key),
		DomainID:  req.DomainID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /demo/resolve-challenge
type demoResolveChallengeReq struct {
	RequestID uint64 `json:"request_id"`
	Decision  string `json:"decision"` // "dismiss" | "slash"
	Key       string `json:"key"`
}

func (s *Server) demoResolveChallenge(w http.ResponseWriter, r *http.Request) {
	var req demoResolveChallengeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		req.Key = "bob"
	}
	if req.Decision != "dismiss" && req.Decision != "slash" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decision must be 'dismiss' or 'slash'"))
		return
	}
	_, err := s.signAndSubmit(req.Key, types.TxResolveChallenge, types.MsgResolveChallenge{
		Authority: resolveAddr(s, req.Key),
		RequestID: req.RequestID,
		Decision:  req.Decision,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "decision": req.Decision})
}
