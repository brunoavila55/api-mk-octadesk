package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"api-mk-octadesk/internal/config"
)

func newTestClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("erro ao montar URL do servidor de teste: %v", err)
	}

	cfg := config.Config{
		OllamaBaseURL:  baseURL,
		OllamaModel:    "atendimento-classificador",
		LLMHTTPTimeout: 2 * time.Second,
	}

	return NewClient(cfg)
}

func writeJSONFixture(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func TestClassifica_Sucesso(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", func(writer http.ResponseWriter, request *http.Request) {
		var body generateRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("corpo inválido: %v", err)
		}
		if body.Model != "atendimento-classificador" {
			t.Errorf("esperava modelo atendimento-classificador, obteve %q", body.Model)
		}
		if body.Prompt != "estou sem internet" {
			t.Errorf("esperava prompt igual à mensagem, obteve %q", body.Prompt)
		}
		writeJSONFixture(writer, generateResponse{Response: `{"destino_principal":"suporte"}`})
	})

	client := newTestClient(t, mux)

	destino, err := client.Classifica(context.Background(), "estou sem internet")
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if destino != "suporte" {
		t.Fatalf("esperava destino suporte, obteve %q", destino)
	}
}

func TestClassifica_DestinoDesconhecido(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, generateResponse{Response: `{"destino_principal":"marketing"}`})
	})

	client := newTestClient(t, mux)

	if _, err := client.Classifica(context.Background(), "qualquer coisa"); err == nil {
		t.Fatal("esperava erro por destino desconhecido")
	}
}

func TestClassifica_RespostaComTextoAoRedorDoJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, generateResponse{Response: "  {\"destino_principal\":\"financeiro\"}  "})
	})

	client := newTestClient(t, mux)

	destino, err := client.Classifica(context.Background(), "quero o boleto")
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if destino != "financeiro" {
		t.Fatalf("esperava destino financeiro, obteve %q", destino)
	}
}

func TestClassifica_RespostaNaoJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, generateResponse{Response: "desculpe, não posso ajudar com isso"})
	})

	client := newTestClient(t, mux)

	if _, err := client.Classifica(context.Background(), "qualquer coisa"); err == nil {
		t.Fatal("esperava erro por resposta que não é JSON válido")
	}
}

func TestClassifica_ErroHTTPDoOllama(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	})

	client := newTestClient(t, mux)

	if _, err := client.Classifica(context.Background(), "qualquer coisa"); err == nil {
		t.Fatal("esperava erro por status HTTP 500 do Ollama")
	}
}

func TestClassifica_Timeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
	})

	client := newTestClient(t, mux)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := client.Classifica(ctx, "qualquer coisa"); err == nil {
		t.Fatal("esperava erro de timeout")
	}
}
