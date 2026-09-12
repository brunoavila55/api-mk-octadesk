// Package mk implementa o cliente HTTP para o ERP MK usado pelas integrações
// do chatbot. Nenhuma chamada externa deve acontecer fora deste pacote.
package mk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"api-mk-octadesk/internal/config"
)

const maxResponseBytes = 2 << 20 // 2 MiB — resposta do MK nunca deveria passar disso.

// requestError substitui o *url.Error nativo do http.Client, que embute a URL
// completa da requisição — incluindo query string com token, senha ou
// documento — na mensagem de erro. Confirmado em teste real: um timeout
// vazou o token do MK no log. requestError preserva a classificação de
// timeout (via Timeout()) e a cadeia de errors.Is/As (via Unwrap()), mas
// nunca expõe a URL.
type requestError struct {
	op    string
	inner error
}

func (e *requestError) Error() string {
	return fmt.Sprintf("%s ao MK: %v", e.op, e.inner)
}

func (e *requestError) Unwrap() error { return e.inner }

func (e *requestError) Timeout() bool {
	var netErr interface{ Timeout() bool }
	return errors.As(e.inner, &netErr) && netErr.Timeout()
}

// sanitizeRequestError remove a URL (e, com ela, qualquer segredo na query
// string) de um erro retornado por http.Client.Do.
func sanitizeRequestError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return &requestError{op: urlErr.Op, inner: urlErr.Err}
	}
	return err
}

type Client struct {
	baseURL       *url.URL
	httpClient    *http.Client
	tokenProvider *tokenProvider
}

func NewClient(cfg config.Config) *Client {
	httpClient := &http.Client{Timeout: cfg.MKHTTPTimeout}

	return &Client{
		baseURL:    cfg.MKBaseURL,
		httpClient: httpClient,
		tokenProvider: &tokenProvider{
			httpClient:                httpClient,
			baseURL:                   cfg.MKBaseURL,
			serviceCode:               cfg.MKServiceCode,
			userAccessToken:           cfg.MKUserAccessToken,
			webserviceCounterPassword: cfg.MKWebserviceCounterPassword,
			fixedToken:                cfg.MKTemporaryAuthToken,
			ttl:                       cfg.MKTemporaryAuthTokenTTL,
		},
	}
}

// get executa uma requisição GET autenticada contra o MK e devolve o corpo
// decodificado em target. path deve começar com "/".
func (client *Client) get(ctx context.Context, path string, query url.Values, target any) error {
	reqURL := client.baseURL.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()})

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return fmt.Errorf("montar requisição ao MK: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return sanitizeRequestError(err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("ler resposta do MK: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("MK retornou status HTTP %d", response.StatusCode)
	}

	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decodificar resposta do MK: %w", err)
	}

	return nil
}

// tokenProvider mantém em cache o token temporário do MK, evitando uma
// autenticação nova a cada consulta.
type tokenProvider struct {
	httpClient *http.Client
	baseURL    *url.URL

	serviceCode               string
	userAccessToken           string
	webserviceCounterPassword string
	fixedToken                string
	ttl                       time.Duration

	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time
}

type authResponse struct {
	Token string `json:"Token"`
}

func (provider *tokenProvider) Token(ctx context.Context) (string, error) {
	if provider.fixedToken != "" {
		return provider.fixedToken, nil
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()

	if provider.cachedToken != "" && time.Now().Before(provider.expiresAt) {
		return provider.cachedToken, nil
	}

	values := url.Values{}
	values.Set("sys", "MK0")
	values.Set("token", provider.userAccessToken)
	values.Set("password", provider.webserviceCounterPassword)
	values.Set("cd_servico", provider.serviceCode)

	reqURL := provider.baseURL.ResolveReference(&url.URL{Path: "/mk/WSAutenticacao.rule", RawQuery: values.Encode()})

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("montar requisição de autenticação ao MK: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := provider.httpClient.Do(request)
	if err != nil {
		return "", sanitizeRequestError(err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("ler resposta de autenticação do MK: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("MK recusou autenticação com status HTTP %d", response.StatusCode)
	}

	var parsed authResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decodificar resposta de autenticação do MK: %w", err)
	}
	if strings.TrimSpace(parsed.Token) == "" {
		return "", fmt.Errorf("MK não retornou token de autenticação")
	}

	provider.cachedToken = parsed.Token
	provider.expiresAt = time.Now().Add(provider.ttl)

	return provider.cachedToken, nil
}
