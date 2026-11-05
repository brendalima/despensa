package adapters

import (
	"errors"
	"os"
	"strings"
)

// MCPConfig is all cmd/mcp needs. It is deliberately not Config: the MCP
// server has no port to listen on and holds no API key — the model calls
// come from the host, on the person's own subscription — so asking for
// either would be asking for a secret that does not exist.
type MCPConfig struct {
	DatabaseURL string
}

// LoadMCPConfig reads the environment. The host launches this binary
// with the variables in its own configuration, so a missing connection
// string is the one error worth a clear message.
func LoadMCPConfig() (MCPConfig, error) {
	if strings.TrimSpace(os.Getenv("APP_ENV")) != string(EnvProd) {
		loadDotEnv()
	}

	url := databaseURL()
	if url == "" {
		return MCPConfig{}, errors.New("missing DATABASE_URL_UNPOOLED or DATABASE_URL")
	}
	return MCPConfig{DatabaseURL: url}, nil
}
