package core

type OutboundContactRequestStatus int

const (
	RequestPending OutboundContactRequestStatus = iota
	RequestAcknowledged
	RequestAccepted
	RequestRejected
	RequestError
)

type OutboundContactRequest struct {
	Contact

	MyName  string
	Message string

	Status OutboundContactRequestStatus
}
