package core

import (
	"errors"
	"time"
)

type InboundContactRequest struct {
	Name     string
	Address  string
	Received time.Time
}

func (request *InboundContactRequest) Accept() (*Contact, error) {
	return nil, errors.New("Not implemented")
}

func (request *InboundContactRequest) Reject(message string) error {
	return errors.New("Not implemented")
}
