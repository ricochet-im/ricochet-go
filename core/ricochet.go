package core

type Ricochet interface {
	Config() *Config
	Network() *Network
	Identity() *Identity
}
