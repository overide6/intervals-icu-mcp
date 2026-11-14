package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xalexb/intervals-icu-mcp/src/app/clients/intervals"
)

func callUpdateWellness(t *testing.T, apiServer *httptest.Server, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	c := intervals.NewTestClient(apiServer.URL, "test-key", "i12345", apiServer.Client())

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "v0.0.1"}, nil)
	NewUpdateWellnessTool(c)(mcpServer)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()

	serverCtx, serverCancel := context.WithCancel(ctx)
	defer serverCancel()

	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)

		_ = mcpServer.Run(serverCtx, serverTransport)
	}()

	t.Cleanup(func() {
		serverCancel()
		<-serverDone
	})

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)

	connectCtx, connectCancel := context.WithTimeout(ctx, 5*time.Second)
	defer connectCancel()

	session, err := mcpClient.Connect(connectCtx, clientTransport, nil)
	if err != nil {
		t.Fatalf("expected client to connect, got: %v", err)
	}

	t.Cleanup(func() { _ = session.Close() })

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	defer callCancel()

	result, err := session.CallTool(callCtx, &mcp.CallToolParams{Name: "update_wellness", Arguments: args})
	if err != nil {
		t.Fatalf("expected CallTool to succeed (errors are returned in result), got: %v", err)
	}

	return result
}

func TestNewUpdateWellnessTool_Success(t *testing.T) {
	t.Parallel()

	const responseJSON = `{"id":"2026-11-14","fatigue":2,"mood":3}`

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got: %s", r.Method)
		}

		if r.URL.Path != "/api/v1/athlete/i12345/wellness/2026-11-14" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		reqBody, _ := io.ReadAll(r.Body)

		var parsed map[string]any

		err := json.Unmarshal(reqBody, &parsed)
		if err != nil {
			t.Fatalf("expected valid JSON body, got error: %v", err)
		}

		if len(parsed) != 2 || parsed["fatigue"] != float64(2) || parsed["mood"] != float64(3) {
			t.Errorf("expected body with only fatigue=2 and mood=3, got: %s", reqBody)
		}

		if _, hasDate := parsed["date"]; hasDate {
			t.Errorf("date must not be in the body, got: %s", reqBody)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseJSON))
	}))
	defer apiServer.Close()

	result := callUpdateWellness(t, apiServer, map[string]any{"date": "2026-11-14", "fatigue": 2, "mood": 3})

	if result.IsError {
		t.Fatalf("expected success, got error result: %v", result.Content)
	}

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got: %d", len(result.Content))
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok || textContent.Text != responseJSON {
		t.Fatalf("expected text %q, got: %v", responseJSON, result.Content[0])
	}
}

func TestNewUpdateWellnessTool_AllFields(t *testing.T) {
	t.Parallel()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, _ := io.ReadAll(r.Body)

		var parsed map[string]any

		err := json.Unmarshal(reqBody, &parsed)
		if err != nil {
			t.Fatalf("expected valid JSON body, got error: %v", err)
		}

		const wantFields = 8
		if len(parsed) != wantFields || parsed["comments"] != "ok" || parsed["locked"] != true {
			t.Errorf("unexpected body: %s", reqBody)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer apiServer.Close()

	result := callUpdateWellness(t, apiServer, map[string]any{
		"date": "2026-11-14", "soreness": 1, "fatigue": 2, "stress": 3, "mood": 4,
		"motivation": 1, "injury": 1, "comments": "ok", "locked": true,
	})

	if result.IsError {
		t.Fatalf("expected success, got error result: %v", result.Content)
	}
}

func TestNewUpdateWellnessTool_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
	}{
		{"missing date", map[string]any{"fatigue": 2}},
		{"invalid date format", map[string]any{"date": "14-11-2026", "fatigue": 2}},
		{"no field set", map[string]any{"date": "2026-11-14"}},
		{"value zero", map[string]any{"date": "2026-11-14", "mood": 0}},
		{"value five", map[string]any{"date": "2026-11-14", "stress": 5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("API should not be called for validation errors")
			}))
			defer apiServer.Close()

			result := callUpdateWellness(t, apiServer, tt.args)

			if !result.IsError {
				t.Fatalf("expected IsError to be true, got: %v", result.IsError)
			}
		})
	}
}

func TestNewUpdateWellnessTool_APIError(t *testing.T) {
	t.Parallel()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer apiServer.Close()

	result := callUpdateWellness(t, apiServer, map[string]any{"date": "2026-11-14", "fatigue": 2})

	if !result.IsError {
		t.Fatalf("expected IsError to be true, got: %v", result.IsError)
	}
}
