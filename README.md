# api-mk-octadesk

API em Go que conecta o chatbot Octadesk ao ERP MK, substituindo as rotas
antigas em SvelteKit (mantidas em `old_api/` apenas como referência histórica —
não fazem parte do build). Segue o padrão descrito em `guia_criar_api.md`.

## Rotas

Todas exigem o header `X-API-Key` e respondem no formato:

```json
{"status": "ok", "dados": {}}
```
ou, em caso de erro:
```json
{"status": "erro", "erro": {"codigo": "...", "mensagem": "..."}}
```

| Método | Rota | Parâmetro | Descrição |
|---|---|---|---|
| GET | `/v1/consulta-documento` | `documento` (CPF/CNPJ) | Busca cadastros e conexões associadas a um documento |
| GET | `/v1/consulta-conexao` | `cd_cliente` | Lista conexões de um código de cliente MK |
| GET | `/v1/consulta-notificacao-ativa` | — | Indica se há alguma notificação de parada ativa |
| GET | `/v1/consulta-notifica-cliente` | `cd_conexao` | Indica se uma conexão específica está afetada por uma parada ativa |
| GET | `/v1/gera-boleto` | `cd_cliente` | Segunda via das faturas pendentes relevantes |
| GET | `/v1/gera-pix` | `cd_cliente` | Código PIX copia-e-cola das faturas pendentes relevantes |
| GET | `/v1/autodesbloqueio` | `cd_conexao` | Solicita desbloqueio automático (ação — MK limita a 1x/mês) |
| GET | `/health` | — | Healthcheck, sem autenticação |
| GET | `/metrics` | — | Métricas Prometheus, sem autenticação |

As rotas de consulta preenchem o array de `dados` com objetos vazios até um
mínimo de 3 itens, para compatibilidade com os flows atuais do Octadesk.

## Validado contra o MK real de produção (2026-09-12)

Testado localmente via `docker run` (sem Traefik) com credenciais de produção
e o IP já liberado no MK. Todas as 7 rotas responderam com dados reais:

- `consulta-documento`, `consulta-conexao`, `consulta-notificacao-ativa`,
  `consulta-notifica-cliente`, `gera-boleto`, `gera-pix`: 200 com dados reais.
- `autodesbloqueio`: confirmados os 3 desfechos possíveis. Sucesso:
  `{"status":"OK"}` (sem mensagem). Conexão não bloqueada:
  `{"status":"ERRO","mensagem":"Status na conexão incompatível para
  auto-desbloqueio."}`. Bloqueada mas desbloqueio já usado este mês:
  `{"status":"ERRO","mensagem":"Operação indisponível para esta
  conexão."}` — o `status` não diferencia os dois motivos de recusa, só a
  `mensagem`; o flow do Octadesk deve ler o texto se quiser responder
  diferente em cada caso.
- `data_vencimento` confirmado no formato `DD/MM/YYYY` (ex.: `10/09/2026`) —
  o parser em `internal/mk/faturas.go` já cobre esse layout corretamente.
- Bug real encontrado e corrigido: `WSMKConsultaDoc.rule` devolve
  `CodigoPessoa` como número JSON, não string — corrigido com o tipo
  `flexString` em `internal/mk/types.go`, aplicado a todos os campos de
  código/id da mesma família de endpoints.
- Bug de segurança real encontrado e corrigido: um timeout vazava a URL
  completa (com token do MK) na mensagem de erro, porque o `*url.Error`
  nativo do `http.Client` inclui a URL. Corrigido com `requestError` /
  `sanitizeRequestError` em `internal/mk/client.go`, que preserva a
  classificação de timeout mas nunca expõe a URL.

Deploy em produção (`api.newlifefibra.com.br`) validado ponta a ponta em
2026-09-12: DNS, certificado Let's Encrypt, roteamento Traefik e autenticação
confirmados nas 7 rotas; dados reais do MK confirmados em 6 delas
(`autodesbloqueio` validado localmente contra o mesmo MK, sem repetir via
Traefik por ser uma ação).

## Corte no Octadesk

Ao migrar cada flow, trocar:
- **Autenticação**: query param `?key=...` → header `X-API-Key: ...`.
- **Envelope de resposta**: campos que antes ficavam na raiz agora ficam
  dentro de `dados` (ex.: `response.codConexao` → `response.dados[0].codConexao`).

| Flow antigo (Octadesk) | Rota nova | Parâmetro | Mudança no acesso à resposta |
|---|---|---|---|
| `consulta-cliente` (por `doc`) | `/v1/consulta-documento` | `doc` → **`documento`** | raiz → `dados.tipo`, `dados.cadastros[]` |
| `consulta-conexao` | `/v1/consulta-conexao` | `cd_cliente` (igual) | raiz array → `dados[]` |
| `consulta-notif-parada-ativa` | `/v1/consulta-notificacao-ativa` | nenhum | `response.status` → `dados.status` |
| `notif-parada` | `/v1/consulta-notifica-cliente` | `cd_conexao` (igual) | `response.status` → `dados.status` |
| `consulta-boletos-pessoa` (parte boleto) | `/v1/gera-boleto` | `cd_cliente` (igual) | raiz array → `dados[]`; **sem** campo `pixCopiaeCola` |
| `consulta-boletos-pessoa` (parte pix) | `/v1/gera-pix` | `cd_cliente` (igual) | raiz array → `dados[]`; campo agora se chama **`pixCopiaECola`** (antes `pixCopiaeCola` — atenção à capitalização do "E") |
| `autodesbloqueio` | `/v1/autodesbloqueio` | `cd_conexao` (igual) | raiz → `dados.status`, `dados.mensagem` |

Ordem sugerida (mais simples → mais arriscada): `consulta-conexao` →
`consulta-documento` → `consulta-notificacao-ativa` → `consulta-notifica-cliente`
→ `gera-boleto`/`gera-pix` → `autodesbloqueio` por último. Para cada uma:
atualizar o flow → testar no Octadesk com um contato de teste → só então
desativar/remover a chamada à rota Svelte antiga correspondente.

## ⚠️ Itens pendentes de confirmação no MK

- **Erros de "não encontrado"**: as rotas que dependiam de `status !== "OK"`
  hoje devolvem 404 (`nao_encontrado`) em vez do 401 que o código antigo
  devolvia — confirmar que isso não quebra nenhuma lógica de flow existente
  no Octadesk.

## Variáveis de ambiente

Ver `.env.example`. Depois de alterar o `.env` na VM, recriar o container:

```bash
docker compose up -d --force-recreate api
```

## Desenvolvimento

```bash
gofmt -w ./cmd ./internal
go vet ./...
go test ./...
go test -race ./...
```

## Build e deploy

```bash
docker compose build api
docker compose up -d api
docker compose logs --tail=100 api
curl http://127.0.0.1:8080/metrics
```

Traefik já está configurado em `compose.yaml` para `api.newlifefibra.com.br`,
um router por rota pública, todos na rede externa `proxy`.

## Validação pós-deploy

Para cada rota, seguindo a seção 13 do guia:

```bash
# Sem chave — espera 401
curl -i https://api.newlifefibra.com.br/v1/consulta-conexao

# Com chave e parâmetro inválido — espera 400
curl --get 'https://api.newlifefibra.com.br/v1/consulta-conexao' \
  --data-urlencode 'cd_cliente=abc' \
  --header "X-API-Key: $CHATBOT_API_KEY"

# Fluxo completo com dado real autorizado — espera 200
```
