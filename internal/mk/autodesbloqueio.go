package mk

import (
	"context"
	"net/url"
	"strings"
)

// AutoDesbloqueio solicita ao MK o desbloqueio automático de uma conexão
// bloqueada por falta de pagamento. O próprio MK limita essa ação a uma vez
// por mês por conexão. Ver AutoDesbloqueioResult para os shapes confirmados.
func (client *Client) AutoDesbloqueio(ctx context.Context, cdConexao string) (AutoDesbloqueioResult, error) {
	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return AutoDesbloqueioResult{}, err
	}

	query := url.Values{}
	query.Set("sys", "MK0")
	query.Set("token", token)
	query.Set("cd_conexao", cdConexao)

	var parsed AutoDesbloqueioResult
	if err := client.get(ctx, "/mk/WSMKAutoDesbloqueio.rule", query, &parsed); err != nil {
		return AutoDesbloqueioResult{}, err
	}

	return parsed, nil
}

// ClassificacaoAutodesbloqueio identifica o desfecho de negócio de uma
// chamada de autodesbloqueio, para observabilidade — em especial para medir
// quantos clientes tentam repetir o desbloqueio já no mesmo mês.
type ClassificacaoAutodesbloqueio string

const (
	AutodesbloqueioSucesso      ClassificacaoAutodesbloqueio = "sucesso"
	AutodesbloqueioNaoBloqueada ClassificacaoAutodesbloqueio = "nao_bloqueada"
	AutodesbloqueioLimiteMensal ClassificacaoAutodesbloqueio = "limite_mensal_atingido"
	AutodesbloqueioDesconhecida ClassificacaoAutodesbloqueio = "desconhecida"
)

// Classificar diferencia os dois motivos de recusa do MK pelo texto de
// Mensagem, já que o Status sozinho não distingue ("ERRO" cobre os dois).
// Confirmado contra produção em 2026-09-12 — ver comentário em
// AutoDesbloqueioResult.
func (result AutoDesbloqueioResult) Classificar() ClassificacaoAutodesbloqueio {
	switch {
	case result.Status == "OK":
		return AutodesbloqueioSucesso
	case strings.Contains(result.Mensagem, "incompatível"):
		return AutodesbloqueioNaoBloqueada
	case strings.Contains(result.Mensagem, "indisponível"):
		return AutodesbloqueioLimiteMensal
	default:
		return AutodesbloqueioDesconhecida
	}
}
