package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"api-mk-octadesk/internal/config"
	"api-mk-octadesk/internal/llm"
	"api-mk-octadesk/internal/mk"
)

const testAPIKey = "chave-de-teste-com-24-caracteres"

// newTestCloudflareClient monta um *llm.CloudflareClient apontando para um
// servidor Cloudflare Workers AI de teste (fake).
func newTestCloudflareClient(t *testing.T, cfMux *http.ServeMux) *llm.CloudflareClient {
	t.Helper()
	if cfMux == nil {
		cfMux = http.NewServeMux()
	}
	cfServer := httptest.NewServer(cfMux)
	t.Cleanup(cfServer.Close)

	baseURL, err := url.Parse(cfServer.URL + "/")
	if err != nil {
		t.Fatalf("erro ao montar URL do servidor Cloudflare de teste: %v", err)
	}

	return llm.NewCloudflareClient(config.Config{
		CloudflareAIBaseURL:   baseURL,
		CloudflareAccountID:   "conta-teste",
		CloudflareAPIToken:    "token-teste",
		CloudflareAIModel:     "@cf/meta/llama-3.1-8b-instruct-fp8-fast",
		CloudflareHTTPTimeout: 2 * time.Second,
	})
}

func newTestHandler(t *testing.T, mkMux *http.ServeMux) (http.Handler, *bytes.Buffer) {
	t.Helper()
	mkServer := httptest.NewServer(mkMux)
	t.Cleanup(mkServer.Close)

	baseURL, err := url.Parse(mkServer.URL)
	if err != nil {
		t.Fatalf("erro ao montar URL do servidor MK de teste: %v", err)
	}

	cfg := config.Config{
		MKBaseURL:               baseURL,
		MKServiceCode:           "9999",
		MKTemporaryAuthToken:    "token-fixo",
		MKHTTPTimeout:           2 * time.Second,
		MKTemporaryAuthTokenTTL: 5 * time.Minute,
	}

	client := mk.NewClient(cfg)
	cloudflareClient := newTestCloudflareClient(t, nil)
	logBuffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuffer, nil))

	return NewHandler(client, cloudflareClient, testAPIKey, logger), logBuffer
}

func writeJSONFixture(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func TestConsultaConexao_SemAPIKey(t *testing.T) {
	handler, _ := newTestHandler(t, http.NewServeMux())

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=42", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401, obteve %d", recorder.Code)
	}
}

func TestConsultaConexao_APIKeyIncorreta(t *testing.T) {
	handler, _ := newTestHandler(t, http.NewServeMux())

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=42", nil)
	request.Header.Set("X-API-Key", "chave-errada")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401, obteve %d", recorder.Code)
	}
}

func TestConsultaConexao_ParametroAusente(t *testing.T) {
	handler, _ := newTestHandler(t, http.NewServeMux())

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", recorder.Code)
	}
}

func TestConsultaConexao_ParametroFormatoInvalido(t *testing.T) {
	handler, _ := newTestHandler(t, http.NewServeMux())

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=abc", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", recorder.Code)
	}
}

func TestConsultaConexao_Sucesso(t *testing.T) {
	mkMux := http.NewServeMux()
	mkMux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{
			"status": "OK",
			"Conexoes": []map[string]string{
				{"codconexao": "123", "endereco": "Rua A, 1", "bloqueada": "Não"},
			},
		})
	})
	mkMux.HandleFunc("/core-api/notificacoes/status", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, []map[string]string{})
	})

	handler, logBuffer := newTestHandler(t, mkMux)

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=42", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recorder.Code, recorder.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("esperava status ok, obteve %v", body["status"])
	}
	dados, ok := body["dados"].([]any)
	if !ok || len(dados) != 3 {
		t.Fatalf("esperava 3 itens (1 real + 2 placeholders), obteve %v", body["dados"])
	}
	primeira, ok := dados[0].(map[string]any)
	if !ok || primeira["notificado"] != false {
		t.Fatalf("esperava notificado=false sem notificação ativa, obteve %v", dados[0])
	}

	if bytes.Contains(logBuffer.Bytes(), []byte("cd_cliente=42")) {
		t.Fatal("log não deveria conter a query string com dados do cliente")
	}
}

