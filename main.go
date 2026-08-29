package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tinyfish "github.com/snowmerak/tinyfish-go/lib"
	"github.com/snowmerak/tinyfish-go/lib/mcpserver"
)

const version = "0.1.0"

func main() {
	logger := log.New(os.Stderr, "tinyfish-mcp: ", 0)
	if err := run(); err != nil {
		logger.Print(err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := tinyfish.New(tinyfish.WithUserAgent("tinyfish-go-mcp/" + version))
	if err != nil {
		return err
	}
	server, err := mcpserver.New(client, version)
	if err != nil {
		return err
	}
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
