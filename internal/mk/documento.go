package mk

import (
	"context"
	"fmt"
	"net/url"
)

const maxCadastrosRetornados = 3

// ConsultaDocumento consulta um CPF/CNPJ no MK e resolve, para cada cadastro
// encontrado, se há conexões ativas associadas — replicando o comportamento
// do fluxo antigo (old_api/consulta-cliente), mas com o documento já validado
// pelo pacote internal/document antes de chegar aqui.
func (client *Client) ConsultaDocumento(ctx context.Context, documento string) (ConsultaDocumentoResult, error) {
	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return ConsultaDocumentoResult{}, err
	}

	query := url.Values{}
	query.Set("sys", "MK0")
	query.Set("token", token)
	query.Set("doc", documento)

	var parsed mkConsultaDocResponse
	if err := client.get(ctx, "/mk/WSMKConsultaDoc.rule", query, &parsed); err != nil {
		return ConsultaDocumentoResult{}, err
	}

	result := ConsultaDocumentoResult{Tipo: "Lead", Cadastros: []CadastroInfo{}}

	switch parsed.Status {
	case "ERRO":
		// Documento não encontrado no MK — não é uma falha, é uma consulta vazia.
		return result, nil
	case "OK":
		// segue abaixo
	default:
		return ConsultaDocumentoResult{}, fmt.Errorf("MK retornou status inesperado %q em WSMKConsultaDoc", parsed.Status)
	}

	if parsed.Situacao == "Ativo" {
		client.appendCadastroSeTiverConexao(ctx, &result, string(parsed.CodigoPessoa), parsed.Nome, parsed.Endereco)
	}

	for index, outro := range parsed.Outros {
		if len(result.Cadastros) >= maxCadastrosRetornados || index >= 10 {
			break
		}
		client.appendCadastroSeTiverConexao(ctx, &result, string(outro.CodigoPessoa), outro.Nome, outro.Endereco)
	}

	return result, nil
}

func (client *Client) appendCadastroSeTiverConexao(ctx context.Context, result *ConsultaDocumentoResult, codigoPessoa, nome, endereco string) {
	conexoes, err := client.ConexoesPorCliente(ctx, codigoPessoa)
	if err != nil || len(conexoes) == 0 {
		return
	}

	result.Tipo = "Cliente"
	result.Cadastros = append(result.Cadastros, CadastroInfo{
		CodCliente: codigoPessoa,
		Nome:       nome,
		Endereco:   endereco,
	})
}
