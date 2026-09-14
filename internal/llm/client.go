// Package llm implementa o cliente HTTP para o Ollama, usado para classificar
// a intenção de mensagens do cliente e decidir o setor de destino no
// Octadesk. Nenhuma chamada ao MK acontece a partir deste pacote.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"api-mk-octadesk/internal/config"
)

const maxResponseBytes = 1 << 20 // 1 MiB — resposta do Ollama nunca deveria passar disso.

// destinosValidos são os únicos setores que o Octadesk sabe rotear.
var destinosValidos = map[string]bool{
	"vendas":        true,
	"renovacao":     true,
	"ampliacao":     true,
	"trocaendereco": true,
	"trocatitular":  true,
	"cancelamento":  true,
	"financeiro":    true,
	"suporte":       true,
	"atendimento":   true,
}

// ErrRespostaInvalida indica que o Ollama respondeu, mas o conteúdo não pôde
// ser interpretado como uma classificação válida (JSON malformado ou destino
// fora do conjunto conhecido).
var ErrRespostaInvalida = errors.New("ollama retornou uma resposta que não pôde ser interpretada")

type Client struct {
	baseURL    *url.URL
	model      string
	httpClient *http.Client
}

func NewClient(cfg config.Config) *Client {
	return &Client{
		baseURL:    cfg.OllamaBaseURL,
		model:      cfg.OllamaModel,
		httpClient: &http.Client{Timeout: cfg.LLMHTTPTimeout},
	}
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Format string `json:"format"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
}

type classificacao struct {
	DestinoPrincipal string `json:"destino_principal"`
}

// Classifica envia a mensagem do cliente ao modelo classificador (construído
// a partir do Modelfile em ollama/Modelfile) e devolve o setor de destino:
// "vendas", "renovacao", "ampliacao", "trocaendereco", "trocatitular",
// "cancelamento", "financeiro", "suporte" ou "atendimento".
func (client *Client) Classifica(ctx context.Context, mensagem string) (string, error) {
	reqBody, err := json.Marshal(generateRequest{
		Model:  client.model,
		Prompt: mensagem,
		Format: "json",
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("montar requisição ao Ollama: %w", err)
	}

	reqURL := client.baseURL.ResolveReference(&url.URL{Path: "/api/generate"})

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("montar requisição ao Ollama: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("ler resposta do Ollama: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("ollama retornou status HTTP %d", response.StatusCode)
	}

	var parsed generateResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decodificar resposta do Ollama: %w", err)
	}

	var result classificacao
	if err := json.Unmarshal([]byte(strings.TrimSpace(parsed.Response)), &result); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRespostaInvalida, err)
	}

	destino := strings.ToLower(strings.TrimSpace(result.DestinoPrincipal))
	if !destinosValidos[destino] {
		return "", fmt.Errorf("%w: destino %q não reconhecido", ErrRespostaInvalida, destino)
	}

	return destino, nil
}
