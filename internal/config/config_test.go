package config

import (
	"testing"
	"time"
)

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CHATBOT_API_KEY", "chave-com-mais-de-24-caracteres")
	t.Setenv("MK_BASE_URL", "https://sac.newlifefibra.com.br")
	t.Setenv("MK_USER_ACCESS_TOKEN", "token")
	t.Setenv("MK_WEBSERVICE_COUNTER_PASSWORD", "senha")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "conta-teste")
	t.Setenv("CLOUDFLARE_API_TOKEN", "token-teste")
}

func TestLoad_ConfiguracaoValida(t *testing.T) {
	setBaseEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("esperava sucesso, obteve erro: %v", err)
	}
	if cfg.Port != "8080" {
		t.Fatalf("esperava porta padrão 8080, obteve %s", cfg.Port)
	}
	if cfg.MKServiceCode != "9999" {
		t.Fatalf("esperava MK_SERVICE_CODE padrão 9999, obteve %s", cfg.MKServiceCode)
	}
	if cfg.OllamaModel != "atendimento-classificador" {
		t.Fatalf("esperava OLLAMA_MODEL padrão atendimento-classificador, obteve %s", cfg.OllamaModel)
	}
	if cfg.LLMHTTPTimeout != 60*time.Second {
		t.Fatalf("esperava LLM_HTTP_TIMEOUT padrão de 60s, obteve %s", cfg.LLMHTTPTimeout)
	}
	if cfg.CloudflareAIModel != "@cf/meta/llama-3.1-8b-instruct-fp8-fast" {
		t.Fatalf("esperava CLOUDFLARE_AI_MODEL padrão @cf/meta/llama-3.1-8b-instruct-fp8-fast, obteve %s", cfg.CloudflareAIModel)
	}
	if cfg.CloudflareAIBaseURL.String() != "https://api.cloudflare.com/client/v4/" {
		t.Fatalf("esperava CLOUDFLARE_AI_BASE_URL padrão https://api.cloudflare.com/client/v4/, obteve %s", cfg.CloudflareAIBaseURL)
	}
	if cfg.CloudflareHTTPTimeout != 45*time.Second {
		t.Fatalf("esperava CLOUDFLARE_HTTP_TIMEOUT padrão de 45s, obteve %s", cfg.CloudflareHTTPTimeout)
	}
}

func TestLoad_SemChatbotAPIKey(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("CHATBOT_API_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por falta de CHATBOT_API_KEY")
	}
}

func TestLoad_ChatbotAPIKeyCurta(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("CHATBOT_API_KEY", "curta")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por CHATBOT_API_KEY curta")
	}
}

func TestLoad_MKBaseURLInvalida(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MK_BASE_URL", "não-é-uma-url")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por MK_BASE_URL inválida")
	}
}

func TestLoad_MKBaseURLHTTPSemFlag(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MK_BASE_URL", "http://sac.newlifefibra.com.br")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por HTTP sem MK_ALLOW_INSECURE_HTTP")
	}
}

func TestLoad_SemCredenciaisMK(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MK_USER_ACCESS_TOKEN", "")
	t.Setenv("MK_WEBSERVICE_COUNTER_PASSWORD", "")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por falta de credenciais MK")
	}
}

func TestLoad_TokenTemporarioSubstituiCredenciais(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MK_USER_ACCESS_TOKEN", "")
	t.Setenv("MK_WEBSERVICE_COUNTER_PASSWORD", "")
	t.Setenv("MK_TEMPORARY_AUTH_TOKEN", "token-fixo")

	if _, err := Load(); err != nil {
		t.Fatalf("esperava sucesso com token temporário fixo, obteve erro: %v", err)
	}
}

func TestLoad_DuracaoInvalida(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MK_HTTP_TIMEOUT", "não-é-uma-duração")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por MK_HTTP_TIMEOUT inválido")
	}
}

func TestLoad_OllamaBaseURLInvalida(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("OLLAMA_BASE_URL", "não-é-uma-url")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por OLLAMA_BASE_URL inválida")
	}
}

func TestLoad_LLMHTTPTimeoutInvalido(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LLM_HTTP_TIMEOUT", "não-é-uma-duração")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por LLM_HTTP_TIMEOUT inválido")
	}
}

// Sem CLOUDFLARE_ACCOUNT_ID/CLOUDFLARE_API_TOKEN, Load() precisa continuar
// funcionando (não pode derrubar a API inteira, Ollama incluído, só porque a
// Cloudflare ainda não foi configurada) — só a rota nova fica indisponível,
// com um erro específico (llm.ErrNaoConfigurado) tratado em tempo de uso.
func TestLoad_SemCredenciaisCloudflareContinuaFuncionando(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("esperava sucesso mesmo sem credenciais da Cloudflare, obteve erro: %v", err)
	}
	if cfg.CloudflareAccountID != "" || cfg.CloudflareAPIToken != "" {
		t.Fatalf("esperava CloudflareAccountID/CloudflareAPIToken vazios, obteve %q/%q", cfg.CloudflareAccountID, cfg.CloudflareAPIToken)
	}
}

func TestLoad_CloudflareAIBaseURLInvalida(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("CLOUDFLARE_AI_BASE_URL", "não-é-uma-url")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por CLOUDFLARE_AI_BASE_URL inválida")
	}
}

func TestLoad_CloudflareHTTPTimeoutInvalido(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("CLOUDFLARE_HTTP_TIMEOUT", "não-é-uma-duração")

	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por CLOUDFLARE_HTTP_TIMEOUT inválido")
	}
}
