// Package llama_http calls a llama-server (llama.cpp) sidecar over HTTP and
// returns the model output for an inference request.
//
// REAL INFERENCE, UNVERIFIED. This executor runs a real LLM but produces results
// outside any verification domain: single machine, no determinism guarantees,
// no fraud-proof challenge. Phase 1 introduces determinism pinning; Phase 3
// introduces challenge mechanisms. Do NOT treat outputs as cryptographically
// verifiable until both land.
package llama_http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Request holds the inputs to one inference call.
type Request struct {
	Prompt string
}

// Result is what the executor returns to the caller.
type Result struct {
	Output     string
	OutputHash string
	OutputURI  string
}

// Executor is the llama-server HTTP client.
type Executor struct {
	serverURL   string
	hardwareTag string
	client      *http.Client
	logger      *zap.Logger
}

// NewExecutor constructs an Executor.
func NewExecutor(serverURL, hardwareTag string, logger *zap.Logger) (*Executor, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("llama-server URL required")
	}
	return &Executor{
		serverURL:   strings.TrimRight(serverURL, "/"),
		hardwareTag: hardwareTag,
		client:      &http.Client{Timeout: 90 * time.Second},
		logger:      logger,
	}, nil
}

func (e *Executor) HardwareTag() string { return e.hardwareTag }

// WaitReady blocks until /health responds OK or timeout.
func (e *Executor) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.serverURL+"/health", nil)
		resp, err := e.client.Do(req)
		cancel()
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			e.logger.Info("llama-server ready", zap.String("url", e.serverURL))
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		e.logger.Info("waiting for llama-server", zap.String("url", e.serverURL))
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("llama-server not ready at %s", e.serverURL)
}

// completionRequest mirrors the llama-server /completion API.
type completionRequest struct {
	Prompt      string  `json:"prompt"`
	NPredict    int     `json:"n_predict"`
	Temperature float64 `json:"temperature"`
	TopK        int     `json:"top_k"`
	TopP        float64 `json:"top_p"`
	Stream      bool    `json:"stream"`
}

type completionResponse struct {
	Content string `json:"content"`
}

// Execute calls /completion and hashes the output.
func (e *Executor) Execute(ctx context.Context, req Request) (Result, error) {
	body, err := json.Marshal(completionRequest{
		Prompt:      req.Prompt,
		NPredict:    256,
		Temperature: 0,
		TopK:        1,
		TopP:        1,
		Stream:      false,
	})
	if err != nil {
		return Result{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.serverURL+"/completion", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("call llama-server: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("llama-server status %d: %s", resp.StatusCode, string(respBody))
	}
	var out completionResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return Result{}, fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
	}
	if out.Content == "" {
		return Result{}, fmt.Errorf("empty content")
	}
	h := sha256.Sum256([]byte(out.Content))
	outputHash := hex.EncodeToString(h[:])
	return Result{
		Output:     out.Content,
		OutputHash: outputHash,
		OutputURI:  "inline:" + outputHash,
	}, nil
}
