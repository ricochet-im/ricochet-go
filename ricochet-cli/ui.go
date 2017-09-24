package main

import (
	"errors"
	"fmt"
	"github.com/chzyer/readline"
	"github.com/ricochet-im/ricochet-go/core"
	"github.com/ricochet-im/ricochet-go/rpc"
	"golang.org/x/net/context"
	"io"
	"strings"
	"time"
)

const (
	MinContactPrefix = 3
)

var Ui UI

type UI struct {
	Input  *readline.Instance
	Stdout io.Writer
	Client *Client

	CurrentContact *Contact
	commandMode    bool

	baseConfig     *readline.Config
	baseChatConfig *readline.Config
}

func (ui *UI) CommandLoop() {
	ui.setupInputConfigs()
	ui.Input.SetConfig(ui.baseConfig)

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

func (ui *UI) setupInputConfigs() {
	ui.baseConfig = ui.Input.Config.Clone()
	ui.baseConfig.Prompt = "> "
	ui.baseChatConfig = ui.baseConfig.Clone()
	ui.baseChatConfig.Prompt = "\x1b[90m%s\x1b[39m | %s \x1b[34m<<\x1b[39m "
	ui.baseChatConfig.UniqueEditLine = true
}

func (ui *UI) Execute(line string) error {
	// Block client event handlers for threadsafety
	ui.Client.Block()
	defer ui.Client.Unblock()

	if ui.CurrentContact != nil && !ui.commandMode {
		ui.CurrentContact.Conversation.SendMessage(line)
		return nil
	}

	words := strings.SplitN(line, " ", 2)
	switch words[0] {
	case "clear":
		readline.ClearScreen(readline.Stdout)

	case "quit":
		return errors.New("Quitting")

	case "status":
		ui.PrintStatus()

	case "connect":
		status, err := ui.Client.Backend.StartNetwork(context.Background(), &ricochet.StartNetworkRequest{})
		if err != nil {
			fmt.Fprintf(ui.Stdout, "start network error: %v\n", err)
		} else {
			fmt.Fprintf(ui.Stdout, "network started: %v\n", status)
		}

	case "disconnect":
		status, err := ui.Client.Backend.StopNetwork(context.Background(), &ricochet.StopNetworkRequest{})
		if err != nil {
			fmt.Fprintf(ui.Stdout, "stop network error: %v\n", err)
		} else {
			fmt.Fprintf(ui.Stdout, "network stopped: %v\n", status)
		}

	case "contacts":
		ui.ListContacts()

	case "add-contact":
		ui.AddContact(words[1:])

	case "delete-contact":
		ui.DeleteContact(words[1:])

	case "log":
		fmt.Fprint(ui.Stdout, LogBuffer.String())

	case "close":
		ui.SetCurrentContact(nil)

	case "help":
		ui.printHelp()

	default:
		contact := ui.ContactByPrefix(line)
		if contact != nil {
			ui.SetCurrentContact(contact)
		} else {
			ui.printHelp()
		}
	}

	return nil
}

func (ui *UI) printHelp() {
	fmt.Fprintf(ui.Stdout, "Commands: clear, quit, status, connect, disconnect, contacts, add-contact, delete-contact, log, close, help\n")
}

func (ui *UI) PrintStatus() {
	controlStatus := ui.Client.NetworkControlStatus()
	connectionStatus := ui.Client.NetworkConnectionStatus()

	switch controlStatus.Status {
	case ricochet.TorControlStatus_STOPPED:
		fmt.Fprintf(ui.Stdout, "Network is stopped -- type 'connect' to go online\n")

	case ricochet.TorControlStatus_ERROR:
		fmt.Fprintf(ui.Stdout, "Network error: %s\n", controlStatus.ErrorMessage)

	case ricochet.TorControlStatus_CONNECTING:
		fmt.Fprintf(ui.Stdout, "Network connecting...\n")

	case ricochet.TorControlStatus_CONNECTED:
		switch connectionStatus.Status {
		case ricochet.TorConnectionStatus_UNKNOWN:
			fallthrough
		case ricochet.TorConnectionStatus_OFFLINE:
			fmt.Fprintf(ui.Stdout, "Network is offline\n")

		case ricochet.TorConnectionStatus_BOOTSTRAPPING:
			fmt.Fprintf(ui.Stdout, "Network bootstrapping: %s\n", connectionStatus.BootstrapProgress)

		case ricochet.TorConnectionStatus_READY:
			fmt.Fprintf(ui.Stdout, "Network is online\n")
		}
	}

	fmt.Fprintf(ui.Stdout, "Your ricochet ID is %s\n", ui.Client.Identity.Address)

	// no. contacts, contact reqs, online contacts
	// unread messages
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
		fmt.Fprintf(ui.Stdout, "%s\n", ColoredContactStatus(status))
		for _, contact := range contacts {
			unreadCount := contact.Conversation.UnreadCount()
			if unreadCount > 0 {
				fmt.Fprintf(ui.Stdout, "    \x1b[1m%s\x1b[0m (\x1b[1m%s\x1b[0m) -- \x1b[34;1m%d new messages\x1b[0m\n", contact.Data.Nickname, ui.PrefixForContact(contact), unreadCount)
			} else {
				fmt.Fprintf(ui.Stdout, "    %s (\x1b[1m%s\x1b[0m)\n", contact.Data.Nickname, ui.PrefixForContact(contact))
			}
		}
	}
}

