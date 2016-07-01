package core

type Identity struct {
	internalId int

	name    string
	address string

	contactList *ContactList
}

func CreateIdentity(id int, name string) *Identity {
	me := &Identity{
		internalId:  id,
		name:        name,
		contactList: &ContactList{},
	}
	return me
}

func (me *Identity) InternalId() int {
	return me.internalId
}

func (me *Identity) Name() string {
	return me.name
}

func (me *Identity) Address() string {
	return me.address
}

func (me *Identity) ContactList() *ContactList {
	return me.contactList
}
