package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	expireDef, _ := strconv.Atoi(env("DUDLE_EXPIRE_DAYS", "365"))
	addr := flag.String("addr", env("DUDLE_ADDR", ":8080"), "listen address (env DUDLE_ADDR)")
	dbPath := flag.String("db", env("DUDLE_DB", "dudle.db"), "SQLite database file (env DUDLE_DB)")
	baseURL := flag.String("base-url", env("DUDLE_BASE_URL", ""), "public base URL, e.g. https://dudle.example.org (env DUDLE_BASE_URL)")
	expire := flag.Int("expire-days", expireDef, "delete polls inactive for this many days, 0 = never (env DUDLE_EXPIRE_DAYS)")
	rateDef, _ := strconv.Atoi(env("DUDLE_RATE_LIMIT", "60"))
	rate := flag.Int("rate-limit", rateDef, "max write requests (POST) per client per minute, 0 = off (env DUDLE_RATE_LIMIT)")
	trustProxy := flag.Bool("trust-proxy", env("DUDLE_TRUST_PROXY", "") == "1", "identify clients by X-Forwarded-For; only behind a reverse proxy (env DUDLE_TRUST_PROXY=1)")
	basePath := flag.String("base-path", env("DUDLE_BASE_PATH", ""), "serve under this URL path prefix, e.g. /dudle (env DUDLE_BASE_PATH)")
	flag.Parse()

	store, err := OpenStore(*dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer store.Close()

	srv, err := NewServer(store, *baseURL)
	if err != nil {
		log.Fatal(err)
	}

	srv.SetBasePath(*basePath)
	srv.SetRateLimit(*rate, *trustProxy)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *expire > 0 {
		go cleanupLoop(ctx, store, time.Duration(*expire)*24*time.Hour)
	}

	hs := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hs.Shutdown(shutdownCtx)
	}()

	log.Printf("dudle-ng listening on %s (db: %s)", *addr, *dbPath)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func cleanupLoop(ctx context.Context, store *Store, maxAge time.Duration) {
	run := func() {
		n, err := store.DeleteInactive(time.Now().Add(-maxAge))
		if err != nil {
			log.Printf("cleanup: %v", err)
		} else if n > 0 {
			log.Printf("cleanup: removed %d inactive polls", n)
		}
	}
	run()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
