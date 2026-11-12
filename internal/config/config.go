package config

import (
	"errors"
	"os"
)

const (
	defaultListenAddr = ":8080"
	defaultDataDir    = "./data"
)

// Config contains the server settings loaded from the environment.
type Config struct {
	ClientID      string
	PrivateKey    string
	WebhookSecret string
	ListenAddr    string
	DataDir       string
}

// Load reads and validates the server configuration.
func Load() (Config, error) {
	c := Config{
		ClientID:      os.Getenv("GITHUB_CLIENT_ID"),
		PrivateKey:    os.Getenv("GITHUB_APP_PRIVATE_KEY"),
		WebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		ListenAddr:    os.Getenv("LISTEN_ADDR"),
		DataDir:       os.Getenv("DATA_DIR"),
	}

	keyFile := os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE")
	if c.PrivateKey != "" && keyFile != "" {
		return c, errors.New("set only one private-key source")
	}
	if keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return c, errors.New("could not read private-key file")
		}
		c.PrivateKey = string(data)
	}

	if c.ClientID == "" || c.PrivateKey == "" || c.WebhookSecret == "" {
		return c, errors.New("GITHUB_CLIENT_ID, a private key, and GITHUB_WEBHOOK_SECRET are required")
	}
	if c.ListenAddr == "" {
		c.ListenAddr = defaultListenAddr
	}
	if c.DataDir == "" {
		c.DataDir = defaultDataDir
	}

	return c, nil
}
