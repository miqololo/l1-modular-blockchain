// Package sub subscribes to the chain's /events SSE stream and updates the
// store as events arrive.
package sub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/aios/indexer/internal/store"
)

func Run(ctx context.Context, chainURL string, st *store.Store, logger *zap.Logger) {
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		if err := ctx.Err(); err != nil {
			return
		}
		err := subscribe(ctx, chainURL, st, logger)
		if err == nil || ctx.Err() != nil {
			return
		}
		logger.Warn("subscriber loop ended; reconnecting", zap.Error(err), zap.Duration("backoff", backoff))
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

func subscribe(ctx context.Context, chainURL string, st *store.Store, logger *zap.Logger) error {
	url := strings.TrimRight(chainURL, "/") + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subscribe status %d", resp.StatusCode)
	}
	logger.Info("subscribed to chain events", zap.String("url", url))

	reader := bufio.NewReader(resp.Body)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var ev struct {
			Type        string          `json:"type"`
			BlockHeight int64           `json:"block_height"`
			Payload     json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			logger.Warn("decode event", zap.Error(err))
			continue
		}

		switch ev.Type {
		case "ServiceRegistered":
			handleServiceRegistered(ev.Payload, ev.BlockHeight, st, logger)
		case "InferenceRequested":
			handleInferenceRequested(ev.Payload, ev.BlockHeight, st, logger)
		case "ResultSubmitted":
			handleResultSubmitted(ev.Payload, ev.BlockHeight, st, chainURL, logger)
		case "RequestFinalized":
			handleRequestFinalized(ev.Payload, ev.BlockHeight, st, chainURL, logger)
		case "RequestRefunded":
			handleRequestRefunded(ev.Payload, ev.BlockHeight, st, logger)
		case "BlockCommitted":
			// silently ignore — used as keepalive
		default:
			logger.Debug("unknown event", zap.String("type", ev.Type))
		}
	}
}

type serviceRegistered struct {
	ServiceID   uint64 `json:"service_id"`
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       struct {
		Denom  string `json:"denom"`
		Amount uint64 `json:"amount"`
	} `json:"price"`
}

func handleServiceRegistered(payload json.RawMessage, height int64, st *store.Store, logger *zap.Logger) {
	var p serviceRegistered
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warn("decode service registered", zap.Error(err))
		return
	}
	err := st.UpsertService(store.Service{
		ID: p.ServiceID, Owner: p.Owner, Name: p.Name, Description: p.Description,
		PriceDenom: p.Price.Denom, PriceAmount: p.Price.Amount,
		Active: true, CreatedAtHeight: height,
	})
	if err != nil {
		logger.Error("upsert service", zap.Error(err))
	}
}

type inferenceRequested struct {
	RequestID      uint64 `json:"request_id"`
	ServiceID      uint64 `json:"service_id"`
	Requester      string `json:"requester"`
	InputHash      string `json:"input_hash"`
	InputURI       string `json:"input_uri"`
	InputText      string `json:"input_text"`
	Escrow         struct {
		Denom  string `json:"denom"`
		Amount uint64 `json:"amount"`
	} `json:"escrow"`
	DeadlineHeight int64 `json:"deadline_height"`
}

func handleInferenceRequested(payload json.RawMessage, height int64, st *store.Store, logger *zap.Logger) {
	var p inferenceRequested
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warn("decode inference requested", zap.Error(err))
		return
	}
	err := st.UpsertRequest(store.Request{
		ID: p.RequestID, ServiceID: p.ServiceID, Requester: p.Requester,
		InputHash: p.InputHash, InputURI: p.InputURI, InputText: p.InputText,
		EscrowDenom: p.Escrow.Denom, EscrowAmount: p.Escrow.Amount,
		DeadlineHeight: p.DeadlineHeight, Status: "PENDING", CreatedAtHeight: height,
	})
	if err != nil {
		logger.Error("upsert request", zap.Error(err))
	}
}

func handleResultSubmitted(payload json.RawMessage, height int64, st *store.Store, chainURL string, logger *zap.Logger) {
	// We capture output details in the FinalizedHandler by querying the chain
	// for the full request (which contains the result). Lightweight version
	// here just logs.
	_ = payload
	_ = height
	_ = chainURL
	_ = logger
}

type requestFinalized struct {
	RequestID uint64 `json:"request_id"`
	Provider  string `json:"provider"`
	Paid      struct {
		Denom  string `json:"denom"`
		Amount uint64 `json:"amount"`
	} `json:"paid"`
}

func handleRequestFinalized(payload json.RawMessage, height int64, st *store.Store, chainURL string, logger *zap.Logger) {
	var p requestFinalized
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warn("decode finalized", zap.Error(err))
		return
	}

	// Fetch the full request to get the output_hash + output_text.
	url := strings.TrimRight(chainURL, "/") + fmt.Sprintf("/requests/%d", p.RequestID)
	resp, err := http.Get(url)
	if err != nil {
		logger.Warn("fetch finalized request", zap.Error(err))
		// Still mark as finalized, even without output details.
		_ = st.FinalizeRequest(p.RequestID, p.Provider, "", "", "", p.Paid.Denom, p.Paid.Amount, height)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("fetch finalized request bad status", zap.Int("status", resp.StatusCode))
		_ = st.FinalizeRequest(p.RequestID, p.Provider, "", "", "", p.Paid.Denom, p.Paid.Amount, height)
		return
	}

	var full struct {
		Result *struct {
			OutputHash string `json:"output_hash"`
			OutputURI  string `json:"output_uri"`
			OutputText string `json:"output_text"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&full); err != nil {
		logger.Warn("decode finalized full request", zap.Error(err))
	}

	outputHash, outputURI, outputText := "", "", ""
	if full.Result != nil {
		outputHash = full.Result.OutputHash
		outputURI = full.Result.OutputURI
		outputText = full.Result.OutputText
	}
	if err := st.FinalizeRequest(p.RequestID, p.Provider, outputHash, outputURI, outputText, p.Paid.Denom, p.Paid.Amount, height); err != nil {
		logger.Error("finalize", zap.Error(err))
	}
}

type requestRefunded struct {
	RequestID uint64 `json:"request_id"`
}

func handleRequestRefunded(payload json.RawMessage, height int64, st *store.Store, logger *zap.Logger) {
	var p requestRefunded
	if err := json.Unmarshal(payload, &p); err != nil {
		logger.Warn("decode refunded", zap.Error(err))
		return
	}
	if err := st.RefundRequest(p.RequestID, height); err != nil {
		logger.Error("refund", zap.Error(err))
	}
}
