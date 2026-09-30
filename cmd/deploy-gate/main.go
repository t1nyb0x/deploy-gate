package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/t1nyb0x/deploy-gate/internal/config"
	"github.com/t1nyb0x/deploy-gate/internal/deploy"
	"github.com/t1nyb0x/deploy-gate/internal/webhook"
)

func main() {
	secret := os.Getenv("DEPLOY_SECRET")
	configPath := os.Getenv("DEPLOY_CONFIG")

	if secret == "" {
		log.Fatal("DEPLOY_SECRET is required")
	}

	if configPath == "" {
		log.Fatal("DEPLOY_CONFIG is required")
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
			s = deploy.NewSerial(route.Script, deploy.Run)
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

	log.Println("listening on :9000")
	log.Fatal(server.ListenAndServe())
}