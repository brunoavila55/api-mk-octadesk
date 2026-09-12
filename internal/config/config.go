package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                        string
	ChatbotAPIKey               string
	MKBaseURL                   *url.URL
	MKServiceCode               string
	MKTemporaryAuthToken        string
	MKUserAccessToken           string
	MKWebserviceCounterPassword string
	MKHTTPTimeout               time.Duration
	MKTemporaryAuthTokenTTL     time.Duration
}

func Load() (Config, error) {
	config := Config{
		Port:                        valueOrDefault("HTTP_PORT", "8080"),
		ChatbotAPIKey:               strings.TrimSpace(os.Getenv("CHATBOT_API_KEY")),
		MKServiceCode:               valueOrDefault("MK_SERVICE_CODE", "9999"),
		MKTemporaryAuthToken:        strings.TrimSpace(os.Getenv("MK_TEMPORARY_AUTH_TOKEN")),
		MKUserAccessToken:           strings.TrimSpace(os.Getenv("MK_USER_ACCESS_TOKEN")),
		MKWebserviceCounterPassword: strings.TrimSpace(os.Getenv("MK_WEBSERVICE_COUNTER_PASSWORD")),
	}

	if config.ChatbotAPIKey == "" {
		return Config{}, errors.New("CHATBOT_API_KEY é obrigatória")
	}
	if len(config.ChatbotAPIKey) < 24 {
		return Config{}, errors.New("CHATBOT_API_KEY deve ter pelo menos 24 caracteres")
	}

	baseURL, err := url.Parse(strings.TrimSpace(os.Getenv("MK_BASE_URL")))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return Config{}, errors.New("MK_BASE_URL deve ser uma URL absoluta válida")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return Config{}, errors.New("MK_BASE_URL deve usar o protocolo HTTP ou HTTPS")
	}
	if baseURL.Scheme == "http" && !environmentBool("MK_ALLOW_INSECURE_HTTP") {
		return Config{}, errors.New("o MK usa HTTP sem criptografia; confirme conscientemente com MK_ALLOW_INSECURE_HTTP=true")
	}
	config.MKBaseURL = baseURL

	if config.MKTemporaryAuthToken == "" && (config.MKUserAccessToken == "" || config.MKWebserviceCounterPassword == "") {
		return Config{}, errors.New("informe MK_USER_ACCESS_TOKEN e MK_WEBSERVICE_COUNTER_PASSWORD; alternativamente, informe MK_TEMPORARY_AUTH_TOKEN")
	}

	config.MKHTTPTimeout, err = durationOrDefault("MK_HTTP_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	config.MKTemporaryAuthTokenTTL, err = durationOrDefault("MK_TEMPORARY_AUTH_TOKEN_TTL", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}

	return config, nil
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s deve ser uma duração positiva", name)
	}
	return duration, nil
}

func environmentBool(name string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(name)))
	return err == nil && value
}
