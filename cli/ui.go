package main

import (
	"errors"
	"fmt"
	"github.com/ricochet-im/ricochet-go/rpc"
	"golang.org/x/net/context"
	"gopkg.in/readline.v1"
	"strconv"
	"strings"
)

var Ui UI

type UI struct {
	Input  *readline.Instance
	Client *Client

	CurrentContact *Contact
}

func (ui *UI) CommandLoop() {
	ui.Input.SetPrompt("> ")
	for {
		line, err := ui.Input.Readline()
		if err != nil {
			return
		}

		if err := ui.Execute(line); err != nil {
			return
		}
	}
}

func (ui *UI) Execute(line string) error {
	// Block client event handlers for threadsafety
	ui.Client.Block()
	defer ui.Client.Unblock()

	words := strings.SplitN(line, " ", 1)

	if ui.CurrentContact != nil {
		if len(words[0]) > 0 && words[0][0] == '/' {
			words[0] = words[0][1:]
		} else {
			ui.SendMessage(line)
			return nil
		}
	}

	if id, err := strconv.Atoi(words[0]); err == nil {
		contact := ui.Client.Contacts.ById(int32(id))
		if contact != nil {
			ui.SetCurrentContact(contact)
		} else {
			fmt.Printf("no contact %d\n", id)
		}
		return nil
	}

	switch words[0] {
	case "clear":
		readline.ClearScreen(readline.Stdout)

	case "quit":
		return errors.New("Quitting")

	case "status":
		fmt.Printf("server: %v\n", ui.Client.ServerStatus)
		fmt.Printf("identity: %v\n", ui.Client.Identity)

	case "connect":
		status, err := ui.Client.Backend.StartNetwork(context.Background(), &ricochet.StartNetworkRequest{})
		if err != nil {
			fmt.Printf("start network error: %v\n", err)
		} else {
			fmt.Printf("network started: %v\n", status)
		}

	case "disconnect":
		status, err := ui.Client.Backend.StopNetwork(context.Background(), &ricochet.StopNetworkRequest{})
		if err != nil {
			fmt.Printf("stop network error: %v\n", err)
		} else {
			fmt.Printf("network stopped: %v\n", status)
		}

	case "contacts":
		ui.ListContacts()

	case "log":
		fmt.Print(LogBuffer.String())

	case "help":
		fallthrough

	default:
		fmt.Println("Commands: clear, quit, status, connect, disconnect, contacts, log, help")
	}

	return nil
}

func (ui *UI) PrintStatus() {
	controlStatus := ui.Client.NetworkControlStatus()
	connectionStatus := ui.Client.NetworkConnectionStatus()

	switch controlStatus.Status {
	case ricochet.TorControlStatus_STOPPED:
		fmt.Fprintf(ui.Input.Stdout(), "Network is stopped -- type 'connect' to go online\n")

	case ricochet.TorControlStatus_ERROR:
		fmt.Fprintf(ui.Input.Stdout(), "Network error: %s\n", controlStatus.ErrorMessage)

	case ricochet.TorControlStatus_CONNECTING:
		fmt.Fprintf(ui.Input.Stdout(), "Network connecting...\n")

	case ricochet.TorControlStatus_CONNECTED:
		switch connectionStatus.Status {
		case ricochet.TorConnectionStatus_UNKNOWN:
			fallthrough
		case ricochet.TorConnectionStatus_OFFLINE:
			fmt.Fprintf(ui.Input.Stdout(), "Network is offline\n")

		case ricochet.TorConnectionStatus_BOOTSTRAPPING:
			fmt.Fprintf(ui.Input.Stdout(), "Network bootstrapping: %s\n", connectionStatus.BootstrapProgress)

		case ricochet.TorConnectionStatus_READY:
			fmt.Fprintf(ui.Input.Stdout(), "Network is online\n")
		}
	}

	fmt.Fprintf(ui.Input.Stdout(), "Your ricochet ID is %s\n", ui.Client.Identity.Address)

	// no. contacts, contact reqs, online contacts
	// unread messages
}

func (ui *UI) PrintMessage(contact *Contact, outbound bool, text string) {
	if contact == ui.CurrentContact {
		if outbound {
			fmt.Fprintf(ui.Input.Stdout(), "\r%s > %s\n", contact.Data.Nickname, text)
		} else {
			fmt.Fprintf(ui.Input.Stdout(), "\r%s < %s\n", contact.Data.Nickname, text)
		}
	} else if !outbound {
		fmt.Fprintf(ui.Input.Stdout(), "\r---- %s < %s\n", contact.Data.Nickname, text)
	}
}

func (ui *UI) SendMessage(text string) {
	_, err := ui.Client.Backend.SendMessage(context.Background(), &ricochet.Message{
		Sender: &ricochet.Entity{IsSelf: true},
		Recipient: &ricochet.Entity{
			ContactId: ui.CurrentContact.Data.Id,
			Address:   ui.CurrentContact.Data.Address,
		},
		Text: text,
	})
	if err != nil {
		fmt.Printf("send message error: %v\n", err)
	}
}

func (ui *UI) ListContacts() {
	byStatus := make(map[ricochet.Contact_Status][]*Contact)
	for _, contact := range ui.Client.Contacts.Contacts {
		byStatus[contact.Data.Status] = append(byStatus[contact.Data.Status], contact)
	}

	order := []ricochet.Contact_Status{ricochet.Contact_ONLINE, ricochet.Contact_UNKNOWN, ricochet.Contact_OFFLINE, ricochet.Contact_REQUEST, ricochet.Contact_REJECTED}
	for _, status := range order {
		contacts := byStatus[status]
		if len(contacts) == 0 {
			continue
		}
		fmt.Printf(". %s\n", strings.ToLower(status.String()))
		for _, contact := range contacts {
			fmt.Printf("... [%d] %s\n", contact.Data.Id, contact.Data.Nickname)
		}
	}
}

func (ui *UI) SetCurrentContact(contact *Contact) {
	if ui.CurrentContact == contact {
		return
	}

	ui.CurrentContact = contact
	if ui.CurrentContact != nil {
		config := *ui.Input.Config
		config.Prompt = fmt.Sprintf("%s > ", contact.Data.Nickname)
		config.UniqueEditLine = true
		ui.Input.SetConfig(&config)
		fmt.Printf("--- %s (%s) ---\n", contact.Data.Nickname, strings.ToLower(contact.Data.Status.String()))
	} else {
		config := *ui.Input.Config
		config.Prompt = "> "
		config.UniqueEditLine = false
		ui.Input.SetConfig(&config)
	}
}
