package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tinyfish "github.com/snowmerak/tinyfish-go/lib"
)

func TestServerToolsAndCalls(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			fmt.Fprint(writer, `{"query":"tinyfish","results":[{"position":1,"site_name":"tinyfish.ai","title":"TinyFish","snippet":"Web APIs","url":"https://tinyfish.ai"}],"total_results":1,"page":0}`)
		case http.MethodPost:
			fmt.Fprint(writer, `{"results":[{"url":"https://tinyfish.ai","final_url":"https://tinyfish.ai","title":"TinyFish","description":null,"language":"en","author":null,"published_date":null,"text":"page text","latency_ms":1,"format":"markdown"}],"errors":[]}`)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer upstream.Close()

	apiClient, err := tinyfish.New(
		tinyfish.WithAPIKey("test-key"),
		tinyfish.WithEndpoints(upstream.URL, upstream.URL),
		tinyfish.WithoutRateLimiting(),
		tinyfish.WithRetryPolicy(tinyfish.RetryPolicy{MaxAttempts: 1}),
	)
	if err != nil {
		t.Fatalf("tinyfish.New() error = %v", err)
	}
	server, err := New(apiClient, "test")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverTransport)
	}()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "search") || !slices.Contains(names, "fetch_content") {
		t.Fatalf("tool names = %v", names)
	}

	searchResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": "tinyfish"},
	})
	if err != nil {
		t.Fatalf("CallTool(search) error = %v", err)
	}
	if searchResult.IsError {
		t.Fatalf("CallTool(search) returned tool error: %+v", searchResult)
	}
	searchJSON, err := json.Marshal(searchResult.StructuredContent)
	if err != nil {
		t.Fatalf("marshal search output: %v", err)
	}
	var searchOutput tinyfish.SearchResponse
	if err := json.Unmarshal(searchJSON, &searchOutput); err != nil {
		t.Fatalf("decode search output: %v", err)
	}
	if searchOutput.TotalResults != 1 {
		t.Errorf("search total_results = %d, want 1", searchOutput.TotalResults)
	}

	fetchResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "fetch_content",
		Arguments: map[string]any{"urls": []string{"https://tinyfish.ai"}},
	})
	if err != nil {
		t.Fatalf("CallTool(fetch_content) error = %v", err)
	}
	if fetchResult.IsError {
		t.Fatalf("CallTool(fetch_content) returned tool error: %+v", fetchResult)
	}
	fetchJSON, err := json.Marshal(fetchResult.StructuredContent)
	if err != nil {
		t.Fatalf("marshal fetch output: %v", err)
	}
	var fetchOutput struct {
		Results []struct {
			Text any `json:"text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(fetchJSON, &fetchOutput); err != nil {
		t.Fatalf("decode fetch output: %v", err)
	}
	if len(fetchOutput.Results) != 1 || fetchOutput.Results[0].Text != "page text" {
		t.Errorf("unexpected fetch output: %s", fetchJSON)
	}
}

func TestNewRejectsNilClient(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, "test"); err == nil {
		t.Fatal("New(nil) error = nil")
	}
}
