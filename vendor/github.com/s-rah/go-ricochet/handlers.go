package goricochet

// ServiceHandler is the interface to handle events for an inbound connection listener
type ServiceHandler interface {
	// OnNewConnection is called for inbound connections to the service after protocol
	// version negotiation has completed successfully.
	OnNewConnection(oc *OpenConnection)
	// OnFailedConnection is called for inbound connections to the service which fail
	// to successfully complete version negotiation for any reason.
	OnFailedConnection(err error)
}

// ConnectionHandler is the interface to handle events for an open protocol connection,
// whether inbound or outbound. Each OpenConnection will need its own instance of an
// application type implementing ConnectionHandler, which could also be used to store
// application state related to the connection.
type ConnectionHandler interface {
	// OnReady is called before OpenConnection.Process() begins from the connection
	OnReady(oc *OpenConnection)
	// OnDisconnect is called when the connection is closed, just before
	// OpenConnection.Process() returns
	OnDisconnect()

	// Authentication Management
	OnAuthenticationRequest(channelID int32, clientCookie [16]byte)
	OnAuthenticationChallenge(channelID int32, serverCookie [16]byte)
	OnAuthenticationProof(channelID int32, publicKey []byte, signature []byte)
	OnAuthenticationResult(channelID int32, result bool, isKnownContact bool)

	// Contact Management
	IsKnownContact(hostname string) bool
	OnContactRequest(channelID int32, nick string, message string)
	OnContactRequestAck(channelID int32, status string)

	// Managing Channels
	OnOpenChannelRequest(channelID int32, channelType string)
	OnOpenChannelRequestSuccess(channelID int32)
	OnChannelClosed(channelID int32)

	// Chat Messages
	OnChatMessage(channelID int32, messageID int32, message string)
	OnChatMessageAck(channelID int32, messageID int32)

	// Handle Errors
	OnFailedChannelOpen(channelID int32, errorType string)
	OnGenericError(channelID int32)
	OnUnknownTypeError(channelID int32)
	OnUnauthorizedError(channelID int32)
	OnBadUsageError(channelID int32)
	OnFailedError(channelID int32)
}
