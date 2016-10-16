package core

import (
	cryptorand "crypto/rand"
	"log"
	"math"
	"math/big"
	"math/rand"
)

type Ricochet struct {
	Config   *Config
	Network  *Network
	Identity *Identity
}

func (core *Ricochet) Init(conf *Config) error {
	initRand()

	var err error
	core.Config = conf
	core.Network = CreateNetwork()
	core.Identity, err = CreateIdentity(core)
	if err != nil {
		return err
	}

	return nil
}

func initRand() {
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		log.Panicf("rng failed: %v", err)
	}

	rand.Seed(n.Int64())
}
