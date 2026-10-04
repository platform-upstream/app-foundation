package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/niiku-platform/app-foundation/access"
	"github.com/niiku-platform/app-foundation/memory"
	"github.com/niiku-platform/app-foundation/reference-app/internal/reference"
	"github.com/niiku-platform/app-foundation/reference-app/migrations"
	webassets "github.com/niiku-platform/app-foundation/reference-app/web"
	"github.com/niiku-platform/app-foundation/server"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	config, err := reference.ConfigFromEnvironment()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := reference.OpenStore(ctx, config.DatabasePath, migrations.Files)
	if err != nil {
		return err
	}
	defer db.Close()
	memoryClient, err := memory.New(db, memory.Config{
		BaseURL: config.MemoryBaseURL,
		APIKey:  config.MemoryAPIKey,
		App:     "foundation-reference",
	})
	if err != nil {
		return err
	}
	llmClient := reference.NewLLMClient(config.LLMBaseURL, config.LLMAPIKey, nil)
	authenticator, err := access.New(access.Config{
		Mode:       config.AuthMode,
		Listener:   config.Address,
		TeamDomain: config.CFTeamDomain,
		Audience:   config.CFAudience,
	})
	if err != nil {
		return err
	}
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		return err
	}
	handler, err := reference.NewHandler(reference.HandlerConfig{
		Database:       db,
		Assets:         assets,
		Authenticator:  authenticator,
		AllowedOrigins: []string{config.Origin},
		Memory:         memoryClient,
		LLM:            llmClient,
	})
	if err != nil {
		return err
	}
	log.Printf("foundation reference listening on %s", config.Address)
	return server.Run(ctx, server.New(config.Address, handler))
}
