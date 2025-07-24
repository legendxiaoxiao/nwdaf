// src/nwdaf/nwdaf.go
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/free5gc/nwdaf/pkg/service"
)

var NWDAF = &service.NWDAF{}

func main() {
	// Initialize service
	NWDAF.Initialize()

	// Start the service
	go NWDAF.Start()

	// Wait for term signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// Terminate the service
	NWDAF.Terminate()
}
