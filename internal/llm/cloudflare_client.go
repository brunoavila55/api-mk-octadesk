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

// CloudflareClient implementa o cliente HTTP para o Cloudflare Workers AI,
// usado para classificar a intenção de mensagens do cliente e decidir o
// setor de destino no Octadesk. Nenhuma chamada ao MK acontece a partir
// deste arquivo.
//
// Convive em paralelo com o Client (Ollama) enquanto a classificação é
// validada em produção via a rota /v1/llm-cf-classifica-mensagem — ver
// internal/httpapi/handlers.go e README.md.
type CloudflareClient struct {
	baseURL    *url.URL
	accountID  string
	apiToken   string
	model      string
	httpClient *http.Client
}

func NewCloudflareClient(cfg config.Config) *CloudflareClient {
	return &CloudflareClient{
		baseURL:    cfg.CloudflareAIBaseURL,
		accountID:  cfg.CloudflareAccountID,
		apiToken:   cfg.CloudflareAPIToken,
		model:      cfg.CloudflareAIModel,
		httpClient: &http.Client{Timeout: cfg.CloudflareHTTPTimeout},
	}
}

type cfMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type cfJSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
}

type cfResponseFormat struct {
	Type       string       `json:"type"`
	JSONSchema cfJSONSchema `json:"json_schema"`
}

