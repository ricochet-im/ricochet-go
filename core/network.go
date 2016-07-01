package core

type NetworkStatus int

const (
	NetworkUnavailable NetworkStatus = iota
	NetworkError
	NetworkOffline
	NetworkOnline
)

type Network struct {
	torConfig *TorConfiguration
	status    NetworkStatus
}
