package core

import (
	cryptorand "crypto/rand"
	"github.com/ricochet-im/ricochet-go/core/config"
	"log"
	"math"
	"math/big"
	"math/rand"
	"net"
	"os"
)

type Ricochet struct {
	Config   *config.ConfigFile
	Network  *Network
	Identity *Identity
}

func (core *Ricochet) Init(conf *config.ConfigFile) (err error) {
	initRand()

	core.Config = conf

	core.Network = CreateNetwork()
	core.setupNetwork()
	core.Identity, err = CreateIdentity(core)
	return
}

func initRand() {
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		log.Panicf("rng failed: %v", err)
	}

	rand.Seed(n.Int64())
}

func (core *Ricochet) setupNetwork() {
	socket := os.Getenv("TOR_CONTROL_SOCKET")
	host := os.Getenv("TOR_CONTROL_HOST")
	port := os.Getenv("TOR_CONTROL_PORT")
	passwd := os.Getenv("TOR_CONTROL_PASSWD")

	if socket != "" {
		core.Network.SetControlAddress("unix:" + socket)
	} else if host != "" {
		if port == "" {
			port = "9051"
		}
		core.Network.SetControlAddress(net.JoinHostPort(host, port))
	} else {
		core.Network.SetControlAddress("127.0.0.1:9051")
	}

	if passwd != "" {
		core.Network.SetControlPassword(passwd)
	}
}
