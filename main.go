package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type App struct {
	Config         *Config
	ActiveMonitors map[string]bool
	Completed      map[string]bool
	WakeCh         chan struct{}
	Mutex          sync.Mutex
	Wg             sync.WaitGroup
	Ctx            context.Context
	Cancel         context.CancelFunc
}

func (a *App) wakeCoordinator() {
	if a.WakeCh == nil {
		return
	}
	select {
	case a.WakeCh <- struct{}{}:
	default:
	}
}

func main() {
	log.SetFlags(0) // K8s handles timestamps

	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		Config:         loadConfig(),
		ActiveMonitors: make(map[string]bool),
		Completed:      make(map[string]bool),
		WakeCh:         make(chan struct{}, 1),
		Ctx:            ctx,
		Cancel:         cancel,
	}

	// 2. Start Trigger Server
	http.HandleFunc("/track", app.ipFilterMiddleware(app.handleTrackRequest))

	port := "9090"
	log.Printf("Sidecar listening on :%s", port)
	log.Printf("Config: Host=%s Auth=%v Topic=%s/*** NtfyAuth=%v", app.Config.QbitHost, app.Config.QbitUser != "", app.Config.NtfyServer, app.Config.NtfyUser != "")

	defer app.Cancel()

	// 3. Run Startup Scan (Background)
	app.Wg.Add(1)
	go app.startupScan()

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
	app.Cancel()

	// Shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	// Wait for workers
	log.Println("Waiting for background workers...")
	app.Wg.Wait()

	log.Println("Sidecar exited gracefully")
}
