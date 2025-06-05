package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", config.Server.Port)
	}

	if config.Server.Host != "localhost" {
		t.Errorf("expected default host 'localhost', got %s", config.Server.Host)
	}

	if config.ImageStore.TileSize != 256 {
		t.Errorf("expected default tile size 256, got %d", config.ImageStore.TileSize)
	}

	if config.LogLevel != "info" {
		t.Errorf("expected default log level 'info', got %s", config.LogLevel)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid port - zero",
			config: &Config{
				Server: ServerConfig{Port: 0, Host: "localhost", ReadTimeout: 30, WriteTimeout: 30},
				ImageStore: ImageStoreConfig{TileSize: 256, DatabasePath: "./test.db"},
				LogLevel: "info",
			},
			wantErr: true,
		},
		{
			name: "invalid port - too high",
			config: &Config{
				Server: ServerConfig{Port: 65536, Host: "localhost", ReadTimeout: 30, WriteTimeout: 30},
				ImageStore: ImageStoreConfig{TileSize: 256, DatabasePath: "./test.db"},
				LogLevel: "info",
			},
			wantErr: true,
		},
		{
			name: "invalid read timeout",
			config: &Config{
				Server: ServerConfig{Port: 8080, Host: "localhost", ReadTimeout: 0, WriteTimeout: 30},
				ImageStore: ImageStoreConfig{TileSize: 256, DatabasePath: "./test.db"},
				LogLevel: "info",
			},
			wantErr: true,
		},
		{
			name: "invalid tile size",
			config: &Config{
				Server: ServerConfig{Port: 8080, Host: "localhost", ReadTimeout: 30, WriteTimeout: 30},
				ImageStore: ImageStoreConfig{TileSize: 0, DatabasePath: "./test.db"},
				LogLevel: "info",
			},
			wantErr: true,
		},
		{
			name: "empty database path",
			config: &Config{
				Server: ServerConfig{Port: 8080, Host: "localhost", ReadTimeout: 30, WriteTimeout: 30},
				ImageStore: ImageStoreConfig{TileSize: 256, DatabasePath: ""},
				LogLevel: "info",
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			config: &Config{
				Server: ServerConfig{Port: 8080, Host: "localhost", ReadTimeout: 30, WriteTimeout: 30},
				ImageStore: ImageStoreConfig{TileSize: 256, DatabasePath: "./test.db"},
				LogLevel: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetServerAddress(t *testing.T) {
	config := &Config{
		Server: ServerConfig{
			Port: 9090,
			Host: "0.0.0.0",
		},
	}

	expected := "0.0.0.0:9090"
	if addr := config.GetServerAddress(); addr != expected {
		t.Errorf("expected address %s, got %s", expected, addr)
	}
}

func TestLoadConfigFromNonExistentFile(t *testing.T) {
	config, err := LoadConfig("nonexistent.json")
	if err != nil {
		t.Errorf("expected no error for nonexistent file, got %v", err)
	}

	defaultConfig := DefaultConfig()
	if config.Server.Port != defaultConfig.Server.Port {
		t.Errorf("expected default config when file doesn't exist")
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.json")

	testConfig := &Config{
		Server: ServerConfig{
			Port:         9000,
			Host:         "test.local",
			ReadTimeout:  60,
			WriteTimeout: 60,
		},
		ImageStore: ImageStoreConfig{
			TileSize:     512,
			DatabasePath: "/tmp/test.db",
		},
		LogLevel: "debug",
	}

	err := SaveConfig(testConfig, configPath)
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loadedConfig, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loadedConfig.Server.Port != testConfig.Server.Port {
		t.Errorf("port mismatch: expected %d, got %d", testConfig.Server.Port, loadedConfig.Server.Port)
	}

	if loadedConfig.Server.Host != testConfig.Server.Host {
		t.Errorf("host mismatch: expected %s, got %s", testConfig.Server.Host, loadedConfig.Server.Host)
	}

	if loadedConfig.ImageStore.TileSize != testConfig.ImageStore.TileSize {
		t.Errorf("tile size mismatch: expected %d, got %d", testConfig.ImageStore.TileSize, loadedConfig.ImageStore.TileSize)
	}

	if loadedConfig.LogLevel != testConfig.LogLevel {
		t.Errorf("log level mismatch: expected %s, got %s", testConfig.LogLevel, loadedConfig.LogLevel)
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "invalid.json")

	err := os.WriteFile(configPath, []byte("invalid json"), 0644)
	if err != nil {
		t.Fatalf("failed to write invalid config file: %v", err)
	}

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestSaveConfigCreatesDirectory(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "subdir", "config.json")

	config := DefaultConfig()
	err := SaveConfig(config, configPath)
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	originalValues := make(map[string]string)
	envVars := map[string]string{
		"SERVER_PORT":         "9999",
		"SERVER_HOST":         "example.com",
		"SERVER_READ_TIMEOUT": "45",
		"TILE_SIZE":           "128",
		"DATABASE_PATH":       "/custom/path.db",
		"LOG_LEVEL":           "warn",
	}

	for key, value := range envVars {
		originalValues[key] = os.Getenv(key)
		os.Setenv(key, value)
	}

	defer func() {
		for key, originalValue := range originalValues {
			if originalValue == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, originalValue)
			}
		}
	}()

	config := LoadConfigFromEnv()

	if config.Server.Port != 9999 {
		t.Errorf("expected port 9999, got %d", config.Server.Port)
	}

	if config.Server.Host != "example.com" {
		t.Errorf("expected host 'example.com', got %s", config.Server.Host)
	}

	if config.Server.ReadTimeout != 45 {
		t.Errorf("expected read timeout 45, got %d", config.Server.ReadTimeout)
	}

	if config.ImageStore.TileSize != 128 {
		t.Errorf("expected tile size 128, got %d", config.ImageStore.TileSize)
	}

	if config.ImageStore.DatabasePath != "/custom/path.db" {
		t.Errorf("expected database path '/custom/path.db', got %s", config.ImageStore.DatabasePath)
	}
}

func TestJSONMarshaling(t *testing.T) {
	config := DefaultConfig()
	
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	var unmarshaled Config
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}

	if unmarshaled.Server.Port != config.Server.Port {
		t.Errorf("port mismatch after JSON round-trip")
	}

	if unmarshaled.LogLevel != config.LogLevel {
		t.Errorf("log level mismatch after JSON round-trip")
	}
}