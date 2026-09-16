# Tutorial: entendendo e expandindo a classificação por LLM

Este documento explica como funciona a classificação de mensagens via LLM
(`POST /v1/llm-cf-classifica-mensagem`, Cloudflare Workers AI), como ajustar
o comportamento dela e como aumentar o número de setores de destino. É
complementar ao `README.md` (que documenta o contrato da rota) e ao
`guia_criar_api.md` (que documenta o padrão geral de APIs deste projeto).

## 1. Visão geral: como a peça se encaixa

```
Cliente manda mensagem no Octadesk
        |
        v
Flow do Octadesk chama POST /v1/llm-cf-classifica-mensagem
        |  (só a mensagem do cliente, sem histórico)
        v
API Go (internal/httpapi + internal/llm)
        |  chama a API HTTP do Cloudflare Workers AI
        v
Cloudflare Workers AI + modelo @cf/meta/llama-3.1-8b-instruct-fp8-fast
        |  classifica e devolve {"destino_principal": "suporte"}
        v
API Go valida e devolve {"status":"ok","dados":{"destino":"suporte"}}
        |
        v
Flow do Octadesk lê dados.destino num if/else e tageia a conversa
```

Pontos importantes desse desenho, decididos durante a implementação:

- A LLM **só classifica**, nunca conversa com o cliente nem responde a
  dúvida dele.
- A LLM **nunca chama o MK** — só o restante da API (`internal/mk`) fala
  com o ERP.
- A API Go **nunca chama a API do Octadesk de volta** — quem tageia a
  conversa é o próprio flow, usando o valor de `dados.destino` que a rota
  devolve. Isso simplifica bastante a integração (sem credenciais do
  Octadesk armazenadas aqui).
- A Cloudflare não tem serviço próprio nesta VM — é só uma API HTTP externa
  chamada pela `api`, sem container/modelo pra manter no ar.

O nome da rota mantém o sufixo `-cf-` (histórico de quando convivia com uma
rota Ollama em paralelo — ver seção 9) mesmo sendo hoje o único backend.

## 2. As peças por trás da rota

| Peça | Arquivo | O que faz |
|---|---|---|
| Prompt/exemplos da LLM | `internal/llm/cloudflare_client.go` (`cfSystemPrompt`, `cfExemplos`) | Define as regras de classificação, os setores e os exemplos few-shot, como mensagens de chat. É o que "ensina" o modelo a classificar. |
| Cliente HTTP da Cloudflare | `internal/llm/cloudflare_client.go` | Monta a requisição pro Workers AI (`/accounts/{id}/ai/run/{model}`, com JSON Mode/schema), decodifica e valida se o `destino_principal` é um dos valores conhecidos (`destinosValidos`). |
| Handler HTTP | `internal/httpapi/handlers.go` (`classificaMensagemCloudflare`) | Recebe o corpo JSON do Octadesk, valida a mensagem, chama o cliente Cloudflare, formata a resposta pública. |
| Métricas | `internal/httpapi/metrics.go` | `mk_octadesk_llm_classificacao_total{backend,destino}` e `mk_octadesk_llm_erros_total{backend,codigo}` — `backend` é sempre `"cloudflare"` hoje, visíveis no Grafana. |

## 3. Os 9 destinos hoje

