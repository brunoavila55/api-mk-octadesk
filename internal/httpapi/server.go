package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"api-mk-octadesk/internal/llm"
	"api-mk-octadesk/internal/mk"
)

func NewHandler(client *mk.Client, cloudflareClient *llm.CloudflareClient, apiKey string, logger *slog.Logger) http.Handler {
	metrics := newAPIMetrics()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONStatus(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /metrics", metrics.handler)

	handlers := &handlers{client: client, cloudflareClient: cloudflareClient, logger: logger, metrics: metrics}

	mux.Handle("GET /v1/consulta-documento", requireAPIKey(apiKey, http.HandlerFunc(handlers.consultaDocumento)))
	mux.Handle("GET /v1/consulta-conexao", requireAPIKey(apiKey, http.HandlerFunc(handlers.consultaConexao)))
	mux.Handle("GET /v1/consulta-notificacao-ativa", requireAPIKey(apiKey, http.HandlerFunc(handlers.consultaNotificacaoAtiva)))
	mux.Handle("GET /v1/consulta-notifica-cliente", requireAPIKey(apiKey, http.HandlerFunc(handlers.consultaNotificaCliente)))
	mux.Handle("GET /v1/gera-boleto", requireAPIKey(apiKey, http.HandlerFunc(handlers.geraBoleto)))
	mux.Handle("GET /v1/gera-pix", requireAPIKey(apiKey, http.HandlerFunc(handlers.geraPix)))
	mux.Handle("GET /v1/autodesbloqueio", requireAPIKey(apiKey, http.HandlerFunc(handlers.autoDesbloqueio)))
	mux.Handle("POST /v1/llm-cf-classifica-mensagem", requireAPIKey(apiKey, http.HandlerFunc(handlers.classificaMensagemCloudflare)))

	return metrics.instrument(requestLog(mux, logger))
}

func requireAPIKey(expected string, next http.Handler) http.Handler {
	expectedHash := sha256.Sum256([]byte(expected))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providedHash := sha256.Sum256([]byte(strings.TrimSpace(request.Header.Get("X-API-Key"))))
		if subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) != 1 {
			writeError(writer, http.StatusUnauthorized, "nao_autorizado", "Chave de acesso ausente ou inválida.")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

// requestLog loga apenas method/path/status/duração — nunca a query string,
// que carrega documento, código de cliente/conexão e, em rotas antigas, a
// própria chave de acesso.
func requestLog(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(recorder, request)
		logger.Info("requisição HTTP", "method", request.Method, "path", request.URL.Path, "status", recorder.status, "duration", time.Since(started))
	})
}
