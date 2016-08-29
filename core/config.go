package core

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	path     string
	filePath string

	root  ConfigRoot
	mutex sync.RWMutex
}

type ConfigRoot struct {
	Contacts map[string]ConfigContact
	Identity ConfigIdentity

	// Not used by the permanent instance in Config
	writable bool
	config   *Config
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
	config := &Config{
		path:     configPath,
		filePath: filepath.Join(configPath, "notricochet.json"),
	}

	data, err := ioutil.ReadFile(config.filePath)
	if err != nil {
		log.Printf("Config read error from %s: %v", config.filePath, err)
		return nil, err
	}

	if err := json.Unmarshal(data, &config.root); err != nil {
		log.Printf("Config parse error: %v", err)
		return nil, err
	}

	log.Printf("Config: %v", config.root)
	return config, nil
}

// Return a read-only snapshot of the current configuration. This object
// _must not_ be stored, and you must call Close() when finished with it.
// This function may block.
func (c *Config) OpenRead() *ConfigRoot {
	c.mutex.RLock()
	root := c.root
	root.writable = false
	root.config = c
	return &root
}

// Return a writable snapshot of the current configuration. This object
// _must not_ be stored, and you must call Save() or Discard() when
// finished with it. This function may block.
func (c *Config) OpenWrite() *ConfigRoot {
	c.mutex.Lock()
	root := c.root
	root.writable = true
	root.config = c
	return &root
}

func (root *ConfigRoot) Close() {
	if root.writable {
		log.Panic("Close called on writable config object; use Save or Discard")
	}
	if root.config == nil {
		log.Panic("Close called on closed config object")
	}
	root.config.mutex.RUnlock()
	root.config = nil
}

// Save saves the state to the Config object, and attempts to write to
// disk. An error is returned if the write fails, but changes to the
// object are not discarded on error. XXX This is bad API
func (root *ConfigRoot) Save() error {
	if !root.writable {
		log.Panic("Save called on read-only config object")
	}
	if root.config == nil {
		log.Panic("Save called on closed config object")
	}
	c := root.config
	root.writable = false
	root.config = nil
	c.root = *root
	err := c.save()
	c.mutex.Unlock()
	return err
}

// Discard cannot be relied on to restore the state exactly as it was,
// because of potentially shared slice or map objects, but does not do
// an immediate save to disk. XXX This is bad API
func (root *ConfigRoot) Discard() {
	if !root.writable {
		log.Panic("Discard called on read-only config object")
	}
	if root.config == nil {
		log.Panic("Discard called on closed config object")
	}
	c := root.config
	root.config = nil
	c.mutex.Unlock()
}

func (c *Config) save() error {
	data, err := json.MarshalIndent(c.root, "", "  ")
	if err != nil {
		log.Printf("Config encoding error: %v", err)
		return err
	}

	// Make a pathetic attempt at atomic file write by writing into a
	// temporary file and renaming over the original; this is probably
	// imperfect as-implemented, but better than truncating and writing
	// directly.
	tempPath := c.filePath + ".new"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("Config save error: %v", err)
		return err
	}

	if _, err := file.Write(data); err != nil {
		log.Printf("Config write error: %v", err)
		file.Close()
		return err
	}

	file.Close()
	if err := os.Rename(tempPath, c.filePath); err != nil {
		log.Printf("Config replace error: %v", err)
		return err
	}

	return nil
}