func (ui *UI) AddContact(params []string) {
	var address string

	if len(params) > 0 {
		address = params[0]
	} else {
		str, err := readline.Line("Contact address: ")
		if err != nil {
			return
		}
		address = str
	}

	// XXX validate address

	nickname, err := readline.Line("Nickname: ")
	if err != nil {
		return
	}
	fromNickname, err := readline.Line("From (your nickname): ")
	if err != nil {
		return
	}
	message, err := readline.Line("Message: ")
	if err != nil {
		return
	}

	contact, err := ui.Client.Backend.AddContactRequest(context.Background(),
		&ricochet.ContactRequest{
			Direction:    ricochet.ContactRequest_OUTBOUND,
			Address:      address,
			Nickname:     nickname,
			Text:         message,
			FromNickname: fromNickname,
		})

	if err != nil {
		fmt.Fprintf(ui.Stdout, "Failed: %s\n", err)
		return
	}

	fmt.Fprintf(ui.Stdout, "Added contact \x1b[1m%s\x1b[0m (\x1b[1m%s\x1b[0m)\n", contact.Nickname, contact.Address)
}

func (ui *UI) DeleteContact(params []string) {
	if len(params) < 1 {
		fmt.Fprintf(ui.Stdout, "Usage: delete-contact [address]\n")
		return
	}
	contact := ui.Client.Contacts.ByAddress(params[0])
	if contact == nil {
		contact = ui.ContactByPrefix(params[0])
	}
	if contact == nil {
		fmt.Fprintf(ui.Stdout, "No contact with address %s\n", params[0])
		return
	}

	fmt.Fprintf(ui.Stdout, "\nThis contact will be \x1b[31mdeleted\x1b[0m:\n\n")
	fmt.Fprintf(ui.Stdout, "    Address:\t%s\n", contact.Data.Address)
	fmt.Fprintf(ui.Stdout, "    Name:\t%s\n", contact.Data.Nickname)
	fmt.Fprintf(ui.Stdout, "    Online:\t%s\n", contact.Data.LastConnected)
	fmt.Fprintf(ui.Stdout, "    Created:\t%s\n\n", contact.Data.WhenCreated)
	confirm, err := readline.Line("Type YES to confirm: ")
	if err != nil || confirm != "YES" {
		fmt.Fprintf(ui.Stdout, "Aborted\n")
		return
	}

	_, err = ui.Client.Backend.DeleteContact(context.Background(),
		&ricochet.DeleteContactRequest{Address: contact.Data.Address})
	if err != nil {
		fmt.Fprintf(ui.Stdout, "Failed: %s\n", err)
		return
	}

	fmt.Fprintf(ui.Stdout, "Contact deleted\n")
}

// This type acts as a readline Listener and handles special behavior for
// the prompt in a conversation. In particular, it swaps temporarily back to
// the normal prompt for command lines (starting with /), and it keeps the
// timestamp in the conversation prompt updated.
type conversationInputConfig struct {
	Input      *readline.Instance
	Config     *readline.Config
	BaseConfig *readline.Config
	PromptFmt  string

	CommandModeFlag *bool
	editingLine     []rune

	usingConfig     bool
	stopPromptTimer chan struct{}
}

func (cc *conversationInputConfig) OnChange(line []rune, pos int, key rune) ([]rune, int, bool) {
	cc.editingLine = line

	// Unset command mode at the beginning of a new read
	if line == nil && pos == 0 && key == 0 {
		cc.Install()
	}

	return line, pos, true
}

