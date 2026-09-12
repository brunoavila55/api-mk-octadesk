package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"api-mk-octadesk/internal/document"
	"api-mk-octadesk/internal/llm"
	"api-mk-octadesk/internal/mk"
)

const minPlaceholderItems = 3

// maxMensagemBodyBytes limita o corpo lido antes mesmo de decodificar o JSON,
// para que um cliente não consiga forçar a API a ler um corpo arbitrariamente
// grande antes da validação de tamanho da mensagem em si.
const maxMensagemBodyBytes = 8 * 1024

// maxMensagemRunes é o tamanho máximo aceito para a mensagem do cliente
// enviada à LLM — suficiente para qualquer mensagem real de chat, sem abrir
// espaço para prompts artificialmente grandes.
const maxMensagemRunes = 2000

type handlers struct {
	client    *mk.Client
	llmClient *llm.Client
	logger    *slog.Logger
	metrics   *apiMetrics
}

func (h *handlers) consultaDocumento(writer http.ResponseWriter, request *http.Request) {
	const route = "/v1/consulta-documento"

	documento, err := document.NormalizeDocument(request.URL.Query().Get("documento"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "documento_invalido", "Informe um CPF ou CNPJ válido.")
		return
	}

	result, err := h.client.ConsultaDocumento(request.Context(), documento)
	if err != nil {
		h.handleMKError(writer, route, err)
		return
	}

	result.Cadastros = padPlaceholders(result.Cadastros, minPlaceholderItems)
	writeJSON(writer, http.StatusOK, result)
}

func (h *handlers) consultaConexao(writer http.ResponseWriter, request *http.Request) {
	const route = "/v1/consulta-conexao"

	cdCliente, err := requireNumericParam(request, "cd_cliente")
	if err != nil {
		writeError(writer, http.StatusBadRequest, "cd_cliente_invalido", "Informe um cd_cliente válido.")
		return
	}

	conexoes, err := h.client.ConexoesPorCliente(request.Context(), cdCliente)
	if err != nil {
		h.handleMKError(writer, route, err)
		return
	}

	writeJSON(writer, http.StatusOK, padPlaceholders(conexoes, minPlaceholderItems))
}

func (h *handlers) consultaNotificacaoAtiva(writer http.ResponseWriter, request *http.Request) {
	const route = "/v1/consulta-notificacao-ativa"

	ativa, err := h.client.NotificacaoAtiva(request.Context())
	if err != nil {
		h.handleMKError(writer, route, err)
		return
	}

	if ativa {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "Incidente ativo"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "OK"})
}

func (h *handlers) consultaNotificaCliente(writer http.ResponseWriter, request *http.Request) {
	const route = "/v1/consulta-notifica-cliente"

	cdConexao, err := requireNumericParam(request, "cd_conexao")
	if err != nil {
		writeError(writer, http.StatusBadRequest, "cd_conexao_invalido", "Informe um cd_conexao válido.")
		return
	}

	afetado, err := h.client.NotificaCliente(request.Context(), cdConexao)
	if err != nil {
		h.handleMKError(writer, route, err)
		return
	}

	if afetado {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "Incidente ativo"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "OK"})
}

func (h *handlers) geraBoleto(writer http.ResponseWriter, request *http.Request) {
	const route = "/v1/gera-boleto"

	cdCliente, err := requireNumericParam(request, "cd_cliente")
	if err != nil {
		writeError(writer, http.StatusBadRequest, "cd_cliente_invalido", "Informe um cd_cliente válido.")
		return
	}

	boletos, err := h.client.GeraBoleto(request.Context(), cdCliente)
	if err != nil {
		h.handleMKError(writer, route, err)
		return
	}

	writeJSON(writer, http.StatusOK, padPlaceholders(boletos, minPlaceholderItems))
}

func (h *handlers) geraPix(writer http.ResponseWriter, request *http.Request) {
	const route = "/v1/gera-pix"

	cdCliente, err := requireNumericParam(request, "cd_cliente")
	if err != nil {
		writeError(writer, http.StatusBadRequest, "cd_cliente_invalido", "Informe um cd_cliente válido.")
		return
	}

	pix, err := h.client.GeraPix(request.Context(), cdCliente)
	if err != nil {
		h.handleMKError(writer, route, err)
		return
	}

	writeJSON(writer, http.StatusOK, padPlaceholders(pix, minPlaceholderItems))
}

