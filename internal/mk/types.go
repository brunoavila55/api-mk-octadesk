package mk

import (
	"encoding/json"
	"fmt"
)

// flexString aceita um valor JSON como string ou número e sempre expõe como
// string em Go. Confirmado necessário contra o MK de produção:
// WSMKConsultaDoc.rule devolve CodigoPessoa como número JSON, não string, ao
// contrário do que o código Svelte antigo assumia (JS não distingue os dois).
// Aplicado a todos os campos de código/id da mesma família de endpoints por
// segurança, já que o MK claramente não é consistente nisso.
type flexString string

func (f *flexString) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*f = flexString(asString)
		return nil
	}

	var asNumber json.Number
	if err := json.Unmarshal(data, &asNumber); err == nil {
		*f = flexString(asNumber.String())
		return nil
	}

	return fmt.Errorf("valor não é string nem número: %s", string(data))
}

// ConexaoInfo representa uma conexão do cliente no MK.
type ConexaoInfo struct {
	CodConexao string `json:"codConexao"`
	Endereco   string `json:"endereco"`
	Bloqueada  bool   `json:"bloqueada"`
	Notificado bool   `json:"notificado"`
}

// CadastroInfo representa um cadastro (lead ou cliente) encontrado por documento.
type CadastroInfo struct {
	CodCliente string `json:"codCliente"`
	Nome       string `json:"nome"`
	Endereco   string `json:"endereco"`
}

// ConsultaDocumentoResult agrega os cadastros encontrados para um CPF/CNPJ.
type ConsultaDocumentoResult struct {
	Tipo      string         `json:"tipo"`
	Cadastros []CadastroInfo `json:"cadastros"`
}

// FaturaBoleto representa a segunda via de uma fatura pendente.
type FaturaBoleto struct {
	CodFatura    string `json:"codFatura"`
	Descricao    string `json:"descricao"`
	LinkDownload string `json:"linkDownload"`
	Valor        string `json:"valor"`
	Vencimento   string `json:"vencimento"`
}

// FaturaPix representa o código copia-e-cola do PIX de uma fatura pendente.
type FaturaPix struct {
	CodFatura     string `json:"codFatura"`
	Descricao     string `json:"descricao"`
	PixCopiaECola string `json:"pixCopiaECola"`
	Valor         string `json:"valor"`
	Vencimento    string `json:"vencimento"`
}

// AutoDesbloqueioResult representa o retorno da ação de autodesbloqueio.
//
// Confirmado contra o MK real (2026-09-12), três desfechos possíveis — o
// `status` só diferencia sucesso de recusa; o *motivo* da recusa vem no texto
// de `mensagem`, que o consumidor (chatbot) deve inspecionar se quiser dar
// uma resposta diferente para cada caso:
//   - sucesso: {"status":"OK"} (sem mensagem)
//   - conexão não está bloqueada (nada a fazer):
//     {"status":"ERRO","mensagem":"Status na conexão incompatível para
//     auto-desbloqueio."}
//   - conexão bloqueada, mas o desbloqueio automático já foi usado este mês:
//     {"status":"ERRO","mensagem":"Operação indisponível para esta conexão."}
type AutoDesbloqueioResult struct {
	Status   string `json:"status"`
	Mensagem string `json:"mensagem,omitempty"`
}

type mkConexoesResponse struct {
	Status   string `json:"status"`
	Conexoes []mkConexaoRaw
}

type mkConexaoRaw struct {
	CodConexao flexString `json:"codconexao"`
	Endereco   string     `json:"endereco"`
	Bloqueada  string     `json:"bloqueada"`
}

type mkConsultaDocResponse struct {
	Status       string            `json:"status"`
	Situacao     string            `json:"Situacao"`
	CodigoPessoa flexString        `json:"CodigoPessoa"`
	Nome         string            `json:"Nome"`
	Endereco     string            `json:"Endereco"`
	Outros       []mkConsultaOutro `json:"Outros"`
}

type mkConsultaOutro struct {
	CodigoPessoa flexString `json:"CodigoPessoa"`
	Nome         string     `json:"Nome"`
	Endereco     string     `json:"Endereco"`
}

type mkFaturasPendentesResponse struct {
	Status           string        `json:"status"`
	FaturasPendentes []mkFaturaRaw `json:"FaturasPendentes"`
}

type mkFaturaRaw struct {
	CodFatura      flexString `json:"codfatura"`
	Descricao      string     `json:"descricao"`
	DataVencimento string     `json:"data_vencimento"`
}

type mkSegundaViaResponse struct {
	PathDownload string `json:"PathDownload"`
	Valor        string `json:"Valor"`
	Vcto         string `json:"Vcto"`
}

type mkPixResponse struct {
	TextoQrCode string `json:"texto_qrcode"`
}

type notificacaoAtivaRaw struct {
	Cod flexString `json:"cod"`
}

type conexaoAfetadaRaw struct {
	CodConexao flexString `json:"codconexao"`
}
