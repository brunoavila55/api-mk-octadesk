package mk

import (
	"context"
	"net/http"
	"testing"
)

// TestConsultaDocumento_CodigoPessoaComoNumero é um teste de regressão: o MK de
// produção devolve CodigoPessoa como número JSON (não string) em
// WSMKConsultaDoc.rule, ao contrário do que o código Svelte antigo assumia.
// Confirmado consultando o MK real durante o desenvolvimento.
func TestConsultaDocumento_CodigoPessoaComoNumero(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mk/WSAutenticacao.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]string{"Token": "token-temporario"})
	})
	mux.HandleFunc("/mk/WSMKConsultaDoc.rule", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSONFixture(writer, map[string]any{
			"status":       "OK",
			"Situacao":     "Ativo",
			"CodigoPessoa": 12345, // número, não string — é assim que o MK real responde.
			"Nome":         "Cliente Teste",
			"Endereco":     "Rua Teste, 1",
			"Outros":       []any{},
		})
	})
	mux.HandleFunc("/mk/WSMKConexoesPorCliente.rule", func(writer http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("cd_cliente"); got != "12345" {
			t.Errorf("esperava cd_cliente=12345, obteve %q", got)
		}
		writeJSONFixture(writer, map[string]any{
			"status": "OK",
			"Conexoes": []map[string]string{
				{"codconexao": "999", "endereco": "Rua Teste, 1", "bloqueada": "Não"},
			},
		})
	})

	client := newTestClient(t, mux)

	result, err := client.ConsultaDocumento(context.Background(), "03143955040")
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if result.Tipo != "Cliente" || len(result.Cadastros) != 1 {
		t.Fatalf("resultado inesperado: %+v", result)
	}
	if result.Cadastros[0].CodCliente != "12345" {
		t.Fatalf("esperava CodCliente 12345, obteve %q", result.Cadastros[0].CodCliente)
	}
}