func TestConsultaConexao_ComNotificacaoAtiva(t *testing.T) {
	mkMux := http.NewServeMux()
	mkMux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{
			"status": "OK",
			"Conexoes": []map[string]string{
				{"codconexao": "123", "endereco": "Rua A, 1", "bloqueada": "Não"},
			},
		})
	})
	mkMux.HandleFunc("/core-api/notificacoes/status", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, []map[string]string{{"cod": "55"}})
	})
	mkMux.HandleFunc("/core-api/notificacoes/conexoes-afetadas", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, []map[string]string{{"codconexao": "123"}})
	})

	handler, _ := newTestHandler(t, mkMux)

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=42", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recorder.Code, recorder.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	dados, ok := body["dados"].([]any)
	if !ok || len(dados) != 3 {
		t.Fatalf("esperava 3 itens, obteve %v", body["dados"])
	}
	primeira, ok := dados[0].(map[string]any)
	if !ok || primeira["notificado"] != true {
		t.Fatalf("esperava notificado=true na conexão afetada, obteve %v", dados[0])
	}
	segunda, ok := dados[1].(map[string]any)
	if !ok || segunda["notificado"] != false {
		t.Fatalf("esperava notificado=false no placeholder, obteve %v", dados[1])
	}
}

func TestConsultaConexao_NotificacaoIndisponivel(t *testing.T) {
	mkMux := http.NewServeMux()
	mkMux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{
			"status": "OK",
			"Conexoes": []map[string]string{
				{"codconexao": "123", "endereco": "Rua A, 1", "bloqueada": "Não"},
			},
		})
	})
	mkMux.HandleFunc("/core-api/notificacoes/status", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	})

	handler, _ := newTestHandler(t, mkMux)

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=42", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("esperava 502, obteve %d", recorder.Code)
	}
}

func TestConsultaConexao_NaoEncontrado(t *testing.T) {
	mkMux := http.NewServeMux()
	mkMux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{"status": "ERRO"})
	})

	handler, _ := newTestHandler(t, mkMux)

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=42", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("esperava 404, obteve %d", recorder.Code)
	}
}

func TestConsultaConexao_MKIndisponivel(t *testing.T) {
	mkMux := http.NewServeMux()
	mkMux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	})

	handler, _ := newTestHandler(t, mkMux)

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=42", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("esperava 502, obteve %d", recorder.Code)
	}
}

func TestConsultaConexao_MKTimeout(t *testing.T) {
	mkMux := http.NewServeMux()
	mkMux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
	})

	mkServer := httptest.NewServer(mkMux)
	t.Cleanup(mkServer.Close)
	baseURL, _ := url.Parse(mkServer.URL)

	cfg := config.Config{
		MKBaseURL:               baseURL,
		MKServiceCode:           "9999",
		MKTemporaryAuthToken:    "token-fixo",
		MKHTTPTimeout:           20 * time.Millisecond,
		MKTemporaryAuthTokenTTL: 5 * time.Minute,
	}
	client := mk.NewClient(cfg)
	cloudflareClient := newTestCloudflareClient(t, nil)
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	handler := NewHandler(client, cloudflareClient, testAPIKey, logger)

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-conexao?cd_cliente=42", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("esperava 504, obteve %d", recorder.Code)
	}
}

func TestConsultaDocumento_DocumentoInvalido(t *testing.T) {
	handler, _ := newTestHandler(t, http.NewServeMux())

	request := httptest.NewRequest(http.MethodGet, "/v1/consulta-documento?documento=123", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", recorder.Code)
	}
}

func TestAutoDesbloqueio_RegistraMetricaPorDesfecho(t *testing.T) {
	mkMux := http.NewServeMux()
	mkMux.HandleFunc("/mk/WSMKAutoDesbloqueio.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]string{
			"status":   "ERRO",
			"mensagem": "Operação indisponível para esta conexão.",
		})
	})

	handler, _ := newTestHandler(t, mkMux)

	request := httptest.NewRequest(http.MethodGet, "/v1/autodesbloqueio?cd_conexao=34586", nil)
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recorder.Code, recorder.Body.String())
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metricsRecorder, metricsRequest)

	body := metricsRecorder.Body.String()
	expected := `mk_octadesk_autodesbloqueio_resultado_total{resultado="limite_mensal_atingido"} 1`
	if !strings.Contains(body, expected) {
		t.Fatalf("esperava métrica %q em /metrics, não encontrada:\n%s", expected, body)
	}
}

// newTestHandlerComCloudflare monta o handler completo com um MK vazio (não
// usado pelos testes de classificação) e o servidor Cloudflare de teste
// fornecido.
func newTestHandlerComCloudflare(t *testing.T, cfMux *http.ServeMux) (http.Handler, *bytes.Buffer) {
	t.Helper()
	mkServer := httptest.NewServer(http.NewServeMux())
	t.Cleanup(mkServer.Close)
	baseURL, _ := url.Parse(mkServer.URL)

	client := mk.NewClient(config.Config{
		MKBaseURL:               baseURL,
		MKServiceCode:           "9999",
		MKTemporaryAuthToken:    "token-fixo",
		MKHTTPTimeout:           2 * time.Second,
		MKTemporaryAuthTokenTTL: 5 * time.Minute,
	})
	cloudflareClient := newTestCloudflareClient(t, cfMux)
	logBuffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuffer, nil))

	return NewHandler(client, cloudflareClient, testAPIKey, logger), logBuffer
}

