# Tutorial: entendendo e expandindo a classificação por LLM

Este documento explica como funciona a classificação de mensagens via LLM
(`POST /v1/llm-classifica-mensagem`, Ollama, e `POST /v1/llm-cf-classifica-mensagem`,
Cloudflare Workers AI — ver seção 9), como ajustar o comportamento dela e
como aumentar o número de setores de destino. É complementar ao `README.md`
(que documenta o contrato das rotas) e ao `guia_criar_api.md` (que documenta
o padrão geral de APIs deste projeto).

## 1. Visão geral: como a peça se encaixa

```
Cliente manda mensagem no Octadesk
        |
        v
Flow do Octadesk chama POST /v1/llm-classifica-mensagem
        |  (só a mensagem do cliente, sem histórico)
        v
API Go (internal/httpapi + internal/llm)
        |  chama a API HTTP local do Ollama
        v
Ollama + modelo "atendimento-classificador"
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
- O Ollama roda num container à parte (`ollama`, no `compose.yaml`), sem
  exposição via Traefik — só a `api` fala com ele, pela rede Docker interna.

## 2. As peças por trás da rota

| Peça | Arquivo | O que faz |
|---|---|---|
| Prompt/exemplos da LLM (Ollama) | `ollama/Modelfile` | Define as regras de classificação, os setores e os exemplos few-shot. É o que "ensina" o modelo a classificar. |
| Cliente HTTP do Ollama | `internal/llm/client.go` | Monta a requisição pro Ollama (`/api/generate`), decodifica a resposta e valida se o `destino_principal` é um dos valores conhecidos (`destinosValidos`). |
| Prompt/exemplos da LLM (Cloudflare) | `internal/llm/cloudflare_client.go` (`cfSystemPrompt`, `cfExemplos`) | Mesma política de classificação do Modelfile, só que como mensagens de chat em vez de sintaxe Modelfile — ver seção 9. |
| Cliente HTTP da Cloudflare | `internal/llm/cloudflare_client.go` | Monta a requisição pro Workers AI (`/accounts/{id}/ai/run/{model}`, com JSON Mode/schema), decodifica e valida do mesmo jeito que o cliente do Ollama (reaproveita `destinosValidos`). |
| Handlers HTTP | `internal/httpapi/handlers.go` (`classificaMensagem`, `classificaMensagemCloudflare`) | Recebem o corpo JSON do Octadesk, validam a mensagem, chamam o cliente LLM correspondente, formatam a resposta pública. Compartilham a validação via `classificaMensagemVia`. |
| Métricas | `internal/httpapi/metrics.go` | `mk_octadesk_llm_classificacao_total{backend,destino}` e `mk_octadesk_llm_erros_total{backend,codigo}` — `backend` é `"ollama"` ou `"cloudflare"`, visíveis no Grafana. |
| Infra | `compose.yaml` | Sobe o container `ollama` com `OLLAMA_KEEP_ALIVE=-1` (nunca descarrega o modelo da memória). A Cloudflare não tem serviço próprio — é só uma API HTTP externa chamada pela `api`. |

## 3. Os 9 destinos hoje

Ver a tabela completa no `README.md` (seção "Classificação de mensagens via
LLM"). Resumo: `suporte`, `cancelamento`, `financeiro`, `titular`,
`renovacao`, `ampliacao`, `endereco`, `vendas`, `atendimento` — com
prioridade `suporte > cancelamento > financeiro > titular > renovacao >
ampliacao > endereco > vendas > atendimento` quando a mensagem tem mais
de uma intenção.

## 4. Como mudar o prompt ("a pergunta" que a LLM responde)

Isso é **só texto**, sem tocar em código Go. O arquivo é `ollama/Modelfile`:

```
FROM qwen2.5:3b-instruct

SYSTEM """
... regras, definições de setor, exemplos ...
"""

PARAMETER temperature 0
PARAMETER num_ctx 4096
PARAMETER num_predict 30

