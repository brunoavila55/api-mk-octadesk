# Guia para criar novas APIs de integração

Este documento usa a API `mk-consulta-cliente` como referência para criar outras integrações HTTP em Go. O objetivo é manter um padrão previsível de autenticação, validação, segurança, métricas, testes e publicação.

## 1. Comece pelo contrato da API

Antes de escrever código, defina:

- qual problema a nova API resolve;
- quem poderá chamá-la;
- método e caminho HTTP;
- parâmetros obrigatórios;
- formato da resposta de sucesso;
- erros esperados;
- serviço e endpoint correspondentes no MK;
- credenciais exigidas pelo MK;
- dados sensíveis envolvidos.

Exemplo:

```text
Objetivo: consultar faturas de um cliente
Método: GET
Caminho interno: /v1/faturas
Parâmetro: documento
Autenticação pública: X-API-Key
Serviço MK: confirmar na documentação do ERP
Resposta: JSON normalizado
```

Evite criar o código antes de confirmar o endpoint, o código do serviço e uma resposta real do MK no Insomnia. Nomes parecidos no ERP não garantem que os contratos sejam iguais.

## 2. Teste primeiro o MK no Insomnia

Crie duas requisições quando o serviço usar a autenticação padrão do MK.

### Autenticação

```text
GET {MK_BASE_URL}/mk/WSAutenticacao.rule
```

Parâmetros comuns:

| Nome | Valor |
|---|---|
| `sys` | `MK0` |
| `token` | token fixo do usuário |
| `password` | contrassenha do perfil de Webservice |
| `cd_servico` | código do serviço que será consumido |

Guarde o token temporário retornado apenas no ambiente local do Insomnia.

### Serviço desejado

```text
GET {MK_BASE_URL}/mk/ENDPOINT_DO_SERVICO.rule
```

Informe o token temporário e os parâmetros definidos na documentação do MK. Registre exemplos sanitizados de:

- sucesso;
- registro inexistente;
- parâmetro inválido;
- token inválido ou expirado;
- erro do MK;
- timeout.

Nunca coloque tokens, contrassenhas, documentos reais ou respostas com dados pessoais no Git.

## 3. Decida entre ampliar esta API ou criar outra

Amplie este projeto quando a nova operação:

- usar o mesmo MK e o mesmo modelo de autenticação;
- pertencer ao mesmo domínio de consulta de clientes;
- puder compartilhar implantação, disponibilidade e política de acesso.

Crie um serviço separado quando houver:

- credenciais ou ERP diferentes;
- requisitos de segurança distintos;
- carga ou disponibilidade muito diferente;
- responsáveis diferentes;
- risco de uma integração prejudicar as demais.

Na maioria das integrações pequenas do mesmo MK, adicionar uma nova rota e um novo método no cliente é mais simples do que manter vários containers quase idênticos.

## 4. Organização do código

Este projeto separa responsabilidades assim:

```text
cmd/api/                 inicialização do processo
internal/config/         variáveis de ambiente e validação
internal/document/       validação de CPF e CNPJ
internal/httpapi/        rotas, autenticação, métricas e respostas
internal/mk/             autenticação e comunicação com o MK
```

Para uma nova operação, normalmente será necessário:

1. criar os tipos de resposta em `internal/mk`;
2. implementar um método no cliente do MK;
3. criar um handler em `internal/httpapi`;
4. registrar a rota em `NewHandler`;
5. adicionar testes unitários e HTTP;
6. documentar o contrato no README.

## 5. Implemente o cliente do MK

O código HTTP externo deve ficar em `internal/mk`, não dentro do handler público.

Estrutura recomendada:

```go
type Invoice struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Value  string `json:"valor"`
}

func (client *Client) Invoices(ctx context.Context, document string) ([]Invoice, error) {
	token, err := client.tokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}

	// Montar a URL com net/url, nunca concatenar parâmetros manualmente.
	// Executar usando client.httpClient para respeitar o timeout.
	// Validar status HTTP e JSON antes de devolver o resultado.
	return nil, nil
}
```

Regras importantes:

- use `context.Context` em todas as chamadas externas;
- monte query strings com `url.Values`;
- defina `Accept: application/json`;
- use o cliente HTTP com timeout;
- limite o tamanho máximo da resposta;
- feche sempre `response.Body`;
- valide tanto o status HTTP quanto o campo de status do JSON do MK;
- não registre URL completa, porque ela pode conter tokens e documentos;
- normalize os nomes dos campos antes de responder ao consumidor.

Se a nova operação usar outro código de serviço, o token precisa ser obtido com esse código. Não reutilize automaticamente o token do serviço `6` sem confirmar que o perfil e o MK permitem isso. Uma evolução possível é fazer o provider manter cache por código de serviço.

