package mk

import "testing"

func TestAutoDesbloqueioResult_Classificar(t *testing.T) {
	cases := []struct {
		nome     string
		result   AutoDesbloqueioResult
		esperado ClassificacaoAutodesbloqueio
	}{
		{
			nome:     "sucesso",
			result:   AutoDesbloqueioResult{Status: "OK"},
			esperado: AutodesbloqueioSucesso,
		},
		{
			nome:     "conexao nao bloqueada",
			result:   AutoDesbloqueioResult{Status: "ERRO", Mensagem: "Status na conexão incompatível para auto-desbloqueio."},
			esperado: AutodesbloqueioNaoBloqueada,
		},
		{
			nome:     "limite mensal atingido",
			result:   AutoDesbloqueioResult{Status: "ERRO", Mensagem: "Operação indisponível para esta conexão."},
			esperado: AutodesbloqueioLimiteMensal,
		},
		{
			nome:     "mensagem desconhecida",
			result:   AutoDesbloqueioResult{Status: "ERRO", Mensagem: "algo que o MK nunca respondeu antes"},
			esperado: AutodesbloqueioDesconhecida,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.nome, func(t *testing.T) {
			if got := testCase.result.Classificar(); got != testCase.esperado {
				t.Fatalf("esperava %q, obteve %q", testCase.esperado, got)
			}
		})
	}
}
