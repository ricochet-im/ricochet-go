package core

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"path/filepath"
)

type Config struct {
}

type ConfigRoot struct {
	Contacts map[string]ConfigContact
	Identity ConfigIdentity
}

type ConfigContact struct {
	Hostname      string
	LastConnected string
	Nickname      string
	WhenCreated   string
}

type ConfigIdentity struct {
	DataDirectory     string
	HostnameBlacklist []string
	ServiceKey        string
}

func LoadConfig(configPath string) (*Config, error) {
	configFile := filepath.Join(configPath, "ricochet.json")
	configData, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Printf("Config read error from %s: %v", configFile, err)
		return nil, err
	}

	var root ConfigRoot
	if err := json.Unmarshal(configData, &root); err != nil {
		log.Printf("Config parse error: %v", err)
		return nil, err
	}

	log.Printf("Config: %v", root)

	return &Config{}, nil
}