const cfRunPath = "/accounts/conta-teste/ai/run/@cf/meta/llama-3.1-8b-instruct-fp8-fast"

func TestClassificaMensagemCloudflare_MensagemVazia(t *testing.T) {
	handler, _ := newTestHandlerComCloudflare(t, nil)

	request := httptest.NewRequest(http.MethodPost, "/v1/llm-cf-classifica-mensagem", strings.NewReader(`{"mensagem":"   "}`))
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", recorder.Code)
	}
}

func TestClassificaMensagemCloudflare_CorpoInvalido(t *testing.T) {
	handler, _ := newTestHandlerComCloudflare(t, nil)

	request := httptest.NewRequest(http.MethodPost, "/v1/llm-cf-classifica-mensagem", strings.NewReader(`{isso não é json`))
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("esperava 400, obteve %d", recorder.Code)
	}
}

func TestClassificaMensagemCloudflare_Sucesso(t *testing.T) {
	cfMux := http.NewServeMux()
	cfMux.HandleFunc(cfRunPath, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{
			"success": true,
			"result":  map[string]any{"response": map[string]string{"destino_principal": "suporte"}},
		})
	})

	handler, logBuffer := newTestHandlerComCloudflare(t, cfMux)

	request := httptest.NewRequest(http.MethodPost, "/v1/llm-cf-classifica-mensagem", strings.NewReader(`{"mensagem":"estou sem internet, quando vai voltar?"}`))
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recorder.Code, recorder.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	dados, ok := body["dados"].(map[string]any)
	if !ok || dados["destino"] != "suporte" {
		t.Fatalf("esperava dados.destino = suporte, obteve %v", body["dados"])
	}

	if bytes.Contains(logBuffer.Bytes(), []byte("estou sem internet")) {
		t.Fatal("log não deveria conter o texto da mensagem do cliente")
	}
}

func TestClassificaMensagemCloudflare_FalhaReportada(t *testing.T) {
	cfMux := http.NewServeMux()
	cfMux.HandleFunc(cfRunPath, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{
			"success": false,
			"errors":  []map[string]any{{"code": 7003, "message": "credenciais inválidas"}},
		})
	})

	handler, _ := newTestHandlerComCloudflare(t, cfMux)

	request := httptest.NewRequest(http.MethodPost, "/v1/llm-cf-classifica-mensagem", strings.NewReader(`{"mensagem":"estou sem internet"}`))
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("esperava 502, obteve %d", recorder.Code)
	}
}

func TestClassificaMensagemCloudflare_Indisponivel(t *testing.T) {
	cfMux := http.NewServeMux()
	cfMux.HandleFunc(cfRunPath, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	})

	handler, _ := newTestHandlerComCloudflare(t, cfMux)

	request := httptest.NewRequest(http.MethodPost, "/v1/llm-cf-classifica-mensagem", strings.NewReader(`{"mensagem":"estou sem internet"}`))
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("esperava 502, obteve %d", recorder.Code)
	}
}

func TestClassificaMensagemCloudflare_RegistraMetricaPorBackendEDestino(t *testing.T) {
	cfMux := http.NewServeMux()
	cfMux.HandleFunc(cfRunPath, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{
			"success": true,
			"result":  map[string]any{"response": map[string]string{"destino_principal": "financeiro"}},
		})
	})

	handler, _ := newTestHandlerComCloudflare(t, cfMux)

	request := httptest.NewRequest(http.MethodPost, "/v1/llm-cf-classifica-mensagem", strings.NewReader(`{"mensagem":"quero a segunda via do boleto"}`))
	request.Header.Set("X-API-Key", testAPIKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d: %s", recorder.Code, recorder.Body.String())
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metricsRecorder, metricsRequest)

	expected := `mk_octadesk_llm_classificacao_total{backend="cloudflare",destino="financeiro"} 1`
	if !strings.Contains(metricsRecorder.Body.String(), expected) {
		t.Fatalf("esperava métrica %q em /metrics, não encontrada:\n%s", expected, metricsRecorder.Body.String())
	}
}

func TestClassificaMensagemCloudflare_SemAPIKey(t *testing.T) {
	handler, _ := newTestHandlerComCloudflare(t, nil)

	request := httptest.NewRequest(http.MethodPost, "/v1/llm-cf-classifica-mensagem", strings.NewReader(`{"mensagem":"estou sem internet"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("esperava 401, obteve %d", recorder.Code)
	}
}

func TestHealth_NaoExigeAPIKey(t *testing.T) {
	handler, _ := newTestHandler(t, http.NewServeMux())

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("esperava 200, obteve %d", recorder.Code)
	}
}