## 6. Crie o handler público

O handler deve cuidar apenas de HTTP, validação de entrada e tradução de erros.

Exemplo conceitual:

```go
func handleInvoices(writer http.ResponseWriter, request *http.Request, client *mk.Client) {
	value := request.URL.Query().Get("documento")
	documentValue, err := document.NormalizeDocument(value)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "documento_invalido", "Informe um CPF ou CNPJ válido.")
		return
	}

	result, err := client.Invoices(request.Context(), documentValue)
	if err != nil {
		writeError(writer, http.StatusBadGateway, "mk_indisponivel", "Não foi possível consultar o ERP.")
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"status": "ok",
		"dados":  result,
	})
}
```

Registro da rota:

```go
mux.Handle("GET /v1/faturas", requireAPIKey(apiKey, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
	handleInvoices(writer, request, client)
})))
```

Use substantivos no plural nas rotas internas:

```text
GET /v1/clientes
GET /v1/faturas
GET /v1/contratos
GET /v1/conexoes
```

## 7. Padronize as respostas

Sucesso:

```json
{
  "status": "ok",
  "dados": {}
}
```

Erro:

```json
{
  "status": "erro",
  "erro": {
    "codigo": "documento_invalido",
    "mensagem": "Informe um CPF ou CNPJ válido."
  }
}
```

Códigos HTTP recomendados:

| HTTP | Uso |
|---:|---|
| `200` | consulta concluída |
| `400` | parâmetro ausente ou inválido |
| `401` | `X-API-Key` ausente ou incorreta |
| `404` | recurso realmente inexistente, quando o contrato exigir essa distinção |
| `429` | limite de requisições excedido |
| `502` | MK recusou ou devolveu resposta inválida |
| `504` | timeout ao consultar o MK |

Não devolva mensagens internas, URLs do MK, tokens ou stack traces.

## 8. Segurança obrigatória

Toda nova rota com dados de clientes deve:

- exigir `X-API-Key`;
- ser publicada somente com HTTPS;
- validar os parâmetros antes de chamar o MK;
- omitir query strings dos logs;
- evitar dados pessoais em labels do Prometheus;
- manter credenciais apenas no `.env` do servidor;
- limitar timeout e tamanho das respostas externas;
- executar no container sem root e com filesystem somente leitura;
- restringir o MK pelo IP de saída da API, quando possível.

Nunca envie ao chatbot:

- `MK_USER_ACCESS_TOKEN`;
- `MK_WEBSERVICE_COUNTER_PASSWORD`;
- token temporário retornado pelo MK.

O chatbot deve conhecer somente a chave pública desta API.

## 9. Variáveis de ambiente

Adicione ao `internal/config/config.go` qualquer nova configuração obrigatória e valide-a na inicialização.

Exemplo:

```text
MK_INVOICE_SERVICE_CODE=123
```

Atualize também:

- `.env.example`, sem valor secreto;
- tabela de configuração do README;
- testes de `internal/config`;
- ambiente real da VM.

Depois de alterar o `.env` na VM, recrie o container:

```bash
docker compose up -d --force-recreate api
```

Um simples `restart` não recarrega variáveis do Compose.

## 10. Testes mínimos

Cada nova operação deve testar:

- entrada válida;
- parâmetro ausente;
- formato inválido;
- ausência de `X-API-Key`;
- chave incorreta;
- resposta de sucesso do MK;
- erro HTTP do MK;
- JSON inválido;
- status de erro dentro de uma resposta HTTP `200`;
- timeout;
- token expirado, quando aplicável;
- ausência de credenciais e documentos nos logs e métricas.

Use `httptest.Server` para simular o MK. Os testes nunca devem depender do ERP real.

Execute:

```bash
gofmt -w ./cmd ./internal
go test ./...
go test -race ./...
```

O build Docker também executa os testes:

```bash
docker compose build api
```

## 11. Métricas e desempenho

As rotas registradas no mesmo handler já entram nas métricas HTTP:

```text
mk_consulta_cliente_http_requests_total
mk_consulta_cliente_http_request_duration_seconds
mk_consulta_cliente_http_in_flight_requests
```

Depois de criar uma rota, confirme:

```bash
curl http://127.0.0.1:8080/metrics
```

Se o Prometheus estiver na rede Docker `proxy`, conecte-o também à rede da API ou use uma rede interna compartilhada. O alvo deve usar o nome do serviço e a porta interna:

```yaml
static_configs:
  - targets:
      - api:8080
```

Nunca use CPF, CNPJ, contrato, telefone ou nome de cliente como label. Isso causaria vazamento de dados e crescimento ilimitado de séries.

## 12. Publicação com o Traefik existente

O Traefik da VM usa:

```text
rede Docker: proxy
entrypoint HTTPS: websecure
certresolver: letsencrypt
```

Para publicar uma nova rota externa, use labels no serviço da API. Exemplo para expor `/v1/faturas` sem alterar o caminho interno:

```yaml
networks:
  - proxy

labels:
  - traefik.enable=true
  - traefik.docker.network=proxy
  - "traefik.http.routers.mk-faturas.rule=Host(`api.newlifefibra.com.br`) && Path(`/v1/faturas`)"
  - traefik.http.routers.mk-faturas.entrypoints=websecure
  - traefik.http.routers.mk-faturas.tls=true
  - traefik.http.routers.mk-faturas.tls.certresolver=letsencrypt
  - traefik.http.services.mk-api.loadbalancer.server.port=8080
```

Se a rota pública tiver nome diferente da rota interna, use `replacePath`. Exemplo:

```yaml
  - "traefik.http.routers.mk-consulta.rule=Host(`api.newlifefibra.com.br`) && Path(`/v1/consulta-documento`)"
  - traefik.http.routers.mk-consulta.middlewares=mk-consulta-path
  - traefik.http.middlewares.mk-consulta-path.replacepath.path=/v1/clientes
```

Use nomes exclusivos para routers e middlewares. Dois recursos com o mesmo nome podem sobrescrever configurações no provider Docker.

A rede externa permanece:

```yaml
networks:
  proxy:
    external: true
    name: proxy
```

Não publique a porta `8080` no host. Use apenas `expose: 8080` e a rede compartilhada com o Traefik.

## 13. Validação após a publicação

Teste por camadas.

### Processo

```bash
docker compose ps
docker compose logs --tail=100 api
```

### Rede Docker

```bash
docker network inspect proxy \
  --format '{{range $id, $container := .Containers}}{{println $container.Name}}{{end}}'
```

### Roteamento, sem chave

```bash
curl -i https://api.newlifefibra.com.br/v1/NOVA-ROTA
```

O retorno `401` confirma DNS, HTTPS, Traefik e chegada à API.

### Validação, com chave e entrada inválida

```bash
curl --get \
  'https://api.newlifefibra.com.br/v1/NOVA-ROTA' \
  --data-urlencode 'documento=valor-invalido' \
  --header "X-API-Key: $CHATBOT_API_KEY"
```

O retorno `400` confirma que a autenticação passou e o handler foi executado.

### Fluxo completo

Teste com dados autorizados e verifique o `200`. Não coloque o valor real em prints, tickets ou commits.

## 14. Interpretação rápida de erros

| Resultado | Interpretação provável |
|---|---|
| DNS não resolve | registro DNS ausente ou não propagado |
| erro de certificado | portas, DNS ou ACME do Traefik |
| `404 page not found` | regra/path do Traefik ou rota interna incorreta |
| `401 nao_autorizado` | header ausente, nome errado ou chave diferente do container |
| `400 ..._invalido` | parâmetro ausente, variável do chatbot não vinculada ou valor inválido |
| `502 mk_indisponivel` | credenciais, serviço, IP permitido, resposta inválida ou indisponibilidade do MK |
| `504 mk_timeout` | MK demorou além de `MK_HTTP_TIMEOUT` |

## 15. Checklist para cada nova API

- [ ] Objetivo e consumidor definidos.
- [ ] Endpoint e código de serviço confirmados na documentação do MK.
- [ ] Sucesso e erros testados no Insomnia.
- [ ] Contrato HTTP escrito antes da implementação.
- [ ] Entrada validada antes da chamada externa.
- [ ] Cliente do MK separado do handler.
- [ ] Resposta normalizada e sem detalhes internos.
- [ ] Rota protegida por `X-API-Key`.
- [ ] Testes normais e `-race` aprovados.
- [ ] Métricas sem dados pessoais.
- [ ] README e `.env.example` atualizados.
- [ ] Container reconstruído e saudável.
- [ ] Router Traefik com nome exclusivo.
- [ ] Testes `401`, `400`, `200`, `502` e `504` realizados conforme aplicável.
- [ ] Nenhuma credencial ou dado real incluído no Git.

## 16. Modelo de solicitação para desenvolver a próxima API

Ao solicitar uma nova integração, forneça informações neste formato:

```text
Nome da operação:
Objetivo:
Quem consumirá:
Endpoint do MK:
Código do serviço:
Método HTTP do MK:
Parâmetros exigidos pelo MK:
Exemplo sanitizado de sucesso:
Exemplo sanitizado de erro:
Rota pública desejada:
Campos que devem ser devolvidos:
```

Com essas informações é possível implementar a nova rota sem adivinhar o contrato do ERP e mantendo o mesmo padrão operacional deste projeto.