type cfRunRequest struct {
	Messages       []cfMessage      `json:"messages"`
	ResponseFormat cfResponseFormat `json:"response_format"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfRunResponse struct {
	Success bool `json:"success"`
	Result  struct {
		// Response vem como objeto JSON direto (não como string contendo
		// JSON) quando response_format é json_schema — confirmado testando
		// contra a API real, diferente do que a documentação (inconclusiva
		// nesse ponto) sugeria. json.RawMessage deixa o Unmarshal final
		// (em Classifica) decodificar direto pra classificacao.
		Response json.RawMessage `json:"response"`
	} `json:"result"`
	Errors []cfError `json:"errors"`
}

// classificacaoJSONSchema restringe o campo destino_principal aos 9 setores
// conhecidos, via JSON Mode nativo da Cloudflare — evita o parsing na marra
// (extrair o primeiro "{...}" do texto) que uma tentativa anterior precisou
// para lidar com modelos "reasoning" que vazam texto antes do JSON.
var classificacaoJSONSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"destino_principal": map[string]any{
			"type": "string",
			"enum": destinosValidosOrdenados,
		},
	},
	"required":             []string{"destino_principal"},
	"additionalProperties": false,
}

var destinosValidosOrdenados = []string{
	"vendas", "renovacao", "ampliacao", "endereco", "titular",
	"cancelamento", "financeiro", "suporte", "atendimento",
}

// cfSystemPrompt é a mesma política de classificação do ollama/Modelfile,
// adaptada para mensagens de chat em vez de Modelfile — qualquer mudança de
// regra deve ser replicada nos dois lugares enquanto ambos os backends
// estiverem em uso. Ver llm_expand.md, seção 4.
const cfSystemPrompt = `Você classifica UMA mensagem de cliente de um provedor de internet.
Use somente a mensagem recebida. Não converse, não responda dúvidas e não invente contexto.

Responda exatamente:
{"destino_principal":"<destino>"}

Destinos:
- suporte: falha técnica; sem internet; caiu; lenta/ruim/oscilando; modem, roteador, sinal, luz vermelha, Wi-Fi sem conexão.
- cancelamento: cancelar ou encerrar contrato/serviço; não quer mais o serviço.
- financeiro: boleto, 2ª via, fatura, pagamento, cobrança, PIX, mensalidade, vencimento, dívida, débito, comprovante, código de barras, negociação. "contrato" sozinho = financeiro.
- titular: mudar titular, dono ou responsável pela conta; contrato no nome de outra pessoa.
- renovacao: renovar contrato; contrato vencendo/vencido; fim de fidelidade; continuar com o mesmo plano.
- ampliacao: aumentar velocidade; upgrade do plano atual; roteador, ponto, repetidor ou mesh adicional.
- endereco: mudar ou transferir serviço/instalação existente para outro endereço ou ponto (só quando fica claro que já é cliente com serviço ativo).
- vendas: novo contrato/instalação; planos/preços para contratar; cobertura em endereço novo; um endereço ou bairro sozinho, sem mais contexto (resposta típica à pergunta "qual o seu endereço?", feita a quem está pedindo cobertura/instalação nova).
- atendimento: saudação, agradecimento, pedido genérico, fragmento ou informação insuficiente.

Regras:
- internet lenta sem pedido explícito de upgrade = suporte.
- novo serviço em outro endereço = vendas.
- mover serviço já existente = endereco.
- endereço ou bairro sozinho, sem mais contexto = vendas.
- "contrato" sozinho = financeiro.
- na dúvida entre um destino específico e atendimento = atendimento.

Se houver várias intenções, prioridade:
suporte > cancelamento > financeiro > titular > renovacao > ampliacao > endereco > vendas > atendimento.

Valores permitidos:
vendas, renovacao, ampliacao, endereco, titular, cancelamento, financeiro, suporte, atendimento.`

// cfExemplos são os mesmos pares few-shot do ollama/Modelfile.
var cfExemplos = []cfMessage{
	{Role: "user", Content: "estou sem internet e também preciso do boleto que vence amanhã"},
	{Role: "assistant", Content: `{"destino_principal":"suporte"}`},
	{Role: "user", Content: "quero a segunda via do boleto"},
	{Role: "assistant", Content: `{"destino_principal":"financeiro"}`},
	{Role: "user", Content: "contrato"},
	{Role: "assistant", Content: `{"destino_principal":"financeiro"}`},
	{Role: "user", Content: "a net aqui morreu ontem"},
	{Role: "assistant", Content: `{"destino_principal":"suporte"}`},
	{Role: "user", Content: "moro perto da rodoviária, quero contratar internet"},
	{Role: "assistant", Content: `{"destino_principal":"vendas"}`},
	{Role: "user", Content: "Rua General Neto 123, vocês têm cobertura aí?"},
	{Role: "assistant", Content: `{"destino_principal":"vendas"}`},
	{Role: "user", Content: "queria saber os planos para instalar internet lá em casa"},
	{Role: "assistant", Content: `{"destino_principal":"vendas"}`},
	{Role: "user", Content: "quero mudar meu endereço, vou me mudar para o centro"},
	{Role: "assistant", Content: `{"destino_principal":"endereco"}`},
	{Role: "user", Content: "trocar o ponto"},
	{Role: "assistant", Content: `{"destino_principal":"endereco"}`},
	{Role: "user", Content: "dá pra transferir minha internet pra outro endereço?"},
	{Role: "assistant", Content: `{"destino_principal":"endereco"}`},
	{Role: "user", Content: "quero trocar o titular"},
	{Role: "assistant", Content: `{"destino_principal":"titular"}`},
	{Role: "user", Content: "trocar o dono da conta"},
	{Role: "assistant", Content: `{"destino_principal":"titular"}`},
	{Role: "user", Content: "quero colocar o contrato no nome da minha esposa"},
	{Role: "assistant", Content: `{"destino_principal":"titular"}`},
	{Role: "user", Content: "mudei de endereço e minha internet também parou"},
	{Role: "assistant", Content: `{"destino_principal":"suporte"}`},
	{Role: "user", Content: "bom dia"},
	{Role: "assistant", Content: `{"destino_principal":"atendimento"}`},
	{Role: "user", Content: "centro"},
	{Role: "assistant", Content: `{"destino_principal":"vendas"}`},
	{Role: "user", Content: "vila block sao sepe"},
	{Role: "assistant", Content: `{"destino_principal":"vendas"}`},
	{Role: "user", Content: "minha internet está muito lenta desde ontem"},
	{Role: "assistant", Content: `{"destino_principal":"suporte"}`},
	{Role: "user", Content: "quero renovar o contrato"},
	{Role: "assistant", Content: `{"destino_principal":"renovacao"}`},
	{Role: "user", Content: "meu contrato está vencendo, como faço pra renovar?"},
	{Role: "assistant", Content: `{"destino_principal":"renovacao"}`},
	{Role: "user", Content: "quero aumentar a velocidade"},
	{Role: "assistant", Content: `{"destino_principal":"ampliacao"}`},
	{Role: "user", Content: "aumentar plano"},
	{Role: "assistant", Content: `{"destino_principal":"ampliacao"}`},
	{Role: "user", Content: "mais um roteador"},
	{Role: "assistant", Content: `{"destino_principal":"ampliacao"}`},
	{Role: "user", Content: "quero cancelar"},
	{Role: "assistant", Content: `{"destino_principal":"cancelamento"}`},
	{Role: "user", Content: "cancelar contrato"},
	{Role: "assistant", Content: `{"destino_principal":"cancelamento"}`},
	{Role: "user", Content: "não quero mais o serviço, pode encerrar"},
	{Role: "assistant", Content: `{"destino_principal":"cancelamento"}`},
	{Role: "user", Content: "queria cancelar, mas antes preciso saber se tem fatura em aberto"},
	{Role: "assistant", Content: `{"destino_principal":"cancelamento"}`},
}

func buildCfMessages(mensagem string) []cfMessage {
	messages := make([]cfMessage, 0, len(cfExemplos)+2)
	messages = append(messages, cfMessage{Role: "system", Content: cfSystemPrompt})
	messages = append(messages, cfExemplos...)
	messages = append(messages, cfMessage{Role: "user", Content: mensagem})
	return messages
}

// ErrNaoConfigurado indica que CLOUDFLARE_ACCOUNT_ID ou CLOUDFLARE_API_TOKEN
// não foram definidos. Deliberadamente não é um erro fatal de config.Load —
// ver o comentário lá — então a rota /v1/llm-cf-classifica-mensagem devolve
// esse erro (502 llm_indisponivel) até alguém configurar as credenciais,
// sem afetar a rota do Ollama nem o resto da API.
var ErrNaoConfigurado = errors.New("cloudflare workers ai não configurado: defina CLOUDFLARE_ACCOUNT_ID e CLOUDFLARE_API_TOKEN")

// Classifica envia a mensagem do cliente ao Cloudflare Workers AI e devolve
// o setor de destino, nos mesmos termos do Client (Ollama) — ver o
// comentário de Client.Classifica.
func (client *CloudflareClient) Classifica(ctx context.Context, mensagem string) (string, error) {
	if client.accountID == "" || client.apiToken == "" {
		return "", ErrNaoConfigurado
	}

	reqBody, err := json.Marshal(cfRunRequest{
		Messages: buildCfMessages(mensagem),
		ResponseFormat: cfResponseFormat{
			Type:       "json_schema",
			JSONSchema: cfJSONSchema{Name: "classificacao", Schema: classificacaoJSONSchema},
		},
	})
	if err != nil {
		return "", fmt.Errorf("montar requisição à Cloudflare: %w", err)
	}

	reqURL := client.baseURL.ResolveReference(&url.URL{Path: "accounts/" + client.accountID + "/ai/run/" + client.model})

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("montar requisição à Cloudflare: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+client.apiToken)

	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("ler resposta da Cloudflare: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("cloudflare retornou status HTTP %d", response.StatusCode)
	}

	var parsed cfRunResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decodificar resposta da Cloudflare: %w", err)
	}

	if !parsed.Success {
		return "", fmt.Errorf("cloudflare reportou falha: %s", formatCfErrors(parsed.Errors))
	}

	var result classificacao
	if err := json.Unmarshal(parsed.Result.Response, &result); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRespostaInvalida, err)
	}

	destino := strings.ToLower(strings.TrimSpace(result.DestinoPrincipal))
	if !destinosValidos[destino] {
		return "", fmt.Errorf("%w: destino %q não reconhecido", ErrRespostaInvalida, destino)
	}

	return destino, nil
}

func formatCfErrors(cfErrors []cfError) string {
	if len(cfErrors) == 0 {
		return "sem detalhes"
	}
	parts := make([]string, len(cfErrors))
	for i, e := range cfErrors {
		parts[i] = fmt.Sprintf("[%d] %s", e.Code, e.Message)
	}
	return strings.Join(parts, "; ")
}