Ver a tabela completa no `README.md` (seção "Classificação de mensagens via
LLM"). Resumo: `suporte`, `cancelamento`, `financeiro`, `titular`,
`renovacao`, `ampliacao`, `relacionamento`, `vendas`, `atendimento` — com
prioridade `suporte > cancelamento > financeiro > titular > renovacao >
ampliacao > relacionamento > vendas > atendimento` quando a mensagem tem
mais de uma intenção.

## 4. Como mudar o prompt ("a pergunta" que a LLM responde)

Diferente do Ollama (que usava um Modelfile textual recarregável sem
rebuild), o prompt da Cloudflare é código Go — `cfSystemPrompt` e
`cfExemplos` em `internal/llm/cloudflare_client.go`. Qualquer mudança de
regra ou de exemplo few-shot passa pelo ciclo normal de build/deploy da API.

Passo a passo pra alterar:

1. Edite `cfSystemPrompt` (regras, definições de setor) e/ou `cfExemplos`
   (pares few-shot `{Role: "user", ...}` / `{Role: "assistant", ...}`) em
   `internal/llm/cloudflare_client.go`.
2. Rode `go test ./...` — não valida qualidade de classificação (não existe
   teste automatizado pra isso), mas garante que o Go compila e que os
   testes existentes do pacote `llm` continuam passando.
3. Rebuild e redeploy da API (ver "Build e deploy" no `README.md`):
   ```bash
   docker compose build api
   docker compose up -d --force-recreate api
   ```
4. Teste pela API real antes de considerar pronto:
   ```bash
   curl -s -X POST https://api.newlifefibra.com.br/v1/llm-cf-classifica-mensagem \
     -H "X-API-Key: $CHATBOT_API_KEY" -H "Content-Type: application/json" \
     -d '{"mensagem": "sua mensagem de teste aqui"}'
   ```

### Dicas pra escrever bons exemplos

- Cada par `cfExemplos` é um exemplo few-shot — o modelo aprende o padrão de
  resposta esperado a partir deles. Mais exemplos bons geralmente ajudam
  mais que regras longas em texto corrido.
- Evite exemplos ambíguos com respostas "confiantes" — se uma mensagem é
  genuinamente ambígua, o exemplo correto é classificá-la como `atendimento`
  (ver a regra "na dúvida... = atendimento" já presente no prompt).
- Depois de qualquer mudança, rode um punhado de mensagens reais (ou
  parecidas com reais) antes de considerar pronto — não tem teste
  automatizado de qualidade de classificação, só validação manual mesmo.

## 5. Como aumentar as saídas (adicionar um novo destino)

Diferente de só mudar regras/exemplos do prompt, isso **exige mudar código
de validação também**, porque a lista de destinos válidos é travada na API
por segurança (pra nunca repassar pro Octadesk um valor que a LLM
"inventou").

Exemplo: adicionar um destino `retencao`.

### 5.1 No prompt (`internal/llm/cloudflare_client.go`)

1. Adicione uma linha em `cfSystemPrompt`, na seção "Destinos", explicando
   quando classificar como `retencao`.
2. Decida onde ele entra na prioridade (linha "Se houver várias intenções,
   prioridade: ...") — por exemplo, entre `cancelamento` e `financeiro`,
   dependendo da urgência que você quer dar a esse setor.
3. Atualize a linha "Valores permitidos" pra incluir `retencao` na lista.
4. Adicione pelo menos 1-2 pares em `cfExemplos` pro novo destino, pro
   modelo aprender o padrão.

### 5.2 Na validação (`internal/llm/cloudflare_client.go`)

Adicione o novo valor a **dois** lugares no mesmo arquivo:

```go
var destinosValidos = map[string]bool{
	"vendas":       true,
	"financeiro":   true,
	"suporte":      true,
	"atendimento":  true,
	"retencao":     true, // novo
}

var destinosValidosOrdenados = []string{
	"vendas", "financeiro", "suporte", "atendimento",
	"retencao", // novo
}
```

`destinosValidos` é o que a API usa pra rejeitar (`llm_resposta_invalida`,
502) qualquer coisa que a LLM devolva fora do conjunto conhecido — essa
validação existe de propósito, pra nunca repassar ao Octadesk um destino
que ele não sabe tratar. `destinosValidosOrdenados` é usado no `enum` do
JSON Schema (`response_format: json_schema`) que já restringe a resposta da
Cloudflare no nível da própria chamada — as duas listas precisam ficar em
sincronia (mesmo conjunto de valores).

### 5.3 Rebuild e deploy

```bash
docker compose build api
docker compose up -d --force-recreate api
```

### 5.4 Depois de implantar

- Atualize a tabela de destinos no `README.md`.
- Configure o novo ramo no if/else do flow do Octadesk.
- Teste ponta a ponta (API → Octadesk) antes de considerar pronto.

### 5.5 Cuidado: mais categorias = mais difícil pro modelo

Todo modelo tem limite de quantas categorias consegue distinguir com
confiança, principalmente com mensagens curtas e informais. Cada novo
destino deveria vir com exemplos claros e, idealmente, alguns exemplos
"negativos" (mensagens parecidas que NÃO são desse destino, pra reforçar a
fronteira de decisão). Teste bastante antes de liberar em produção — não
existe teste automatizado de qualidade, só validação manual.

## 6. Operação: o que já aprendemos rodando isso em produção

- **Latência típica**: ~0.3-0.9s por classificação — a Cloudflare mantém os
  modelos do catálogo oficial sempre residentes ("sempre quentes"), sem
  custo de aquecimento a frio.
- **Orçamento de Neurons**: free tier de 10.000/dia (reseta 00:00 UTC; ao
  estourar, bloqueia em vez de cobrar — só cobra excedente no plano pago).
  Com ~250 classificações/dia e o modelo escolhido (`@cf/meta/llama-3.1-8b-
  instruct-fp8-fast`), a estimativa fica bem abaixo do limite. Se o volume
  crescer muito ou trocar pra um modelo mais caro, vale reduzir o tamanho do
  prompt (`cfSystemPrompt` + `cfExemplos`) antes de aceitar o bloqueio ou
  migrar pro plano pago.
- **Timeout**: `CLOUDFLARE_HTTP_TIMEOUT` (padrão 45s, ver `.env.example`).
  Ajustar com base na latência real observada (Grafana) se necessário.
- **Nomes de destino podem mudar independente do conteúdo do prompt.**
  Dois exemplos reais: `trocaendereco`/`trocatitular` viraram
  `endereco`/`titular` (nomes mais curtos, mesmo significado) numa revisão
  quando o backend ainda era Ollama; depois, já na Cloudflare, `endereco`
  virou `relacionamento` — o nome antigo colidia com a própria palavra
  "endereço" que aparece nas mensagens de `vendas`, o que ajudava o modelo a
  confundir "a mensagem tem um endereço" com "o destino é sobre endereço".
  Renomear um destino existente exige os mesmos passos de adicionar um novo
  (seção 5): atualizar `cfSystemPrompt`/`cfExemplos`,
  `destinosValidos`/`destinosValidosOrdenados`, `README.md`, e
  rebuildar/redeployar a `api`. **E mais um passo que só existe pra rename,
  não pra destino novo**: o flow do Octadesk que já estava configurado pra
  ler o valor antigo precisa ser atualizado pro nome novo — sem isso, o
  if/else do flow não reconhece o valor novo e a conversa cai no branch
  errado (ou nenhum).
- **Nome de rua pode parecer data**: caso real em produção — cliente
  respondeu "21 de abril 1746" pra "Para qual endereço você deseja
  contratar a nossa internet?" e foi classificado errado, porque "21 de
  abril" isolado parece uma data, não uma rua. Na região atendida há vários
  bairros com ruas batizadas com datas de feriados (ex.: 7 de Setembro, 15
  de Novembro, 20 de Setembro), então "<data> <número>" sozinho deve ser
  tratado como endereço = `vendas`. Corrigido em `cfSystemPrompt` (seção
  "vendas" e regras) e `cfExemplos` — ver `internal/llm/cloudflare_client.go`.
- **Nome de rua pode coincidir com nome de cidade**: mesma família de
  problema — a região tem ruas chamadas "Avenida Pelotas", "Rua Santa
  Maria", "Rua Alegrete" (nomes de cidades do RS), com risco de o modelo
  interpretar como o cliente falando de outra cidade em vez do próprio
  endereço. Corrigido junto com o caso acima, no mesmo padrão (regra +
  few-shot em `cfSystemPrompt`/`cfExemplos`).
- **Avisar que o boleto já está pago caiu em `cancelamento`**: caso real —
  "esse boleto ta pago" foi classificado como `cancelamento`, sem nenhum
  pedido de encerrar o serviço na mensagem. Hipótese: o modelo associou
  "reclamação sobre cobrança" a cancelamento em vez de financeiro. Corrigido
  com regra explícita ("avisar/contestar boleto pago = financeiro, só é
  cancelamento com pedido explícito de cancelar/encerrar") e exemplos
  few-shot em `cfSystemPrompt`/`cfExemplos`.
- **Monitoramento**: o dashboard "API MK Octadesk" no Grafana tem painéis de
  classificação por destino, erros por código, latência p95 da rota, e
  CPU/memória do host (via `node-exporter`) — útil pra acompanhar se o
  padrão de uso real se mantém parecido com o que foi medido.

## 7. Troubleshooting — problemas reais já encontrados

| Sintoma | Causa | Solução |
|---|---|---|
| `405 Method Not Allowed` no teste do Octadesk | O tipo de integração escolhido no flow builder do Octadesk só manda GET (sem campo de corpo) | Usar o tipo de integração que tem campo de Body/Payload e método configurável (POST) |
| `401 - Chave de acesso ausente ou inválida`, mesmo com a chave certa | `X-API-Key` configurado em **Params** (query string) em vez de **Headers** | Mover a chave pra seção de Headers do Octadesk |
| Resposta chega, mas com `llm_resposta_invalida` | A LLM devolveu um destino fora do `destinosValidos`, ou o novo destino foi adicionado no prompt mas esquecido na validação | Conferir `internal/llm/cloudflare_client.go` (seção 5.2) |
| `502 llm_indisponivel` mesmo com tudo aparentemente certo | `CLOUDFLARE_ACCOUNT_ID`/`CLOUDFLARE_API_TOKEN` ausentes ou inválidos (`config.Load` já falha o boot se estiverem ausentes; um token revogado/errado só aparece em runtime) | Conferir `.env` e o token no dashboard da Cloudflare |

## 8. Ideias pra depois (não implementadas)

- **Mensagens de áudio**: exigiria um passo de transcrição (ex.: Whisper)
  antes da classificação — o modelo não processa áudio nativamente. Seria
  um serviço novo na VM, mais carga de CPU, e depende do Octadesk conseguir
  enviar a URL/arquivo do áudio pra API (hoje só manda texto).
- **Ajuste fino do prompt com base em erros reais**: o prompt atual não é a
  versão final — vale revisar periodicamente com mensagens reais que o
  modelo classificou errado.

## 9. Histórico: migração de Ollama para Cloudflare Workers AI

Até 2026-09-15 a classificação rodava num modelo Ollama local
(`qwen2.5:3b-instruct`, 100% CPU nesta VM). Motivo da migração: baixo
desempenho/raciocínio do modelo 3B, e viés de classificar qualquer coisa
"com cara de endereço" (rua/cidade) como `endereco` em vez de `vendas`,
mesmo depois de ajustes de prompt.

**Uma primeira tentativa (branch `llm-cloudflare-workers-ai`, já deletada)
trocou o Ollama direto pela Cloudflare e foi abandonada**: a classificação
mandou clientes pro setor errado algumas vezes, e — separadamente — o
Ollama sobrecarregou a VM (Proxmox) por causa dos até 14 núcleos que
reservava pra si. Essa tentativa também usava um modelo "reasoning"
(`qwen3-30b-a3b-fp8`, sem suporte a JSON Mode nativo da Cloudflare),
dependendo de extrair `{...}` na marra do texto de resposta pra lidar com
um possível vazamento de raciocínio (`<think>...</think>`) antes do JSON.

**A segunda tentativa, adotada**: rota nova em paralelo
(`/v1/llm-cf-classifica-mensagem`), sem mexer na rota do Ollama, pra dar pra
comparar os dois antes de trocar o flow do Octadesk de vez. Modelo trocado
pra `@cf/meta/llama-3.1-8b-instruct-fp8-fast`: não é "reasoning" (sem risco
de vazar `<think>`) e tem suporte nativo a JSON Mode da Cloudflare
(`response_format: json_schema`), então a resposta é restringida por schema
em vez de depender só do prompt + parsing defensivo. Validada com sucesso
em produção (100% de acerto nos testes, latência ~0.3-0.9s vs. 3-4s+ do
Ollama) e adotada como único backend — a rota antiga
(`/v1/llm-classifica-mensagem`), o cliente Ollama (`internal/llm/client.go`)
e o `ollama/Modelfile` foram removidos, e o serviço `ollama` saiu do
`compose.yaml`.

Lições operacionais específicas do Ollama que já não se aplicam mais (janela
de contexto `num_ctx`, aquecimento a frio do modelo, limite de núcleos de
CPU) foram removidas deste documento — ver histórico do git
(`git log -- llm_expand.md`) se precisar recuperá-las.
