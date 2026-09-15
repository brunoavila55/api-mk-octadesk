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
| GET | `/v1/consulta-conexao` | `cd_cliente` | Lista conexões de um código de cliente MK, cada uma já com `notificado` (se está afetada por parada ativa) |
| GET | `/v1/consulta-notificacao-ativa` | — | Indica se há alguma notificação de parada ativa |
| GET | `/v1/consulta-notifica-cliente` | `cd_conexao` | Indica se uma conexão específica está afetada por uma parada ativa (redundante se o flow já chamou `consulta-conexao` antes) |
| GET | `/v1/gera-boleto` | `cd_cliente` | Segunda via das faturas pendentes relevantes |
| GET | `/v1/gera-pix` | `cd_cliente` | Código PIX copia-e-cola das faturas pendentes relevantes |
| GET | `/v1/autodesbloqueio` | `cd_conexao` | Solicita desbloqueio automático (ação — MK limita a 1x/mês) |
| POST | `/v1/llm-cf-classifica-mensagem` | corpo `{"mensagem": "..."}` | Classifica a mensagem do cliente em um setor (`vendas`, `renovacao`, `ampliacao`, `relacionamento`, `titular`, `cancelamento`, `financeiro`, `suporte` ou `atendimento`) via Cloudflare Workers AI — ver seção "Classificação de mensagens via LLM" |
| GET | `/health` | — | Healthcheck, sem autenticação |
| GET | `/metrics` | — | Métricas Prometheus, sem autenticação |

As rotas de consulta preenchem o array de `dados` com objetos vazios até um
mínimo de 3 itens, para compatibilidade com os flows atuais do Octadesk.

## Classificação de mensagens via LLM

A classificação roda via Cloudflare Workers AI
(`@cf/meta/llama-3.1-8b-instruct-fp8-fast`), na rota
`POST /v1/llm-cf-classifica-mensagem` (o nome mantém o sufixo `-cf-` porque é
o que o flow do Octadesk já chama hoje). Antes rodava num modelo Ollama local
(`qwen2.5:3b-instruct`) em paralelo, mas esse backend foi descontinuado: o
Ollama tinha viés de classificar qualquer coisa "com cara de endereço"
(rua/cidade) como `endereco` em vez de `vendas`, e a Cloudflare saiu bem mais
rápida (~0.3-0.9s vs. 3-4s+) e mais precisa nos testes — ver `llm_expand.md`
para o histórico da migração.

A rota recebe **só a mensagem do cliente** (sem histórico, sem pergunta
anterior) e devolve o setor de destino:

```json
// corpo da requisição
{"mensagem": "estou sem internet desde ontem"}
```
```json
// resposta
{"status": "ok", "dados": {"destino": "suporte"}}
```

`dados.destino` é sempre uma destas 9 strings (usar exatamente esses valores
no if/else do flow do Octadesk):

| `destino` | Quando a LLM classifica assim | Exemplos de mensagem |
|---|---|---|
| `suporte` | Sem internet/serviço, conexão caindo ou lenta, modem/roteador com problema, Wi-Fi não conecta | "estou sem internet", "a net caiu", "modem com luz vermelha" |
| `cancelamento` | Cancelar o serviço, encerrar o contrato, não quer mais o serviço | "quero cancelar", "cancelar contrato", "não quero mais o serviço" |
| `financeiro` | Boleto, segunda via, fatura, pagamento, PIX, cobrança, vencimento, ou só "contrato" sozinho (sem contexto pra saber se é renovação/cancelamento) | "quero a segunda via do boleto", "me manda o pix", "minha fatura venceu", "contrato" |
| `titular` | Trocar o titular do contrato, trocar o dono da conta, trocar quem paga | "quero trocar o titular", "trocar o dono da conta", "quero colocar o contrato no nome da minha esposa" |
| `renovacao` | Renovar contrato existente, contrato vencendo, continuar no mesmo plano | "quero renovar o contrato", "meu contrato está vencendo" |
| `ampliacao` | Aumentar velocidade/plano do contrato existente, pedir mais um roteador/ponto de rede | "quero aumentar a velocidade", "aumentar plano", "mais um roteador" |
| `relacionamento` | Cliente já ativo pedindo pra mudar o serviço/instalação de lugar — mudança de casa, trocar o ponto, transferir o serviço atual — só quando há verbo/expressão explícita de mudança; nunca só por a mensagem conter um endereço | "quero trocar o endereço", "vou mudar de casa, preciso trocar o ponto", "trocar o ponto", "vou mudar de casa semana que vem, preciso transferir a internet" |
| `vendas` | Endereço/localização/cobertura, contratação nova (endereço onde o cliente nunca teve serviço), planos, nova instalação; **inclui um endereço dito sozinho, sem verbo de mudar/trocar/transferir** — mesmo um endereço completo com rua e número, é a resposta típica à pergunta "qual o seu endereço?" feita a quem está pedindo cobertura/instalação nova | "quero contratar internet", "vocês atendem no meu bairro?", "quais os planos?", "centro", "vila block sao sepe", "rua erechin 369" |
| `atendimento` | Mensagem sem informação suficiente pra decidir com segurança (saudações, agradecimentos, pedido genérico, fragmento ambíguo) — a LLM prefere isso a arriscar um chute | "bom dia", "oi", "tenho uma dúvida" |

