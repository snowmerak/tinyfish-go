# tinyfish-go

An idiomatic Go client and local MCP server for [TinyFish](https://tinyfish.ai).

The current milestone implements the free Search and Fetch APIs. Agent and
Browser support will be added on top of the same transport and lifecycle
foundation.

## Go client

The reusable implementation lives in `lib/` and uses package name `tinyfish`.

```go
package main

import (
	"context"
	"fmt"
	"log"

	tinyfish "github.com/snowmerak/tinyfish-go/lib"
)

func main() {
	client, err := tinyfish.New() // reads TINYFISH_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	result, err := client.Search.Query(context.Background(), tinyfish.SearchRequest{
		Query:    "Go MCP SDK",
		Language: "en",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, item := range result.Results {
		fmt.Println(item.Title, item.URL)
	}
}
```

Fetch one or more pages:

```go
result, err := client.Fetch.GetContents(ctx, tinyfish.FetchRequest{
	URLs:   []string{"https://docs.tinyfish.ai"},
	Format: tinyfish.FetchFormatMarkdown,
})
if err != nil {
	return err
}

for _, page := range result.Results {
	text, err := page.TextString()
	if err != nil {
		return err
	}
	fmt.Println(text)
}
```

`FetchResponse` can contain successful `Results` and per-URL `Errors` at the
same time. HTTP-level failures are returned as `*tinyfish.APIError`.

The client applies these proactive defaults per instance using an exact,
weighted sliding window over the previous 60 seconds:

- Search: 30 requests per minute
- Fetch: 150 URLs per minute; a ten-URL request consumes ten units
- safe retries: up to three attempts for transport errors and HTTP
  `429`, `502`, `503`, and `504`

The API remains the source of truth. Use `tinyfish.WithLimits`,
`tinyfish.WithoutRateLimiting`, and `tinyfish.WithRetryPolicy` to customize the
local behavior.

Fetch usage history is available without consuming the Fetch URL limiter:

```go
usage, err := client.Fetch.ListUsage(ctx, tinyfish.FetchUsageRequest{
	Status: tinyfish.FetchUsageCompleted,
	Limit:  100,
	Page:   1,
})
```

## MCP server

The root `main.go` is a thin stdio MCP entry point. It exposes:

- `search`
- `fetch_content`
- `list_fetch_usage`

Build it with:

```sh
go build -o tinyfish-mcp .
```

Example MCP client configuration:

```json
{
  "mcpServers": {
    "tinyfish": {
      "command": "/absolute/path/to/tinyfish-mcp",
      "env": {
        "TINYFISH_API_KEY": "your-api-key"
      }
    }
  }
}
```

The server writes MCP messages only to stdout. Startup and fatal errors go to
stderr so they cannot corrupt the stdio protocol.

## Development

All regular tests are offline and use local mock HTTP servers. They never use a
TinyFish API key or make paid requests.

```sh
go test ./...
go test -race ./...
go vet ./...
```

API documentation:

- [Search API](https://docs.tinyfish.ai/search-api/reference)
- [Fetch API](https://docs.tinyfish.ai/fetch-api/reference)
- [TinyFish MCP integration](https://docs.tinyfish.ai/mcp-integration)
