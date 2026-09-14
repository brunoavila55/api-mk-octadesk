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

// ConexoesAfetadasAtivas retorna o conjunto de códigos de conexão afetados
// por alguma notificação de parada ativa no momento. Usado para cruzar contra
// várias conexões de uma vez (ex.: todas as conexões de um cliente) sem
// repetir a busca de notificações ativas a cada conexão.
func (client *Client) ConexoesAfetadasAtivas(ctx context.Context) (map[string]bool, error) {
	ativas, err := client.notificacoesAtivas(ctx)
	if err != nil {
		return nil, err
	}

	afetadas := make(map[string]bool)
	if len(ativas) == 0 {
		return afetadas, nil
	}

	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	for _, notificacao := range ativas {
		query := url.Values{}
		query.Set("token", token)
		query.Set("codigo_parada", string(notificacao.Cod))

		var lista []conexaoAfetadaRaw
		if err := client.get(ctx, "/core-api/notificacoes/conexoes-afetadas", query, &lista); err != nil {
			return nil, err
		}

		for _, afetada := range lista {
			afetadas[string(afetada.CodConexao)] = true
		}
	}

	return afetadas, nil
}

// NotificaCliente indica se a conexão informada está na lista de afetados de
// alguma notificação de parada ativa.
func (client *Client) NotificaCliente(ctx context.Context, cdConexao string) (bool, error) {
	afetadas, err := client.ConexoesAfetadasAtivas(ctx)
	if err != nil {
		return false, err
	}

	return afetadas[cdConexao], nil
}