Se houver mais de uma intenção na mesma mensagem (ex.: "sem internet e
preciso do boleto"), a LLM decide sozinha o `destino` mais urgente, nesta
prioridade: `suporte` > `cancelamento` > `financeiro` > `titular` >
`renovacao` > `ampliacao` > `relacionamento` > `vendas` > `atendimento`.

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

### Setup da Cloudflare Workers AI

1. **Conta**: se ainda não tiver, crie em https://dash.cloudflare.com/sign-up
   (o Workers AI funciona mesmo sem domínio próprio configurado na conta).
2. **Account ID**: no dashboard da Cloudflare, abra qualquer domínio/zona (ou
   a página "Workers & Pages") — o "Account ID" aparece na barra lateral
   direita. Esse é o valor de `CLOUDFLARE_ACCOUNT_ID`.
3. **API Token**: em *My Profile → API Tokens → Create Token → Custom
   Token*, dê um nome (ex. `api-mk-octadesk-workers-ai`) e adicione a
   permissão **Account → Workers AI → Read** (suficiente para rodar
   inferência; não precisa de "Edit"). Restrinja a esse Account ID
   específico se a conta tiver mais de um. Copie o token gerado — ele só é
   mostrado uma vez — e cole em `CLOUDFLARE_API_TOKEN` no `.env`.
4. Deixe `CLOUDFLARE_AI_MODEL` e `CLOUDFLARE_AI_BASE_URL` nos valores padrão
   do `.env.example`, a menos que queira testar outro modelo do catálogo
   (https://developers.cloudflare.com/workers-ai/models/).
5. Suba/recrie a `api` normalmente (ver "Build e deploy") — não há serviço
   novo no `compose.yaml`, é só a rota `/v1/llm-cf-classifica-mensagem` já
   embutida no binário da API chamando a API HTTP da Cloudflare.

**Orçamento**: o plano gratuito do Workers AI dá 10.000 Neurons/dia; o
`llama-3.1-8b-instruct-fp8-fast` gasta bem pouco por classificação (mensagem
curta, resposta curtíssima em JSON), então o volume atual de ~250
classificações/dia cabe com folga. Se passar do limite, a Cloudflare
bloqueia novas chamadas até resetar (00:00 UTC) — não cobra automaticamente;
cobrar exige estar no plano pago.

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
mais usam). As métricas de classificação (`mk_octadesk_llm_classificacao_total`
e `mk_octadesk_llm_erros_total`) têm um label `backend` (hoje sempre
`cloudflare`) — útil pra ver volume/distribuição de destino e taxa de erro
da classificação.

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

# Classificação LLM (Cloudflare) — espera 200 com dados.destino em {vendas,renovacao,ampliacao,relacionamento,titular,cancelamento,financeiro,suporte,atendimento}
curl -i https://api.newlifefibra.com.br/v1/llm-cf-classifica-mensagem \
  --request POST \
  --header "X-API-Key: $CHATBOT_API_KEY" \
  --header "Content-Type: application/json" \
  --data '{"mensagem": "estou sem internet desde ontem"}'
```
