package core

type Ricochet struct {
	Config   *Config
	Network  *Network
	Identity *Identity
}

func (core *Ricochet) Init(conf *Config) error {
	var err error
	core.Config = conf
	core.Network = CreateNetwork()
	core.Identity, err = CreateIdentity(core)
	if err != nil {
		return err
	}

	return nil
}
