package adapters

import (
	"errors"
	"os"
	"strings"
)

// DigestConfig is all cmd/digest needs: a database and a way to send.
//
// It is deliberately not APIConfig. The digest never serves a webhook
// and never calls the model — it reads the pantry and sends one
// template — so it asks for neither ANTHROPIC_API_KEY, nor the verify
// token, nor the app secret. A scheduled job that refuses to start
// without three secrets it does not use is a job that stops running the
// first time one of them is rotated.
type DigestConfig struct {
	DatabaseURL string

	PhoneNumberID string
	AccessToken   string

	// Recipients is the same closed list the bot answers. One
	// household: the people who may write to the pantry are the people
	// who get its shopping list.
	Recipients []string

	// Template is the approved template's name, and the one value here
	// that is allowed to be empty. Without it the digest is owed
	// rather than sent, which is the documented fallback and not a
	// misconfiguration.
	Template string
}

func LoadDigestConfig() (DigestConfig, error) {
	if strings.TrimSpace(os.Getenv("APP_ENV")) != string(EnvProd) {
		loadDotEnv()
	}

	cfg := DigestConfig{
		DatabaseURL:   databaseURL(),
		PhoneNumberID: strings.TrimSpace(os.Getenv("WHATSAPP_PHONE_NUMBER_ID")),
		AccessToken:   strings.TrimSpace(os.Getenv("WHATSAPP_ACCESS_TOKEN")),
		Recipients:    splitNumbers(os.Getenv("WHATSAPP_ALLOWED_NUMBERS")),
		Template:      strings.TrimSpace(os.Getenv("WHATSAPP_DIGEST_TEMPLATE")),
	}

	if cfg.DatabaseURL == "" {
		return DigestConfig{}, errors.New("missing DATABASE_URL_UNPOOLED or DATABASE_URL")
	}
	if cfg.PhoneNumberID == "" {
		return DigestConfig{}, errors.New("missing WHATSAPP_PHONE_NUMBER_ID")
	}
	if cfg.AccessToken == "" {
		return DigestConfig{}, errors.New("missing WHATSAPP_ACCESS_TOKEN")
	}
	if len(cfg.Recipients) == 0 {
		return DigestConfig{}, errors.New("missing WHATSAPP_ALLOWED_NUMBERS")
	}

	return cfg, nil
}
