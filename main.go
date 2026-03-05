package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(0) // K8s handles timestamps

	loadConfig()

	// 2. Start Trigger Server
	http.HandleFunc("/track", ipFilterMiddleware(handleTrackRequest))

	port := "9090"
	log.Printf("Sidecar listening on :%s", port)
	log.Printf("Config: Host=%s Auth=%v Topic=%s/%s NtfyAuth=%v", qbitHost, qbitUser != "", ntfyServer, ntfyTopic, ntfyUser != "")

	// Global Context for shutdown signaling
	appCtx, appCancel = context.WithCancel(context.Background())
	defer appCancel()

	// 3. Run Startup Scan (Background)
	appWg.Add(1)
	go startupScan()

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      nil, // DefaultServeMux
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	// 4. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	// SIGINT (Ctrl+C) and SIGTERM (Kubernetes/Docker stop)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down sidecar...")

	// Signal workers to stop
	appCancel()

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	// Wait for workers
	log.Println("Waiting for background workers...")
	appWg.Wait()

	log.Println("Sidecar exited gracefully")
}
