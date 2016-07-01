package core

type ContactStatus int

const (
	ContactOnline ContactStatus = iota
	ContactOffline
	ContactRequestPending
	ContactRequestRejected
	ContactOutdated
)

type Contact struct {
	InternalId int

	Name    string
	Address string
}
