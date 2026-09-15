package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"api-mk-octadesk/internal/config"
)

const cfTestRunPath = "/accounts/conta-teste/ai/run/@cf/meta/llama-3.1-8b-instruct-fp8-fast"

func writeJSONFixture(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func newTestCloudflareClient(t *testing.T, mux *http.ServeMux) *CloudflareClient {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("erro ao montar URL do servidor de teste: %v", err)
	}

	cfg := config.Config{
		CloudflareAIBaseURL:   baseURL,
		CloudflareAccountID:   "conta-teste",
		CloudflareAPIToken:    "token-teste",
		CloudflareAIModel:     "@cf/meta/llama-3.1-8b-instruct-fp8-fast",
		CloudflareHTTPTimeout: 2 * time.Second,
	}

	return NewCloudflareClient(cfg)
}

func TestCloudflareClassifica_Sucesso(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfTestRunPath, func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer token-teste" {
			t.Errorf("esperava header Authorization Bearer token-teste, obteve %q", got)
		}

		var body cfRunRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("corpo inválido: %v", err)
		}
		if len(body.Messages) == 0 || body.Messages[len(body.Messages)-1].Content != "estou sem internet" {
			t.Errorf("esperava última mensagem igual à mensagem do cliente, obteve %+v", body.Messages)
		}
		if body.ResponseFormat.Type != "json_schema" {
			t.Errorf("esperava response_format json_schema, obteve %q", body.ResponseFormat.Type)
		}

		writeJSONFixture(writer, cfRunResponse{
			Success: true,
			Result: struct {
				Response json.RawMessage `json:"response"`
			}{Response: json.RawMessage(`{"destino_principal":"suporte"}`)},
		})
	})

	client := newTestCloudflareClient(t, mux)

	destino, err := client.Classifica(context.Background(), "estou sem internet")
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if destino != "suporte" {
		t.Fatalf("esperava destino suporte, obteve %q", destino)
	}
}

func TestCloudflareClassifica_NaoConfigurado(t *testing.T) {
	baseURL, err := url.Parse("https://exemplo-nao-usado.invalid/")
	if err != nil {
		t.Fatalf("erro ao montar URL de teste: %v", err)
	}

	client := NewCloudflareClient(config.Config{
		CloudflareAIBaseURL:   baseURL,
		CloudflareAIModel:     "@cf/meta/llama-3.1-8b-instruct-fp8-fast",
		CloudflareHTTPTimeout: 2 * time.Second,
		// CloudflareAccountID e CloudflareAPIToken deliberadamente vazios.
	})

	if _, err := client.Classifica(context.Background(), "qualquer coisa"); !errors.Is(err, ErrNaoConfigurado) {
		t.Fatalf("esperava ErrNaoConfigurado, obteve %v", err)
	}
}

func TestCloudflareClassifica_FalhaReportadaPelaCloudflare(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfTestRunPath, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, cfRunResponse{
			Success: false,
			Errors:  []cfError{{Code: 7003, Message: "credenciais inválidas"}},
		})
	})

	client := newTestCloudflareClient(t, mux)

	if _, err := client.Classifica(context.Background(), "qualquer coisa"); err == nil {
		t.Fatal("esperava erro quando a Cloudflare reporta success=false")
	}
}

func TestCloudflareClassifica_DestinoDesconhecido(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfTestRunPath, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, cfRunResponse{
			Success: true,
			Result: struct {
				Response json.RawMessage `json:"response"`
			}{Response: json.RawMessage(`{"destino_principal":"marketing"}`)},
		})
	})

	client := newTestCloudflareClient(t, mux)

	if _, err := client.Classifica(context.Background(), "qualquer coisa"); err == nil {
		t.Fatal("esperava erro por destino desconhecido")
	}
}

// TestCloudflareClassifica_RespostaFormatoInesperado cobre o caso em que
// result.response, embora seja JSON válido (o Cloudflare sempre devolve JSON
// válido em JSON Mode), não é o objeto {"destino_principal": "..."} esperado
// — ex.: um array, por alguma mudança de comportamento do modelo/API.
func TestCloudflareClassifica_RespostaFormatoInesperado(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfTestRunPath, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, cfRunResponse{
			Success: true,
			Result: struct {
				Response json.RawMessage `json:"response"`
			}{Response: json.RawMessage(`[]`)},
		})
	})

	client := newTestCloudflareClient(t, mux)

	if _, err := client.Classifica(context.Background(), "qualquer coisa"); err == nil {
		t.Fatal("esperava erro por formato de resposta inesperado")
	}
}

func TestCloudflareClassifica_ErroHTTPDaCloudflare(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfTestRunPath, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	})

	client := newTestCloudflareClient(t, mux)

	if _, err := client.Classifica(context.Background(), "qualquer coisa"); err == nil {
		t.Fatal("esperava erro por status HTTP 500 da Cloudflare")
	}
}

func TestCloudflareClassifica_Timeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(cfTestRunPath, func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
	})

	client := newTestCloudflareClient(t, mux)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := client.Classifica(ctx, "qualquer coisa"); err == nil {
		t.Fatal("esperava erro de timeout")
	}
}
