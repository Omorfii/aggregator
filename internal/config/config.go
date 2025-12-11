package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Url         string `json:"db_url"`
	CurrentUser string `json:"current_user_name"`
}

const configFileName = ".gatorconfig.json"

func getConfigFilePath() (string, error) {

	homePath, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	truePath := filepath.Join(homePath, configFileName)

	return truePath, nil
}

func write(cfg Config) error {

	jsonData, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	filepath, err := getConfigFilePath()
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, jsonData, 0666)

}

func Read() (Config, error) {

	filepath, err := getConfigFilePath()
	if err != nil {
		return Config{}, err
	}

	jsonData, err := os.ReadFile(filepath)
	if err != nil {
		return Config{}, err
	}

	var config Config
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return Config{}, err
	}

	return config, nil
}

func (c *Config) SetUser(username string) error {

	c.CurrentUser = username

	return write(*c)
}
