package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"xhs-workbench/internal/app"
)

func main() {
	root := flag.String("root", "..", "workspace directory containing data and worker adapters")
	data := flag.String("data-dir", "data", "persistent data directory")
	addr := flag.String("addr", "127.0.0.1:8082", "API loopback address")
	origins := flag.String("allowed-origins", "http://127.0.0.1:8080,http://localhost:8080,http://127.0.0.1:5173,http://localhost:5173", "comma-separated exact frontend origins")
	flag.Parse()

	// The workbench is a local desktop service; reject non-loopback binds.
	host, _, e := net.SplitHostPort(*addr)
	if e != nil || host != "127.0.0.1" {
		log.Fatal("--addr must bind 127.0.0.1")
	}
	abs, _ := filepath.Abs(*root)
	a, e := app.New(abs, *data, *addr)
	if e != nil {
		log.Fatal(e)
	}
	if e = a.ConfigureFrontendOrigins(strings.Split(*origins, ",")); e != nil {
		a.Close()
		log.Fatal(e)
	}
	// Start background jobs only after application recovery has completed.
	a.StartWorkers()
	srv := &http.Server{Addr: *addr, Handler: a.Router(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		// Drain active requests before closing workers and persistent storage.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	log.Printf("小红书工作台 API http://%s/api/v1", *addr)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Printf("server: %v", e)
	}
	a.Close()
}