MESSAGE user """..."""
MESSAGE assistant """{"destino_principal":"..."}"""
```

Passo a passo pra alterar:

1. Edite o `ollama/Modelfile` (ajuste as regras da `SYSTEM`, adicione ou troque
   exemplos `MESSAGE user`/`MESSAGE assistant`).
2. Se a `SYSTEM` + os exemplos `MESSAGE` cresceram bastante (novo setor,
   muitos exemplos novos), confira se `PARAMETER num_ctx` ainda cobre o
   prompt inteiro — ver aviso na seção 6 antes de seguir, senão a próxima
   chamada pode dar timeout mesmo com o modelo "aquecido".
3. Recrie o modelo customizado no Ollama (isso sobrescreve a versão anterior):
   ```bash
   docker compose exec ollama ollama create atendimento-classificador -f /modelfiles/Modelfile
   ```
4. Teste direto no Ollama antes de liberar pro Octadesk, pra não gastar o
   "aquecimento" (ver seção 6) com um teste que pode falhar:
   ```bash
   docker compose exec ollama ollama run atendimento-classificador "sua mensagem de teste aqui"
   ```
5. Se o resultado bater com o esperado, teste pela API real:
   ```bash
   curl -s -X POST https://api.newlifefibra.com.br/v1/llm-classifica-mensagem \
     -H "X-API-Key: $CHATBOT_API_KEY" -H "Content-Type: application/json" \
     -d '{"mensagem": "sua mensagem de teste aqui"}'
   ```

Não precisa rebuildar nem reiniciar o container `api` — só o `ollama create`
já troca o comportamento do modelo (a API sempre chama pelo nome do modelo,
`OLLAMA_MODEL=atendimento-classificador`, então passa a usar a versão nova
automaticamente na próxima requisição).

### Dicas pra escrever bons exemplos

- Cada `MESSAGE user`/`MESSAGE assistant` é um exemplo few-shot — o modelo
  aprende o padrão de resposta esperado a partir deles. Mais exemplos bons
  geralmente ajudam mais que regras longas em texto corrido.
- Evite exemplos ambíguos com respostas "confiantes" — se uma mensagem é
  genuinamente ambígua, o exemplo correto é classificá-la como `atendimento`
  (ver a regra "não adivinhe" já presente no Modelfile).
- Depois de qualquer mudança, rode um punhado de mensagens reais (ou
  parecidas com reais) antes de considerar pronto — não tem teste automatizado
  de qualidade de classificação, só validação manual mesmo.

## 5. Como aumentar as saídas (adicionar um novo destino)

Diferente de mudar o prompt, isso **exige mudar código Go também**, porque a
lista de destinos válidos é travada na API por segurança (pra nunca repassar
pro Octadesk um valor que a LLM "inventou").

Exemplo: adicionar um destino `cancelamento`.

### 5.1 No `ollama/Modelfile`

1. Adicione uma nova seção de setor, no mesmo formato das existentes:
   ```
   ==================================================
   CANCELAMENTO
   ==================================================

   Classifique como CANCELAMENTO quando a mensagem estiver relacionada a:

   - cancelar o serviço
   - encerrar o contrato
   - não quero mais o serviço

   Exemplos:

   "quero cancelar minha internet"
   "não quero mais o serviço, pode encerrar"

   Destino: "cancelamento"
   ```
2. Decida onde ele entra na prioridade (seção "MÚLTIPLAS INTENÇÕES E
   PRIORIDADE") — por exemplo, entre `suporte` e `financeiro`, ou depois de
   todos, dependendo da urgência que você quer dar a esse setor.
3. Atualize a seção "FORMATO DE SAÍDA" pra incluir `"cancelamento"` na lista
   de valores possíveis de `destino_principal`.
4. Adicione pelo menos 1-2 exemplos `MESSAGE user`/`MESSAGE assistant` pro
   novo destino, pro modelo aprender o padrão.

### 5.2 No código Go (`internal/llm/client.go`)

Adicione o novo valor ao mapa de validação:

```go
var destinosValidos = map[string]bool{
	"vendas":       true,
	"financeiro":   true,
	"suporte":      true,
	"atendimento":  true,
	"cancelamento": true, // novo
}
```

Sem essa mudança, mesmo que a LLM classifique corretamente como
`"cancelamento"`, a API vai rejeitar como `llm_resposta_invalida` (502) por
não reconhecer o valor — essa validação existe de propósito, pra nunca
repassar ao Octadesk um destino que ele não sabe tratar.

`destinosValidos` é compartilhado pelos dois clientes (Ollama e Cloudflare),
então essa parte só precisa ser editada uma vez. **Mas** o novo destino
também precisa ser adicionado em três lugares que não são compartilhados:
o `ollama/Modelfile` (regras + exemplos), `cfSystemPrompt`/`cfExemplos` em
`internal/llm/cloudflare_client.go` (mesma coisa, formato de mensagem de
chat) e `destinosValidosOrdenados` no mesmo arquivo (usado no `enum` do JSON
Schema que restringe a resposta da Cloudflare). Esquecer um dos dois prompts
faz um dos backends ficar defasado do outro sem erro nenhum — só classificação
divergente entre as duas rotas.

### 5.3 Rebuild e deploy

Diferente de só mudar o prompt, isso precisa recompilar e reimplantar a API:

```bash
docker compose build api
docker compose up -d --force-recreate api
```

E recriar o modelo no Ollama com o Modelfile atualizado (mesmo comando da
seção 4).

### 5.4 Depois de implantar

- Atualize a tabela de destinos no `README.md`.
- Configure o novo ramo no if/else do flow do Octadesk.
- Teste ponta a ponta (Ollama → API → Octadesk) antes de considerar pronto.

### 5.5 Cuidado: mais categorias = mais difícil pro modelo

Um modelo de 3B parâmetros tem limite de quantas categorias consegue
distinguir com confiança, principalmente com mensagens curtas e informais.
Cada novo destino deveria vir com exemplos claros e, idealmente, alguns
exemplos "negativos" (mensagens parecidas que NÃO são desse destino, pra
reforçar a fronteira de decisão). Teste bastante antes de liberar em
produção — não existe teste automatizado de qualidade, só validação manual.

## 6. Operação: o que já aprendemos rodando isso em produção

- **O modelo precisa ficar sempre carregado.** `OLLAMA_KEEP_ALIVE=-1` no
  `compose.yaml` garante isso — sem essa variável, o Ollama descarrega o
  modelo da memória depois de 5 min sem uso (padrão), e a próxima mensagem
  paga o custo de recarregar (~2GB de tensores).
- **Toda vez que o processo do Ollama reinicia** (deploy, restart do
  container, reboot da VM), a primeira classificação depois disso é lenta
  (60-90s), porque o prompt do Modelfile (~1700 tokens de sistema + exemplos)
  precisa ser reprocessado do zero — o cache de prompt do llama.cpp fica
  vazio até a primeira chamada. Depois dessa primeira chamada, as seguintes
  ficam rápidas (~3-4s) enquanto o processo não reiniciar de novo.
  **Sempre "aquente" manualmente depois de qualquer restart do `ollama`:**
  ```bash
  docker compose exec ollama ollama run atendimento-classificador "teste de aquecimento"
  ```
  Assim quem paga esse custo é você, não um cliente real.
- **`num_ctx` precisa crescer junto com o prompt.** O `num_ctx 2048`
  original foi calibrado pro prompt com 4 setores (~1700 tokens). Ao
  adicionar os 5 setores extras (`renovacao`, `ampliacao`, `endereco`,
  `titular`, `cancelamento`) com suas seções de regras e ~15 exemplos
  few-shot novos, o Modelfile praticamente triplicou de tamanho e passou a
  estourar a janela de 2048 tokens. Isso não deu erro explícito — deu
  **timeout 504 (`llm_timeout`) constante em produção**, mesmo com o modelo
  já "aquecido" e respondendo rápido via `ollama run` direto na CLI, porque
  o llama.cpp ficava reprocessando/descartando parte do contexto a cada
  chamada HTTP. A correção foi subir `num_ctx` pra 8192 (o host tem RAM de
  sobra pra isso — confira com `free -h` antes de decidir o valor) e recriar
  o modelo. **Sempre que adicionar setor(es) novo(s) ou vários exemplos de
  uma vez, confira se o prompt ainda cabe no `num_ctx` antes de dar como
  pronto** — não existe aviso automático, só o sintoma de timeout depois do
  deploy.
- **Depois de mudar `num_ctx`, o aquecimento a frio demora mais.** Contexto
  maior = mais tokens de prompt pra processar no prefill, que nesse host é
  100% CPU (sem GPU). Na prática, o aquecimento depois de subir de 2048 pra
  8192 levou ~3min (contra os 55-90s do prompt antigo, menor). É esperado —
  não é sinal de problema, só reflete o tamanho do prompt atual.
- **O Modelfile foi enxugado depois do incidente acima.** A versão que
  estourou o `num_ctx 2048` tinha ~14,2 mil caracteres, com bastante regra
  repetida (cada setor explicado no bloco de setor, de novo em "confiança e
  cautela", de novo em "regras finais"). Reescrito num formato mais direto
  (uma linha por setor, uma lista de regras só, sem separadores decorativos
  nem instruções negativas redundantes — o `format: "json"` do Ollama já
  força sintaxe JSON, não precisa pedir "não escreva Markdown" etc.),
  mantendo os 26 exemplos few-shot, o arquivo caiu pra ~4,8 mil caracteres
  (-66%). Com esse tamanho, `num_ctx 4096` já dá margem confortável (~2x o
  uso estimado) — não precisou voltar pro 2048 original nem manter o 8192.
  Também foi adicionado `PARAMETER num_predict 30`, que limita o máximo de
  tokens gerados (a saída é sempre um JSON minúsculo, então não há motivo
  pra deixar sem teto). **Lição**: prompt longo com regra repetida não é só
  desperdício de contexto, é a causa raiz do incidente de `num_ctx` — vale
  revisar por redundância antes de simplesmente aumentar `num_ctx` de novo
  da próxima vez que o prompt crescer.
- **Nomes de destino podem mudar independente do conteúdo do Modelfile.**
  Nessa mesma revisão, `trocaendereco` virou `endereco` e `trocatitular`
  virou `titular` (nomes mais curtos, mesmo significado). Renomear um
  destino existente exige os mesmos passos de adicionar um novo (seção 5):
  atualizar `ollama/Modelfile` (todas as menções, incluindo `MESSAGE`),
  `internal/llm/client.go` (`destinosValidos`), `README.md`, recriar o
  modelo no Ollama e rebuildar/redeployar a `api`. **E mais um passo que só
  existe pra rename, não pra destino novo**: o flow do Octadesk que já
  estava configurado pra ler o valor antigo (`trocaendereco`/`trocatitular`)
  precisa ser atualizado pro nome novo — sem isso, o if/else do flow não
  reconhece o valor novo e a conversa cai no branch errado (ou nenhum).
- **Capacidade real, medida com teste de carga usando dados reais do pior
  dia do mês**: ~3-4s por classificação, 100% serializado (o Ollama usa
  todos os núcleos da CPU disponíveis numa única requisição, não roda duas em
  paralelo). No pior minuto do pior dia observado (considerando atendimentos
  distintos, não linhas brutas de log do MK), a latência sobe para 10-15s
  no p90, mas sem nenhum timeout — folga confortável em relação aos 60s de
  timeout configurado.
- **CPU do Ollama é limitada a 14 dos 16 núcleos da VM** (`deploy.resources.
  limits.cpus: "14"` no `compose.yaml`), porque a VM roda outras APIs além
  desta. Sem esse limite, o Ollama consome 100% de todos os núcleos durante
  uma classificação (medido: ~1600% de CPU com 16 núcleos livres), o que
  derrubaria a performance das outras APIs no pior minuto. Isso deixa a
  classificação um pouco mais lenta em teoria (2 núcleos a menos), mas ainda
  dentro da folga observada nos testes de carga. Se o limite mudar (VM
  ganhar/perder núcleos, outras APIs saírem), reajuste esse valor.
- **Monitoramento**: o dashboard "API MK Octadesk" no Grafana tem painéis de
  classificação por destino, erros por código, latência p95 da rota, e
  CPU/memória do host (via `node-exporter`) — útil pra acompanhar se o
  padrão de uso real se mantém parecido com o que foi medido.

## 7. Troubleshooting — problemas reais já encontrados

| Sintoma | Causa | Solução |
|---|---|---|
| `405 Method Not Allowed` no teste do Octadesk | O tipo de integração escolhido no flow builder do Octadesk só manda GET (sem campo de corpo) | Usar o tipo de integração que tem campo de Body/Payload e método configurável (POST) |
| `401 - Chave de acesso ausente ou inválida`, mesmo com a chave certa | `X-API-Key` configurado em **Params** (query string) em vez de **Headers** | Mover a chave pra seção de Headers do Octadesk |
| Primeira mensagem depois de um deploy/restart demora ~1 min e falha | Ollama processando o prompt do zero (cache vazio) — ver seção 6 | Rodar o "aquecimento" manual antes de liberar o teste real |
| Resposta chega, mas com `llm_resposta_invalida` | A LLM devolveu um destino fora do `destinosValidos`, ou o novo destino foi adicionado no Modelfile mas esquecido no Go | Conferir `internal/llm/client.go` (seção 5.2) |
| `504`/`llm_timeout` constante depois de adicionar setor(es) novo(s), mesmo já tendo "aquentado" o modelo | Prompt do Modelfile cresceu além do `num_ctx` configurado — ver seção 6 | Aumentar `PARAMETER num_ctx` pra cobrir o prompt (com folga); se o prompt tiver regra repetida, vale enxugar o texto antes de só aumentar `num_ctx` — recriar o modelo (`ollama create`) e aquecer de novo em qualquer um dos dois casos |

## 8. Ideias pra depois (não implementadas)

- **Mensagens de áudio**: exigiria um passo de transcrição (ex.: Whisper)
  antes da classificação — o Ollama não processa áudio nativamente. Seria
  um serviço novo na VM, mais carga de CPU, e depende do Octadesk conseguir
  enviar a URL/arquivo do áudio pra API (hoje só manda texto).
- **Ajuste fino do Modelfile com base em erros reais**: o Modelfile atual é
  a primeira versão validada, não a versão final — vale revisar
  periodicamente com mensagens reais que o modelo classificou errado.

## 9. Migração para Cloudflare Workers AI (rota paralela, em validação)

O Ollama roda 100% em CPU nesta VM e o modelo de 3B às vezes classifica
errado; o objetivo aqui é trocar por um modelo maior/melhor rodando na
Cloudflare, sem repetir dois problemas de uma tentativa anterior:

1. **Uma primeira tentativa (branch `llm-cloudflare-workers-ai`, já
   deletada) trocou o Ollama direto pela Cloudflare e foi abandonada**: a
   classificação mandou clientes pro setor errado algumas vezes, e —
   separadamente — o Ollama sobrecarregou a VM (Proxmox) por causa dos até
   14 núcleos que ele reserva pra si (`compose.yaml`), o que não tem relação
   com a chamada à Cloudflare em si. Essa tentativa também usava um modelo
   "reasoning" (`qwen3-30b-a3b-fp8`, sem suporte a JSON Mode nativo da
   Cloudflare), dependendo de extrair `{...}` na marra do texto de resposta
   pra lidar com um possível vazamento de raciocínio (`<think>...</think>`)
   antes do JSON.
2. Desta vez: **rota nova em paralelo** (`/v1/llm-cf-classifica-mensagem`),
   sem mexer na rota do Ollama, pra dar pra comparar os dois antes de trocar
   o flow do Octadesk de vez — ver o plano de corte no `README.md`.
   Modelo trocado pra `@cf/meta/llama-3.1-8b-instruct-fp8-fast`: não é
   "reasoning" (sem risco de vazar `<think>`) e tem suporte nativo a JSON
   Mode da Cloudflare (`response_format: json_schema`), então a resposta é
   restringida por schema em vez de depender só do prompt + parsing
   defensivo.

**Orçamento de Neurons**: a Cloudflare cobra em "Neurons", com free tier de
10.000/dia (reseta 00:00 UTC; ao estourar, bloqueia em vez de cobrar — só
cobra excedente no plano pago). Com ~250 classificações/dia e o modelo
escolhido, a estimativa fica bem abaixo do limite — não é o fator limitante
aqui. Se algum dia isso mudar (volume muito maior, ou troca pra um modelo
mais caro), vale reduzir o tamanho do prompt (mesma lição da seção 6: prompt
grande = mais tokens = mais Neurons) antes de simplesmente aceitar o
bloqueio ou migrar pro plano pago.

**Timeout**: `CLOUDFLARE_HTTP_TIMEOUT` (padrão 45s, ver `.env.example`) é
mais folgado que os 30s que a tentativa anterior usou sem validar em
produção — mas também não precisa ser tão alto quanto os 60s do Ollama,
porque a Cloudflare mantém os modelos do catálogo oficial sempre residentes
("sempre quentes", sem o custo de aquecimento a frio descrito na seção 6
pro Ollama). Ajustar com base na latência real observada (Grafana) depois
de um tempo em produção.

**Quando desligar o Ollama**: depois que a rota `-cf-` estiver validada e o
flow do Octadesk já estiver usando ela, pare o serviço `ollama` no
`compose.yaml` (ou remova o serviço e os arquivos relacionados, se não
houver interesse em mantê-lo como plano B). Enquanto isso não acontece, as
duas rotas ficam ativas e o Ollama continua consumindo CPU normalmente.
