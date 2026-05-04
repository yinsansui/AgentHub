package main

import (
	"log"
	"net/http"
	"os"

	"agenthub/internal/agentpod"
)

func main() {
	addr := os.Getenv("AGENT_POD_ADDR")
	if addr == "" {
		addr = ":3001"
	}
	server := agentpod.NewServerFromEnv()
	log.Printf("agent-pod listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}
