package adapters

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// APIConfig is what cmd/api needs: everything in Config, plus the model
// and the WhatsApp credentials. It is separate from Config so the CLI
// and the MCP server keep starting without a single WhatsApp variable
// set — they have no webhook and no API key, and asking for one would
// be asking for a secret that does not exist.
type APIConfig struct {
	Config

	Anthropic AnthropicConfig
	WhatsApp  WhatsAppConfig
}

type AnthropicConfig struct {
	APIKey string

	// Model is open on purpose: what matters is the quality of portion
	// estimates, so the right one is measured, not assumed.
	Model string

	// MaxToolIterations caps one message's tool-use loop. A runaway
	// loop is a bug, not an expense — but the bill is how anyone finds
	// out, so the cap is the fix.
	MaxToolIterations int
}

type WhatsAppConfig struct {
	VerifyToken   string
	AppSecret     string
	PhoneNumberID string
	AccessToken   string

	// AllowedNumbers is a closed list. Anyone who can send a message
	// can write to the pantry, which is why this is not optional.
	AllowedNumbers []string

	// DigestTemplate is the approved template the weekly digest goes
	// out as. Empty until phase D, and unused before it.
	DigestTemplate string
}

const (
	defaultModel             = "claude-opus-5-5"
	defaultMaxToolIterations = 6
)

func LoadAPIConfig() (APIConfig, error) {
	base, err := LoadConfig()
	if err != nil {
		return APIConfig{}, err
	}

	cfg := APIConfig{
		Config: base,
		Anthropic: AnthropicConfig{
			APIKey:            strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
			Model:             defaultModel,
			MaxToolIterations: defaultMaxToolIterations,
		},
		WhatsApp: WhatsAppConfig{
			VerifyToken:    strings.TrimSpace(os.Getenv("WHATSAPP_VERIFY_TOKEN")),
			AppSecret:      strings.TrimSpace(os.Getenv("WHATSAPP_APP_SECRET")),
			PhoneNumberID:  strings.TrimSpace(os.Getenv("WHATSAPP_PHONE_NUMBER_ID")),
			AccessToken:    strings.TrimSpace(os.Getenv("WHATSAPP_ACCESS_TOKEN")),
			AllowedNumbers: splitNumbers(os.Getenv("WHATSAPP_ALLOWED_NUMBERS")),
			DigestTemplate: strings.TrimSpace(os.Getenv("WHATSAPP_DIGEST_TEMPLATE")),
		},
	}

	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")); v != "" {
		cfg.Anthropic.Model = v
	}
	if v := strings.TrimSpace(os.Getenv("BOT_MAX_TOOL_ITERATIONS")); v != "" {
		iterations, err := strconv.Atoi(v)
		if err != nil {
			return APIConfig{}, fmt.Errorf("invalid BOT_MAX_TOOL_ITERATIONS %q", v)
		}
		if iterations < 1 || iterations > 20 {
			return APIConfig{}, fmt.Errorf("BOT_MAX_TOOL_ITERATIONS out of range: %d", iterations)
		}
		cfg.Anthropic.MaxToolIterations = iterations
	}

	for name, value := range map[string]string{
		"ANTHROPIC_API_KEY":        cfg.Anthropic.APIKey,
		"WHATSAPP_VERIFY_TOKEN":    cfg.WhatsApp.VerifyToken,
		"WHATSAPP_APP_SECRET":      cfg.WhatsApp.AppSecret,
		"WHATSAPP_PHONE_NUMBER_ID": cfg.WhatsApp.PhoneNumberID,
		"WHATSAPP_ACCESS_TOKEN":    cfg.WhatsApp.AccessToken,
	} {
		if value == "" {
			return APIConfig{}, errors.New("missing " + name)
		}
	}
	if len(cfg.WhatsApp.AllowedNumbers) == 0 {
		return APIConfig{}, errors.New("missing WHATSAPP_ALLOWED_NUMBERS")
	}

	return cfg, nil
}

// splitNumbers reads a comma-separated list. The bot compares digits
// only, so the formatting here is whatever is easiest to read.
func splitNumbers(value string) []string {
	numbers := []string{}
	for _, number := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(number); trimmed != "" {
			numbers = append(numbers, trimmed)
		}
	}
	return numbers
}
