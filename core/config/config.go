package config

import (
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/ricochet-im/ricochet-go/rpc"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

type ConfigFile struct {
	filePath     string
	root         *ricochet.Config
	readSnapshot atomic.Value
	mutex        sync.Mutex
}

func NewConfigFile(path string) (*ConfigFile, error) {
	cfg := &ConfigFile{
		filePath: path,
		root:     &ricochet.Config{},
	}
	cfg.readSnapshot.Store(cfg.root)
	if err := cfg.save(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadConfigFile(path string) (*ConfigFile, error) {
	cfg := &ConfigFile{
		filePath: path,
		root:     &ricochet.Config{},
	}

	file, err := os.Open(cfg.filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	json := jsonpb.Unmarshaler{
		AllowUnknownFields: true,
	}
	if err := json.Unmarshal(file, cfg.root); err != nil {
		return nil, err
	}

	cfg.readSnapshot.Store(cfg.root)
	return cfg, nil
}

// Read returns a **read-only** snapshot of the current configuration. This
// function is threadsafe, and the values in this instance of the configuration
// will not change when the configuration changes.
//
// Do not under any circumstances modify any part of this object.
func (cfg *ConfigFile) Read() *ricochet.Config {
	return cfg.readSnapshot.Load().(*ricochet.Config)
}

// Lock gains exclusive control of the configuration state, allowing the caller
// to safely make changes to the configuration. Concurrent readers will not see
// any changes until Unlock() is called, at which point they will atomically be
// visible in future calls to Read() as well as being saved persistently.
func (cfg *ConfigFile) Lock() *ricochet.Config {
	cfg.mutex.Lock()
	// Clone the current configuration for a mutable copy
	cfg.root = proto.Clone(cfg.root).(*ricochet.Config)
	return cfg.root
}

func (cfg *ConfigFile) Unlock() {
	// Clone cfg.root again to guarantee that any messages are detached from
	// instances that exist elsewhere in the code. Inefficient but safe.
	cfg.root = proto.Clone(cfg.root).(*ricochet.Config)
	cfg.readSnapshot.Store(cfg.root)
	err := cfg.save()
	if err != nil {
		log.Printf("WARNING: Unable to save configuration: %s", err)
	}
	cfg.mutex.Unlock()
}

func (cfg *ConfigFile) save() error {
	json := jsonpb.Marshaler{Indent: "  "}

	// Make a pathetic attempt at atomic file write by writing into a
	// temporary file and renaming over the original; this is probably
	// imperfect as-implemented, but better than truncating and writing
	// directly.
	tempPath := cfg.filePath + ".new"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("Config save error: %v", err)
		return err
	}

	err = json.Marshal(file, cfg.root)
	if err != nil {
		log.Printf("Config encoding error: %v", err)
		file.Close()
		return err
	}

	file.Close()
	if err := os.Rename(tempPath, cfg.filePath); err != nil {
		log.Printf("Config replace error: %v", err)
		return err
	}

	return nil
}
