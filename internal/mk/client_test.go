package mk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
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
		MKBaseURL:                   baseURL,
		MKServiceCode:               "9999",
		MKUserAccessToken:           "token-fixo",
		MKWebserviceCounterPassword: "senha-fixa",
		MKHTTPTimeout:               2 * time.Second,
		MKTemporaryAuthTokenTTL:     5 * time.Minute,
	}

	return NewClient(cfg)
}

func writeJSONFixture(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func TestConexoesPorCliente_Sucesso(t *testing.T) {
	var authCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/mk/WSAutenticacao.rule", func(writer http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&authCalls, 1)
		writeJSONFixture(writer, map[string]string{"Token": "token-temporario"})
	})
	mux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("token") != "token-temporario" {
			t.Errorf("esperava token temporário na chamada, obteve %q", request.URL.Query().Get("token"))
		}
		writeJSONFixture(writer, map[string]any{
			"status": "OK",
			"Conexoes": []map[string]string{
				{"codconexao": "123", "endereco": "Rua A, 1", "bloqueada": "N"},
			},
		})
	})

	client := newTestClient(t, mux)

	conexoes, err := client.ConexoesPorCliente(context.Background(), "42")
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if len(conexoes) != 1 || conexoes[0].CodConexao != "123" {
		t.Fatalf("resultado inesperado: %+v", conexoes)
	}

	// Segunda chamada não deve autenticar de novo (token em cache).
	if _, err := client.ConexoesPorCliente(context.Background(), "42"); err != nil {
		t.Fatalf("segunda chamada falhou: %v", err)
	}
	if calls := atomic.LoadInt32(&authCalls); calls != 1 {
		t.Fatalf("esperava 1 chamada de autenticação (token em cache), obteve %d", calls)
	}
}

func TestConexoesPorCliente_NaoEncontrado(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mk/WSAutenticacao.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]string{"Token": "token-temporario"})
	})
	mux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{"status": "ERRO"})
	})

	client := newTestClient(t, mux)

	if _, err := client.ConexoesPorCliente(context.Background(), "42"); err != ErrRegistroNaoEncontrado {
		t.Fatalf("esperava ErrRegistroNaoEncontrado, obteve %v", err)
	}
}

func TestConexoesPorCliente_ErroHTTPDoMK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mk/WSAutenticacao.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]string{"Token": "token-temporario"})
	})
	mux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	})

	client := newTestClient(t, mux)

	if _, err := client.ConexoesPorCliente(context.Background(), "42"); err == nil {
		t.Fatal("esperava erro por status HTTP 500 do MK")
	}
}

func TestConexoesPorCliente_JSONInvalido(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mk/WSAutenticacao.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]string{"Token": "token-temporario"})
	})
	mux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("{ isso não é json"))
	})

	client := newTestClient(t, mux)

	if _, err := client.ConexoesPorCliente(context.Background(), "42"); err == nil {
		t.Fatal("esperava erro por JSON inválido do MK")
	}
}

func TestConexoesPorCliente_Timeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mk/WSAutenticacao.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]string{"Token": "token-temporario"})
	})
	mux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
	})

	client := newTestClient(t, mux)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := client.ConexoesPorCliente(ctx, "42"); err == nil {
		t.Fatal("esperava erro de timeout")
	}
}

// TestConexoesPorCliente_ErroNuncaExpoeURL é um teste de regressão: um erro de
// timeout real contra o MK de produção vazou o token de autenticação no log
// porque o *url.Error nativo do http.Client embute a URL completa (com query
// string) na mensagem de erro.
func TestConexoesPorCliente_ErroNuncaExpoeURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mk/WSAutenticacao.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]string{"Token": "token-secreto-nao-pode-vazar"})
	})
	mux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
	})

	client := newTestClient(t, mux)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.ConexoesPorCliente(ctx, "42")
	if err == nil {
		t.Fatal("esperava erro de timeout")
	}
	if got := err.Error(); containsAny(got, "token-secreto-nao-pode-vazar", "?", "WSMKConexoesPorCliente") {
		t.Fatalf("mensagem de erro vazou a URL/token: %q", got)
	}
}

func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestTokenProvider_TokenFixoIgnoraAutenticacao(t *testing.T) {
	var authCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/mk/WSAutenticacao.rule", func(writer http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&authCalls, 1)
		writeJSONFixture(writer, map[string]string{"Token": "não-deveria-ser-usado"})
	})
	mux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("token") != "token-fixo-de-teste" {
			t.Errorf("esperava token fixo, obteve %q", request.URL.Query().Get("token"))
		}
		writeJSONFixture(writer, map[string]any{"status": "OK", "Conexoes": []map[string]string{}})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	baseURL, _ := url.Parse(server.URL)

	cfg := config.Config{
		MKBaseURL:               baseURL,
		MKServiceCode:           "9999",
		MKTemporaryAuthToken:    "token-fixo-de-teste",
		MKHTTPTimeout:           2 * time.Second,
		MKTemporaryAuthTokenTTL: 5 * time.Minute,
	}

	client := NewClient(cfg)
	if _, err := client.ConexoesPorCliente(context.Background(), "42"); err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if calls := atomic.LoadInt32(&authCalls); calls != 0 {
		t.Fatalf("esperava 0 chamadas de autenticação com token fixo, obteve %d", calls)
	}
}
