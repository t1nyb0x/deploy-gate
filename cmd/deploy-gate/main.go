package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/t1nyb0x/deploy-gate/internal/config"
	"github.com/t1nyb0x/deploy-gate/internal/deploy"
	"github.com/t1nyb0x/deploy-gate/internal/webhook"
)

const (
	defaultShutdownTimeout = 30 * time.Second
	defaultOutputLimit     = 4096
)

func parseOutputLimit(value string) (int, error) {
	if value == "" {
		return defaultOutputLimit, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", value, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("must not be negative: %q", value)
	}
	return n, nil
}

func parseShutdownTimeout(value string) (time.Duration, error) {
	if value == "" {
		return defaultShutdownTimeout, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", value, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be positive: %q", value)
	}
	return d, nil
}

func main() {
	secret := os.Getenv("DEPLOY_SECRET")
	configPath := os.Getenv("DEPLOY_CONFIG")

	if secret == "" {
		log.Fatal("DEPLOY_SECRET is required")
	}

	if configPath == "" {
		log.Fatal("DEPLOY_CONFIG is required")
	}

	shutdownTimeout, err := parseShutdownTimeout(os.Getenv("DEPLOY_SHUTDOWN_TIMEOUT"))
	if err != nil {
		log.Fatalf("DEPLOY_SHUTDOWN_TIMEOUT: %v", err)
	}

	outputLimit, err := parseOutputLimit(os.Getenv("DEPLOY_LOG_OUTPUT_BYTES"))
	if err != nil {
		log.Fatalf("DEPLOY_LOG_OUTPUT_BYTES: %v", err)
	}

	runner := deploy.Runner{
		MaxOutput: outputLimit,
		// Scripts never need the webhook secret; keep it out of their environment and output.
		DropEnv: []string{"DEPLOY_SECRET"},
		Redact:  []string{secret},
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	mux := http.NewServeMux()

	// Share one Serial per script so routes pointing at the same script never run it concurrently.
	serials := map[string]*deploy.Serial{}

	for _, route := range cfg.Routes {
		log.Printf("register route: path=%s script=%s branch=%s", route.Path, route.Script, route.Branch)
		if route.Branch == "" {
			log.Printf("warning: route %s has no branch; pushes to any branch will deploy", route.Path)
		}
		s, ok := serials[route.Script]
		if !ok {
			s = deploy.NewSerial(route.Script, runner.Run)
			serials[route.Script] = s
		}
		mux.HandleFunc(route.Path, webhook.Deploy(secret, route, s))
	}

	server := &http.Server{
		Addr:              ":9000",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.Println("listening on :9000")
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		log.Fatal(err)
	case <-ctx.Done():
	}
	// Restore default signal handling so a second signal terminates immediately.
	stop()

	log.Printf("shutting down: timeout=%s", shutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown: %v", err)
	}

	// Serials share one deadline; once it passes, the remaining deploys are killed immediately.
	for script, s := range serials {
		if err := s.Shutdown(shutdownCtx); err != nil {
			log.Printf("deploy shutdown: script=%s error=%v", script, err)
		}
	}

	log.Println("shutdown complete")
}
