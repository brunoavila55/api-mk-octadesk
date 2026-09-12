# api-mk-octadesk

API em Go que conecta o chatbot Octadesk ao ERP MK, rodando em paralelo às
rotas antigas em SvelteKit (que continuam no ar em outro fluxo do Octadesk,
sem mudanças). Segue o padrão descrito em `guia_criar_api.md`.

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
| POST | `/v1/llm-classifica-mensagem` | corpo `{"mensagem": "..."}` | Classifica a mensagem do cliente em um setor (`vendas`, `financeiro`, `suporte` ou `atendimento`) via LLM |
| GET | `/health` | — | Healthcheck, sem autenticação |
| GET | `/metrics` | — | Métricas Prometheus, sem autenticação |

As rotas de consulta preenchem o array de `dados` com objetos vazios até um
mínimo de 3 itens, para compatibilidade com os flows atuais do Octadesk.

## Classificação de mensagens via LLM

`POST /v1/llm-classifica-mensagem` recebe **só a mensagem do cliente** (sem
histórico, sem pergunta anterior) e devolve o setor de destino:

```json
// corpo da requisição
{"mensagem": "estou sem internet desde ontem"}
```
```json
// resposta
{"status": "ok", "dados": {"destino": "suporte"}}
```

`dados.destino` é sempre uma destas 4 strings (usar exatamente esses valores
no if/else do flow do Octadesk):

| `destino` | Quando a LLM classifica assim | Exemplos de mensagem |
|---|---|---|
| `suporte` | Sem internet/serviço, conexão caindo ou lenta, modem/roteador com problema, Wi-Fi não conecta | "estou sem internet", "a net caiu", "modem com luz vermelha" |
| `financeiro` | Boleto, segunda via, fatura, pagamento, PIX, cobrança, vencimento | "quero a segunda via do boleto", "me manda o pix", "minha fatura venceu" |
| `vendas` | Endereço/localização/cobertura, contratação, planos, nova instalação, mudança de endereço | "quero contratar internet", "vocês atendem no meu bairro?", "quais os planos?" |
| `atendimento` | Mensagem sem informação suficiente pra decidir com segurança (saudações, agradecimentos, pedido genérico, fragmento ambíguo) — a LLM prefere isso a arriscar um chute | "bom dia", "oi", "tenho uma dúvida", "centro" (sozinho, sem mais contexto) |

Se houver mais de uma intenção na mesma mensagem (ex.: "sem internet e
preciso do boleto"), a LLM decide sozinha o `destino` mais urgente, nesta
prioridade: `suporte` > `financeiro` > `vendas` > `atendimento`.

Em caso de erro/timeout na chamada (ver lista de códigos abaixo), a rota não
devolve nenhum desses 4 valores — devolve `status: "erro"` com HTTP 502/504,
e o flow deve tratar isso caindo na fila de humanos em vez de tentar ler
`dados.destino`.

`dados.destino` é o campo que o flow do Octadesk usa num if/else para tagear
a conversa. Esta rota:

- **não chama o MK** (não importa `internal/mk`);
- **não chama a API do Octadesk de volta** — quem tageia a conversa é o
  próprio flow, com o valor que esta rota devolve;
- nunca loga o texto da mensagem do cliente, só o destino classificado
  (mesmo princípio de privacidade já usado no resto do projeto);
- em caso de falha, timeout ou resposta não interpretável do modelo, devolve
  erro (`llm_timeout` / 504, ou `llm_indisponivel`/`llm_resposta_invalida` /
  502) para o flow do Octadesk cair na fila de humanos.

### Setup do Ollama (modelo `Qwen/Qwen2.5-3B-Instruct`)

O `compose.yaml` já sobe um serviço `ollama` (imagem oficial `ollama/ollama`,
sem exposição via Traefik — só a `api` fala com ele na rede interna). Depois
de subir a stack, é preciso baixar o modelo base e criar o modelo
classificador customizado a partir de `ollama/Modelfile`:

```bash
docker compose up -d ollama
docker compose exec ollama ollama pull qwen2.5:3b-instruct
docker compose exec ollama ollama create atendimento-classificador -f /modelfiles/Modelfile
```

`OLLAMA_MODEL` (em `.env`) precisa bater com o nome usado no `ollama create`
acima. Para reaplicar mudanças no Modelfile, rode o `ollama create` de novo —
ele substitui o modelo existente.

O `ollama/Modelfile` **ainda não é a versão final** — é a base atual do
prompt/exemplos usados para classificar a mensagem, e deve ser ajustado
conforme o modelo for testado com conversas reais.

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

## Monitoramento

Prometheus + Grafana rodam como stack separada — ver `monitoring/README.md`.
Dashboard já provisionado com requisições/erros/latência por rota, incluindo
total de requisições por rota no período (pra ver quais rotas os clientes
mais usam).

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

# Classificação LLM — espera 200 com dados.destino em {vendas,financeiro,suporte,atendimento}
curl -i https://api.newlifefibra.com.br/v1/llm-classifica-mensagem \
  --request POST \
  --header "X-API-Key: $CHATBOT_API_KEY" \
  --header "Content-Type: application/json" \
  --data '{"mensagem": "estou sem internet desde ontem"}'
```
