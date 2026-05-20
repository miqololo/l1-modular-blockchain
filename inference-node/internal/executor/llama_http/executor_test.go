package llama_http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func fakeLlamaServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		require.Equal(t, "/completion", r.URL.Path)
		var body completionRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, 0.0, body.Temperature)
		require.Equal(t, 1, body.TopK)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(completionResponse{Content: response})
	}))
}

func TestExecute_HappyPath(t *testing.T) {
	srv := fakeLlamaServer(t, "bonjour")
	defer srv.Close()

	exec, err := NewExecutor(srv.URL, "test-hw", zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, exec.WaitReady(5*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := exec.Execute(ctx, Request{Prompt: "hello"})
	require.NoError(t, err)
	require.Equal(t, "bonjour", r.Output)
	h := sha256.Sum256([]byte("bonjour"))
	require.Equal(t, hex.EncodeToString(h[:]), r.OutputHash)
}

func TestExecute_SameInputSameOutput(t *testing.T) {
	srv := fakeLlamaServer(t, "deterministic")
	defer srv.Close()

	exec, err := NewExecutor(srv.URL, "test-hw", zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, exec.WaitReady(5*time.Second))

	r1, err := exec.Execute(context.Background(), Request{Prompt: "p"})
	require.NoError(t, err)
	r2, err := exec.Execute(context.Background(), Request{Prompt: "p"})
	require.NoError(t, err)
	require.Equal(t, r1.OutputHash, r2.OutputHash)
}
