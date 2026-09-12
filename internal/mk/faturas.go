package mk

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// parseDataVencimento converte a data de vencimento retornada pelo MK.
//
// ATENÇÃO: o formato exato de data_vencimento devolvido por
// WSMKFaturasPendentes.rule ainda não foi confirmado no Insomnia (o código
// antigo em Svelte usava um parser próprio, $lib/core/utils.js, não
// preservado neste repositório). Os layouts abaixo cobrem os formatos mais
// comuns de ERPs brasileiros; ajuste assim que o formato real for confirmado.
func parseDataVencimento(raw string) (time.Time, error) {
	layouts := []string{"02/01/2006", "2006-01-02", "2006-01-02T15:04:05"}
	var lastErr error
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
	}
	return time.Time{}, fmt.Errorf("formato de data_vencimento não reconhecido (%q): %w", raw, lastErr)
}

type faturaRelevante struct {
	CodFatura  string
	Descricao  string
	Vencimento time.Time
}

// relevantPendingInvoices replica o filtro de datas do fluxo antigo: faturas
// vencidas (do ano passado, de meses anteriores deste ano, ou deste mês antes
// de hoje); se nenhuma estiver vencida, cai para as faturas deste mês que
// ainda vão vencer.
func (client *Client) relevantPendingInvoices(ctx context.Context, cdCliente string) ([]faturaRelevante, error) {
	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("sys", "MK0")
	query.Set("token", token)
	query.Set("cd_cliente", cdCliente)

	var parsed mkFaturasPendentesResponse
	if err := client.get(ctx, "/mk/WSMKFaturasPendentes.rule", query, &parsed); err != nil {
		return nil, err
	}

	if parsed.Status != "OK" {
		return nil, ErrRegistroNaoEncontrado
	}

	hoje := time.Now()
	anoPassado := hoje.AddDate(-1, 0, 0).Year()

	var vencidas, aVencerEsteMes []faturaRelevante

	for _, raw := range parsed.FaturasPendentes {
		vencimento, err := parseDataVencimento(raw.DataVencimento)
		if err != nil {
			continue
		}

		item := faturaRelevante{CodFatura: string(raw.CodFatura), Descricao: raw.Descricao, Vencimento: vencimento}

		switch {
		case vencimento.Year() == anoPassado:
			vencidas = append(vencidas, item)
		case vencimento.Year() == hoje.Year() && vencimento.Month() < hoje.Month():
			vencidas = append(vencidas, item)
		case vencimento.Year() == hoje.Year() && vencimento.Month() == hoje.Month() && vencimento.Day() < hoje.Day():
			vencidas = append(vencidas, item)
		case vencimento.Year() == hoje.Year() && vencimento.Month() == hoje.Month() && vencimento.Day() >= hoje.Day():
			aVencerEsteMes = append(aVencerEsteMes, item)
		}
	}

	if len(vencidas) > 0 {
		return vencidas, nil
	}
	return aVencerEsteMes, nil
}

// GeraBoleto devolve a segunda via de cada fatura pendente relevante do cliente.
func (client *Client) GeraBoleto(ctx context.Context, cdCliente string) ([]FaturaBoleto, error) {
	faturas, err := client.relevantPendingInvoices(ctx, cdCliente)
	if err != nil {
		return nil, err
	}

	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	boletos := make([]FaturaBoleto, 0, len(faturas))
	for _, fatura := range faturas {
		query := url.Values{}
		query.Set("sys", "MK0")
		query.Set("token", token)
		query.Set("cd_fatura", fatura.CodFatura)

		var segundaVia mkSegundaViaResponse
		if err := client.get(ctx, "/mk/WSMKSegundaViaCobranca.rule", query, &segundaVia); err != nil {
			return nil, err
		}

		boletos = append(boletos, FaturaBoleto{
			CodFatura:    fatura.CodFatura,
			Descricao:    fatura.Descricao,
			LinkDownload: segundaVia.PathDownload,
			Valor:        segundaVia.Valor,
			Vencimento:   segundaVia.Vcto,
		})
	}

	return boletos, nil
}

// GeraPix devolve o código copia-e-cola do PIX de cada fatura pendente
// relevante do cliente. Assim como no fluxo antigo, o valor e o vencimento
// exibidos vêm de WSMKSegundaViaCobranca (fonte de verdade do valor atualizado
// da cobrança), não do registro bruto de FaturasPendentes.
func (client *Client) GeraPix(ctx context.Context, cdCliente string) ([]FaturaPix, error) {
	faturas, err := client.relevantPendingInvoices(ctx, cdCliente)
	if err != nil {
		return nil, err
	}

	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	pix := make([]FaturaPix, 0, len(faturas))
	for _, fatura := range faturas {
		segundaViaQuery := url.Values{}
		segundaViaQuery.Set("sys", "MK0")
		segundaViaQuery.Set("token", token)
		segundaViaQuery.Set("cd_fatura", fatura.CodFatura)

		var segundaVia mkSegundaViaResponse
		if err := client.get(ctx, "/mk/WSMKSegundaViaCobranca.rule", segundaViaQuery, &segundaVia); err != nil {
			return nil, err
		}

		pixQuery := url.Values{}
		pixQuery.Set("sys", "MK0")
		pixQuery.Set("token", token)
		pixQuery.Set("CodigoFatura", fatura.CodFatura)

		var pixResponse mkPixResponse
		if err := client.get(ctx, "/mk/WSMKRetornarCopieColaPix.rule", pixQuery, &pixResponse); err != nil {
			return nil, err
		}

		pix = append(pix, FaturaPix{
			CodFatura:     fatura.CodFatura,
			Descricao:     fatura.Descricao,
			PixCopiaECola: pixResponse.TextoQrCode,
			Valor:         segundaVia.Valor,
			Vencimento:    segundaVia.Vcto,
		})
	}

	return pix, nil
}
