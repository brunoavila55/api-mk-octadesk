package mk

import (
	"context"
	"net/url"
)

// notificacoesAtivas retorna os códigos de notificação de parada ativos no MK.
func (client *Client) notificacoesAtivas(ctx context.Context) ([]notificacaoAtivaRaw, error) {
	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("token", token)
	query.Set("status", "A")

	var parsed []notificacaoAtivaRaw
	if err := client.get(ctx, "/core-api/notificacoes/status", query, &parsed); err != nil {
		return nil, err
	}

	return parsed, nil
}

// NotificacaoAtiva indica se existe qualquer notificação de parada ativa no momento.
func (client *Client) NotificacaoAtiva(ctx context.Context) (bool, error) {
	ativas, err := client.notificacoesAtivas(ctx)
	if err != nil {
		return false, err
	}
	return len(ativas) > 0, nil
}

// NotificaCliente indica se a conexão informada está na lista de afetados de
// alguma notificação de parada ativa.
func (client *Client) NotificaCliente(ctx context.Context, cdConexao string) (bool, error) {
	ativas, err := client.notificacoesAtivas(ctx)
	if err != nil {
		return false, err
	}

	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return false, err
	}

	for _, notificacao := range ativas {
		query := url.Values{}
		query.Set("token", token)
		query.Set("codigo_parada", string(notificacao.Cod))

		var afetadas []conexaoAfetadaRaw
		if err := client.get(ctx, "/core-api/notificacoes/conexoes-afetadas", query, &afetadas); err != nil {
			return false, err
		}

		for _, afetada := range afetadas {
			if string(afetada.CodConexao) == cdConexao {
				return true, nil
			}
		}
	}

	return false, nil
}
