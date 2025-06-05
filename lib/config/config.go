package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port         int    `json:"port"`
	Host         string `json:"host"`
	ReadTimeout  int    `json:"read_timeout_seconds"`
	WriteTimeout int    `json:"write_timeout_seconds"`
}

// ImageStoreConfig holds image store configuration
type ImageStoreConfig struct {
	TileSize     int    `json:"tile_size"`
	DatabasePath string `json:"database_path"`
}

// Config holds the complete application configuration
type Config struct {
	Server     ServerConfig     `json:"server"`
	ImageStore ImageStoreConfig `json:"image_store"`
	LogLevel   string           `json:"log_level"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:         8080,
			Host:         "localhost",
			ReadTimeout:  30,
			WriteTimeout: 30,
		},
		ImageStore: ImageStoreConfig{
			TileSize:     256,
			DatabasePath: "./imagestore.db",
		},
		LogLevel: "info",
	}
}

// LoadConfig loads configuration from a file, falling back to defaults
func LoadConfig(configPath string) (*Config, error) {
	config := DefaultConfig()

	// If config file doesn't exist, return defaults
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return config, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	err = json.Unmarshal(data, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// SaveConfig saves configuration to a file
func SaveConfig(config *Config, configPath string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(configPath)
	if dir != "" && dir != "." {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate server config
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Server.ReadTimeout <= 0 {
		return fmt.Errorf("invalid read timeout: %d", c.Server.ReadTimeout)
	}

	if c.Server.WriteTimeout <= 0 {
		return fmt.Errorf("invalid write timeout: %d", c.Server.WriteTimeout)
	}

	// Validate image store config
	if c.ImageStore.TileSize <= 0 {
		return fmt.Errorf("invalid tile size: %d", c.ImageStore.TileSize)
	}

	if c.ImageStore.DatabasePath == "" {
		return fmt.Errorf("database path cannot be empty")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}

	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid log level: %s", c.LogLevel)
	}

	return nil
}

// GetServerAddress returns the full server address
func (c *Config) GetServerAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// LoadConfigFromEnv loads configuration from environment variables
func LoadConfigFromEnv() *Config {
	config := DefaultConfig()

	// Server config from env
	if port := os.Getenv("SERVER_PORT"); port != "" {
		fmt.Sscanf(port, "%d", &config.Server.Port)
	}

	if host := os.Getenv("SERVER_HOST"); host != "" {
		config.Server.Host = host
	}

	if readTimeout := os.Getenv("SERVER_READ_TIMEOUT"); readTimeout != "" {
		fmt.Sscanf(readTimeout, "%d", &config.Server.ReadTimeout)
	}

	if writeTimeout := os.Getenv("SERVER_WRITE_TIMEOUT"); writeTimeout != "" {
		fmt.Sscanf(writeTimeout, "%d", &config.Server.WriteTimeout)
	}

	// Image store config from env
	if tileSize := os.Getenv("TILE_SIZE"); tileSize != "" {
		fmt.Sscanf(tileSize, "%d", &config.ImageStore.TileSize)
	}

	if dbPath := os.Getenv("DATABASE_PATH"); dbPath != "" {
		config.ImageStore.DatabasePath = dbPath
	}

	// Log level from env
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.LogLevel = logLevel
	}

	return config
}
