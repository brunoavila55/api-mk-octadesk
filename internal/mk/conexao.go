package mk

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// ErrRegistroNaoEncontrado indica que o MK respondeu, mas não encontrou dados
// para o parâmetro informado — não é uma falha de autenticação nem do MK.
var ErrRegistroNaoEncontrado = errors.New("registro não encontrado no MK")

// ConexoesPorCliente consulta as conexões associadas a um código de cliente MK.
func (client *Client) ConexoesPorCliente(ctx context.Context, cdCliente string) ([]ConexaoInfo, error) {
	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("sys", "MK0")
	query.Set("token", token)
	query.Set("cd_cliente", cdCliente)

	var parsed mkConexoesResponse
	if err := client.get(ctx, "/mk/WSMKConexoesPorCliente.rule", query, &parsed); err != nil {
		return nil, err
	}

	if parsed.Status != "OK" {
		return nil, ErrRegistroNaoEncontrado
	}

	conexoes := make([]ConexaoInfo, 0, len(parsed.Conexoes))
	for _, raw := range parsed.Conexoes {
		conexoes = append(conexoes, ConexaoInfo{
			CodConexao: string(raw.CodConexao),
			Endereco:   raw.Endereco,
			Bloqueada:  bloqueadaParaBool(raw.Bloqueada),
		})
	}

	return conexoes, nil
}

// bloqueadaParaBool converte o texto que o MK devolve no campo bloqueada
// para booleano. Confirmado contra o MK real (2026-09-14): o valor é "Sim"
// ou "Não" (não "S"/"N", como os fixtures de teste antigos assumiam) —
// qualquer outro valor é tratado como não bloqueada.
func bloqueadaParaBool(valor string) bool {
	return strings.EqualFold(strings.TrimSpace(valor), "sim")
}
