package mk

import (
	"context"
	"net/url"
)

// AutoDesbloqueio solicita ao MK o desbloqueio automático de uma conexão
// bloqueada por falta de pagamento. O próprio MK limita essa ação a uma vez
// por mês por conexão.
//
// ATENÇÃO: a classificação abaixo (Status == "OK") é a melhor suposição a
// partir do padrão usado nas demais rotas do MK — o código antigo em Svelte
// nunca validava esta resposta, só repassava o JSON cru. Antes de liberar
// esta rota em produção, confirme no Insomnia os retornos reais de: sucesso,
// já desbloqueado este mês, e ainda bloqueado por falta de pagamento; ajuste
// este parsing e a resposta pública em internal/httpapi/handlers.go de acordo.
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
