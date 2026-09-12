package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// apiMetrics agrega as métricas Prometheus da API. Além do padrão HTTP básico
// (requisições, duração, em andamento), mantém um contador por código de erro
// do MK, para diferenciar timeout/indisponibilidade/registro-não-encontrado
// sem nunca usar documento, telefone ou nome como label.
type apiMetrics struct {
	requestsTotal            *prometheus.CounterVec
	requestDuration          *prometheus.HistogramVec
	inFlight                 prometheus.Gauge
	mkErrorsTotal            *prometheus.CounterVec
	autodesbloqueioResultado *prometheus.CounterVec
	llmClassificacaoTotal    *prometheus.CounterVec
	llmErrosTotal            *prometheus.CounterVec
	handler                  http.Handler
}

// newAPIMetrics usa um registry Prometheus próprio (em vez do registry global
// padrão), para que múltiplas instâncias do handler no mesmo processo — como
// acontece nos testes — não colidam registrando a mesma métrica duas vezes.
func newAPIMetrics() *apiMetrics {
	registry := prometheus.NewRegistry()
	factory := promauto.With(registry)

	return &apiMetrics{
		requestsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mk_octadesk_http_requests_total",
			Help: "Total de requisições HTTP recebidas, por rota e status HTTP.",
		}, []string{"route", "status"}),
		requestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mk_octadesk_http_request_duration_seconds",
			Help:    "Duração das requisições HTTP, por rota.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		inFlight: factory.NewGauge(prometheus.GaugeOpts{
			Name: "mk_octadesk_http_in_flight_requests",
			Help: "Requisições HTTP em andamento.",
		}),
		mkErrorsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mk_octadesk_mk_errors_total",
			Help: "Total de erros ao consultar o MK, por rota e código de erro interno.",
		}, []string{"route", "codigo"}),
		autodesbloqueioResultado: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mk_octadesk_autodesbloqueio_resultado_total",
			Help: "Total de chamadas de autodesbloqueio, por desfecho de negócio (sucesso, não bloqueada, limite mensal já atingido).",
		}, []string{"resultado"}),
		llmClassificacaoTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mk_octadesk_llm_classificacao_total",
			Help: "Total de mensagens classificadas pela LLM, por destino (nunca inclui o texto da mensagem).",
		}, []string{"destino"}),
		llmErrosTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mk_octadesk_llm_erros_total",
			Help: "Total de erros ao classificar mensagens via LLM, por código de erro interno.",
		}, []string{"codigo"}),
		handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	}
}

func (metrics *apiMetrics) recordMKError(route, codigo string) {
	metrics.mkErrorsTotal.WithLabelValues(route, codigo).Inc()
}

func (metrics *apiMetrics) recordAutodesbloqueio(resultado string) {
	metrics.autodesbloqueioResultado.WithLabelValues(resultado).Inc()
}

func (metrics *apiMetrics) recordClassificacao(destino string) {
	metrics.llmClassificacaoTotal.WithLabelValues(destino).Inc()
}

func (metrics *apiMetrics) recordLLMErro(codigo string) {
	metrics.llmErrosTotal.WithLabelValues(codigo).Inc()
}

// instrument mede toda requisição atendida por next. A rota usada como label
// é o caminho da URL, que nunca contém dados de cliente (esses vão só na
// query string, que não é usada como label).
func (metrics *apiMetrics) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		metrics.inFlight.Inc()
		defer metrics.inFlight.Dec()

		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(recorder, request)

		route := request.URL.Path
		metrics.requestsTotal.WithLabelValues(route, strconv.Itoa(recorder.status)).Inc()
		metrics.requestDuration.WithLabelValues(route).Observe(time.Since(started).Seconds())
	})
}
