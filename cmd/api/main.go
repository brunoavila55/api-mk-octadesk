package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"api-mk-octadesk/internal/config"
	"api-mk-octadesk/internal/httpapi"
	"api-mk-octadesk/internal/llm"
	"api-mk-octadesk/internal/mk"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuração inválida", "error", err)
		os.Exit(1)
	}

	client := mk.NewClient(cfg)
	llmClient := llm.NewClient(cfg)
	handler := httpapi.NewHandler(client, llmClient, cfg.ChatbotAPIKey, logger)

	// WriteTimeout precisa acomodar o LLM_HTTP_TIMEOUT (padrão 60s) da rota
	// de classificação, além de alguma folga — sem isso o servidor cortaria
	// a resposta antes do Ollama terminar de responder.
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      cfg.LLMHTTPTimeout + 15*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("servidor iniciado", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("servidor encerrado com erro", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("encerrando servidor")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("erro ao encerrar servidor", "error", err)
	}
}
