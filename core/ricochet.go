package core

type Ricochet struct {
	Network  *Network
	Identity *Identity
	Config   *Config
}

func Initialize(configPath string) (*Ricochet, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	ricochet := &Ricochet{
		Config: cfg,
	}

	return ricochet, nil
}
