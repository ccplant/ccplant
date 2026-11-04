package main

import (
	"context"
	"github.com/ccplant/ccplant/e2e/internal/api"
	"github.com/ccplant/ccplant/e2e/internal/catalog"
	"github.com/ccplant/ccplant/e2e/internal/mcpserver"
	"github.com/ccplant/ccplant/e2e/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	catalogPath := env("E2E_CATALOG", "scenarios/catalog.yaml")
	dbPath := env("E2E_DATABASE", "e2e.db")
	addr := env("E2E_API_ADDR", ":8081")
	c, err := catalog.Load(catalogPath)
	if err != nil {
		log.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	service := api.NewReloadable(s, c, catalogPath)
	if len(os.Args) == 2 && os.Args[1] == "mcp" {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal(err)
		}
		httpServer := &http.Server{Handler: service.Handler()}
		go func() {
			if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				log.Printf("e2e api failed: %v", err)
			}
		}()
		mcpServer := mcpserver.New(service, mcpserver.Config{
			TargetAPIURL:      os.Getenv("E2E_TARGET_API_URL"),
			TargetFrontendURL: os.Getenv("E2E_TARGET_FRONTEND_URL"),
			AuthType:          os.Getenv("E2E_AUTH_TYPE"),
			AuthValue:         os.Getenv("E2E_AUTH_VALUE"),
		})
		if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Printf("mcp server failed: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		return
	}
	if len(os.Args) != 1 {
		log.Fatalf("usage: %s [mcp]", os.Args[0])
	}
	log.Printf("e2e api listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, service.Handler()))
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
