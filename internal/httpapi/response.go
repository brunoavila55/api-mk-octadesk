package httpapi

import (
	"encoding/json"
	"net/http"
)

func writeError(writer http.ResponseWriter, status int, codigo, mensagem string) {
	writeJSONStatus(writer, status, map[string]any{
		"status": "erro",
		"erro":   map[string]string{"codigo": codigo, "mensagem": mensagem},
	})
}

func writeJSON(writer http.ResponseWriter, status int, dados any) {
	writeJSONStatus(writer, status, map[string]any{
		"status": "ok",
		"dados":  dados,
	})
}

func writeJSONStatus(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

// padPlaceholders garante um tamanho mínimo no array de resultado,
// completando com zero-values — comportamento herdado do fluxo antigo em
// Svelte, provavelmente usado pelo flow do Octadesk para referenciar itens
// por índice fixo (response.dados[0], [1], [2]).
func padPlaceholders[T any](items []T, minLength int) []T {
	if items == nil {
		items = []T{}
	}
	for len(items) < minLength {
		var placeholder T
		items = append(items, placeholder)
	}
	return items
}
