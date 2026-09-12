package config

import "testing"

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CHATBOT_API_KEY", "chave-com-mais-de-24-caracteres")
	t.Setenv("MK_BASE_URL", "https://sac.newlifefibra.com.br")
	t.Setenv("MK_USER_ACCESS_TOKEN", "token")
	t.Setenv("MK_WEBSERVICE_COUNTER_PASSWORD", "senha")
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