func (h *handlers) autoDesbloqueio(writer http.ResponseWriter, request *http.Request) {
	const route = "/v1/autodesbloqueio"

	cdConexao, err := requireNumericParam(request, "cd_conexao")
	if err != nil {
		writeError(writer, http.StatusBadRequest, "cd_conexao_invalido", "Informe um cd_conexao válido.")
		return
	}

	result, err := h.client.AutoDesbloqueio(request.Context(), cdConexao)
	if err != nil {
		h.handleMKError(writer, route, err)
		return
	}

	classificacao := result.Classificar()
	h.metrics.recordAutodesbloqueio(string(classificacao))
	h.logger.Info("autodesbloqueio solicitado", "cd_conexao", cdConexao, "status", result.Status, "classificacao", classificacao)
	writeJSON(writer, http.StatusOK, result)
}

type classificaMensagemRequest struct {
	Mensagem string `json:"mensagem"`
}

// classificaMensagem manda a mensagem do cliente para a LLM (via Ollama) e
// devolve o setor de destino, que o próprio flow do Octadesk usa num
// if/else para tagear a conversa. Esta rota nunca chama o MK.
func (h *handlers) classificaMensagem(writer http.ResponseWriter, request *http.Request) {
	const route = "/v1/llm-classifica-mensagem"

	var body classificaMensagemRequest
	decoder := json.NewDecoder(io.LimitReader(request.Body, maxMensagemBodyBytes))
	if err := decoder.Decode(&body); err != nil {
		writeError(writer, http.StatusBadRequest, "corpo_invalido", `Informe um corpo JSON válido com o campo "mensagem".`)
		return
	}

	mensagem := strings.TrimSpace(body.Mensagem)
	if mensagem == "" || utf8.RuneCountInString(mensagem) > maxMensagemRunes {
		writeError(writer, http.StatusBadRequest, "mensagem_invalida", "Informe uma mensagem não vazia de até 2000 caracteres.")
		return
	}

	destino, err := h.llmClient.Classifica(request.Context(), mensagem)
	if err != nil {
		h.handleLLMError(writer, route, err)
		return
	}

	h.metrics.recordClassificacao(destino)
	h.logger.Info("mensagem classificada", "destino", destino)
	writeJSON(writer, http.StatusOK, map[string]string{"destino": destino})
}

// handleLLMError traduz erros do cliente do Ollama para a resposta HTTP
// pública. Nunca loga a mensagem do cliente, só o erro técnico e o destino
// (quando houver).
func (h *handlers) handleLLMError(writer http.ResponseWriter, route string, err error) {
	h.logger.Error("falha ao classificar mensagem via LLM", "route", route, "error", err)

	var networkErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkErr) && networkErr.Timeout()):
		h.metrics.recordLLMErro("llm_timeout")
		writeError(writer, http.StatusGatewayTimeout, "llm_timeout", "A classificação demorou demais para responder.")
	case errors.Is(err, llm.ErrRespostaInvalida):
		h.metrics.recordLLMErro("llm_resposta_invalida")
		writeError(writer, http.StatusBadGateway, "llm_resposta_invalida", "A classificação não pôde ser interpretada.")
	default:
		h.metrics.recordLLMErro("llm_indisponivel")
		writeError(writer, http.StatusBadGateway, "llm_indisponivel", "Não foi possível classificar a mensagem.")
	}
}

// requireNumericParam valida que o parâmetro existe e contém apenas dígitos —
// os códigos internos do MK (cd_cliente, cd_conexao) nunca têm outro formato.
func requireNumericParam(request *http.Request, name string) (string, error) {
	value := request.URL.Query().Get(name)
	if value == "" {
		return "", errParametroInvalido
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return "", errParametroInvalido
		}
	}
	return value, nil
}

var errParametroInvalido = errors.New("parâmetro inválido")

// handleMKError traduz erros do cliente MK para a resposta HTTP pública,
// sem nunca vazar detalhes internos (URL, token, stack trace) ao chatbot.
func (h *handlers) handleMKError(writer http.ResponseWriter, route string, err error) {
	h.logger.Error("falha ao consultar o MK", "route", route, "error", err)

	var networkErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkErr) && networkErr.Timeout()):
		h.metrics.recordMKError(route, "mk_timeout")
		writeError(writer, http.StatusGatewayTimeout, "mk_timeout", "O ERP demorou demais para responder.")
	case errors.Is(err, mk.ErrRegistroNaoEncontrado):
		h.metrics.recordMKError(route, "nao_encontrado")
		writeError(writer, http.StatusNotFound, "nao_encontrado", "Nenhum registro encontrado para os dados informados.")
	default:
		h.metrics.recordMKError(route, "mk_indisponivel")
		writeError(writer, http.StatusBadGateway, "mk_indisponivel", "Não foi possível consultar o ERP.")
	}
}
