package main

import (
	"github.com/ccplant/ccplant/e2e/internal/api"
	"github.com/ccplant/ccplant/e2e/internal/catalog"
	"github.com/ccplant/ccplant/e2e/internal/store"
	"log"
	"net/http"
	"os"
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
	log.Printf("e2e api listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, api.New(s, c).Handler()))
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