func (cc *conversationInputConfig) FilterInputRune(key rune) (rune, bool) {
	if key == '/' && len(cc.editingLine) == 0 && cc.usingConfig {
		// '/' at the beginning of the input triggers command mode
		cc.stopPromptTimer <- struct{}{}
		cc.usingConfig = false
		close(cc.stopPromptTimer)
		cc.BaseConfig.Listener = cc.Config.Listener
		cc.BaseConfig.FuncFilterInputRune = cc.FilterInputRune
		cc.Input.SetConfig(cc.BaseConfig)
		*cc.CommandModeFlag = true

		// The / is suppressed from the output
		return key, false
	} else if (key == readline.CharBackspace || key == readline.CharCtrlH) &&
		len(cc.editingLine) == 0 && !cc.usingConfig {
		// Backspace on an empty command mode line unsets command mode
		cc.Install()
		return key, false
	} else {
		return key, true
	}
}

func (cc *conversationInputConfig) Install() {
	if !cc.usingConfig {
		cc.usingConfig = true
		cc.Config.FuncFilterInputRune = cc.FilterInputRune
		cc.Input.SetConfig(cc.Config)
		cc.stopPromptTimer = make(chan struct{})
		go cc.updatePromptTimer()
		*cc.CommandModeFlag = false
	}
}

func (cc *conversationInputConfig) Remove() {
	cc.BaseConfig.Listener = nil

	if cc.usingConfig {
		cc.stopPromptTimer <- struct{}{}
		cc.usingConfig = false
		close(cc.stopPromptTimer)
		cc.BaseConfig.FuncFilterInputRune = nil
		cc.Input.SetConfig(cc.BaseConfig)
		*cc.CommandModeFlag = true
	}
}

func (cc *conversationInputConfig) updatePromptTimer() {
	for {
		t := time.Now()
		cc.Input.SetPrompt(fmt.Sprintf(cc.PromptFmt, t.Format("15:04")))
		cc.Input.Refresh()

		sec := 61 - t.Second()
		select {
		case <-time.After(time.Duration(sec) * time.Second):
			continue
		case <-cc.stopPromptTimer:
			return
		}
	}
}

func (ui *UI) setupConversationPrompt() {
	if ui.Input.Config.Listener != nil {
		ui.Input.Config.Listener.(*conversationInputConfig).Remove()
	}

	listener := &conversationInputConfig{
		Input:           ui.Input,
		Config:          ui.baseChatConfig.Clone(),
		BaseConfig:      ui.baseConfig,
		PromptFmt:       fmt.Sprintf(ui.baseChatConfig.Prompt, "%s", ui.CurrentContact.Data.Nickname),
		CommandModeFlag: &ui.commandMode,
	}
	listener.Config.Listener = listener
	listener.Install()
}

func ColoredContactStatus(status ricochet.Contact_Status) string {
	switch status {
	case ricochet.Contact_UNKNOWN:
		return "\x1b[31moffline\x1b[39m"
	case ricochet.Contact_OFFLINE:
		return "\x1b[31moffline\x1b[39m"
	case ricochet.Contact_ONLINE:
		return "\x1b[32monline\x1b[39m"
	case ricochet.Contact_REQUEST:
		return "\x1b[33mcontact request\x1b[39m"
	case ricochet.Contact_REJECTED:
		return "\x1b[31mrejected\x1b[39m"
	default:
		return status.String()
	}
}

func (ui *UI) ContactByPrefix(prefix string) *Contact {
	if len(prefix) < MinContactPrefix {
		return nil
	}
	var contact *Contact
	for _, c := range ui.Client.Contacts.Contacts {
		host, _ := core.PlainHostFromAddress(c.Data.Address)
		if prefix == host[:len(prefix)] {
			if contact != nil {
				// Ambiguous prefix
				return nil
			}
			contact = c
		}
	}
	return contact
}

func (ui *UI) PrefixForContact(contact *Contact) string {
	host, _ := core.PlainHostFromAddress(contact.Data.Address)
	prefix := host[:MinContactPrefix]

	for _, c := range ui.Client.Contacts.Contacts {
		if c == contact {
			continue
		}
		cHost, _ := core.PlainHostFromAddress(c.Data.Address)
		for prefix == cHost[:len(prefix)] && len(prefix) < len(host) {
			prefix = host[:len(prefix)+1]
		}
	}

	return prefix
}

func (ui *UI) SetCurrentContact(contact *Contact) {
	if ui.CurrentContact == contact {
		return
	}

	if ui.CurrentContact != nil {
		ui.CurrentContact.Conversation.SetActive(false)
	}

	ui.CurrentContact = contact
	if ui.CurrentContact != nil {
		ui.setupConversationPrompt()
		fmt.Fprintf(ui.Stdout, "------- \x1b[1m%s\x1b[0m is %s -------\n", contact.Data.Nickname, ColoredContactStatus(contact.Data.Status))
		ui.CurrentContact.Conversation.SetActive(true)
	} else {
		ui.Input.Config.Listener.(*conversationInputConfig).Remove()
		ui.Input.SetConfig(ui.baseConfig)
	}
}
