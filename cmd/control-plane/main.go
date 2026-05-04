package main

import (
	"log"
	"net/http"

	"agenthub/internal/controlplane"
)

func main() {
	config := controlplane.LoadConfig()
	server := controlplane.NewServer(config)
	log.Printf("control-plane listening on %s", config.Addr)
	if err := http.ListenAndServe(config.Addr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}
