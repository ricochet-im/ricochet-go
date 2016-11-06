package core

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"sync"
)

// XXX This is partially but not fully compatible with Ricochet's JSON
// configs. It might be better to be explicitly not compatible, but have
// an automatic import function.

type Config struct {
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
	LastConnected string `json:",omitempty"`
	Nickname      string
	WhenCreated   string
	Request       ConfigContactRequest `json:",omitempty"`
}

type ConfigContactRequest struct {
	Pending       bool
	MyNickname    string
	Message       string
	WhenDelivered string `json:",omitempty"`
	WhenRejected  string `json:",omitempty"`
	RemoteError   string `json:",omitempty"`
}

type ConfigIdentity struct {
	DataDirectory string `json:",omitempty"`
	ServiceKey    string
}

func LoadConfig(filePath string) (*Config, error) {
	config := &Config{
		filePath: filePath,
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

	return config, nil
}

// Return a read-only snapshot of the current configuration. This object
// _must not_ be stored, and you must call Close() when finished with it.
// This function may block.
func (c *Config) OpenRead() *ConfigRoot {
	c.mutex.RLock()
	root := c.root.Clone()
	root.writable = false
	root.config = c
	return root
}

// Return a writable snapshot of the current configuration. This object
// _must not_ be stored, and you must call Save() or Discard() when
// finished with it. This function may block.
func (c *Config) OpenWrite() *ConfigRoot {
	c.mutex.Lock()
	root := c.root.Clone()
	root.writable = true
	root.config = c
	return root
}

func (root *ConfigRoot) Clone() *ConfigRoot {
	re := *root
	re.Contacts = make(map[string]ConfigContact)
	for k, v := range root.Contacts {
		re.Contacts[k] = v
	}
	return &re
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

// Save writes the state to disk, and updates the Config object if
// successful. Changes to the object are discarded on error.
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
	err := c.save(root)
	c.mutex.Unlock()
	return err
}

// Discard closes a config write without saving any changes to disk
// or to the Config object.
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

func (c *Config) save(newRoot *ConfigRoot) error {
	data, err := json.MarshalIndent(newRoot, "", "  ")
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

	c.root = *newRoot
	return nil
}
