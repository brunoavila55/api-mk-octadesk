# Monitoring — Prometheus + Grafana

Stack separada (own `docker compose` project) que faz scrape do `/metrics` da
`api-mk-octadesk` e expõe dashboards em Grafana. Já publicada na VM em
`prometheus.api.newlifefibra.com.br` (com basic auth) e
`grafana.api.newlifefibra.com.br`.

## Dashboard "API MK Octadesk"

Provisionado automaticamente (`grafana/provisioning/dashboards/api-mk-octadesk.json`):

- Requisições por rota (taxa)
- Erros HTTP 5xx por rota (taxa)
- Latência p95 por rota
- Requisições em andamento / API up
- Erros do MK por rota e código (`mk_octadesk_mk_errors_total`)
- Total de requisições por rota no período selecionado — para responder
  "quais rotas os clientes mais usam" (ex.: nem todo mundo pede boleto)

## Deploy / atualização

```bash
cd monitoring
cp .env.example .env   # preencher GRAFANA_ADMIN_PASSWORD e o basic auth do Prometheus
docker compose up -d
```

Editar o dashboard: alterar `grafana/provisioning/dashboards/api-mk-octadesk.json`
e rodar `docker compose restart grafana` (o provider re-lê o arquivo a cada
30s, mas um restart garante o reload imediato).

Editar o scrape config: alterar `prometheus/prometheus.yml` e rodar
`docker compose restart prometheus`.
