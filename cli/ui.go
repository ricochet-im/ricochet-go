package main

import (
	"errors"
	"fmt"
	"github.com/chzyer/readline"
	"github.com/ricochet-im/ricochet-go/rpc"
	"golang.org/x/net/context"
	"io"
	"strconv"
	"strings"
	"time"
)

var Ui UI

type UI struct {
	Input  *readline.Instance
	Stdout io.Writer
	Client *Client

	CurrentContact *Contact

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

	words := strings.SplitN(line, " ", 1)

	if ui.CurrentContact != nil {
		if len(words[0]) > 0 && words[0][0] == '/' {
			words[0] = words[0][1:]
		} else {
			ui.CurrentContact.Conversation.SendMessage(line)
			return nil
		}
	}

	if id, err := strconv.Atoi(words[0]); err == nil {
		contact := ui.Client.Contacts.ById(int32(id))
		if contact != nil {
			ui.SetCurrentContact(contact)
		} else {
			fmt.Fprintf(ui.Stdout, "no contact %d\n", id)
		}
		return nil
	}

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

	case "log":
		fmt.Fprint(ui.Stdout, LogBuffer.String())

	case "close":
		ui.SetCurrentContact(nil)

	case "help":
		fallthrough

	default:
		fmt.Fprintf(ui.Stdout, "Commands: clear, quit, status, connect, disconnect, contacts, log, close, help\n")
	}

	return nil
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
				fmt.Fprintf(ui.Stdout, "    \x1b[1m%s\x1b[0m (\x1b[1m%d\x1b[0m) -- \x1b[34;1m%d new messages\x1b[0m\n", contact.Data.Nickname, contact.Data.Id, unreadCount)
			} else {
				fmt.Fprintf(ui.Stdout, "    %s (\x1b[1m%d\x1b[0m)\n", contact.Data.Nickname, contact.Data.Id)
			}
		}
	}
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

	usingConfig     bool
	stopPromptTimer chan struct{}
}

func (cc *conversationInputConfig) OnChange(line []rune, pos int, key rune) ([]rune, int, bool) {
	if len(line) == 0 && key != 0 && !cc.usingConfig {
		cc.Install()
	}

	if len(line) > 0 && line[0] == '/' {
		if cc.usingConfig {
			cc.stopPromptTimer <- struct{}{}
			cc.usingConfig = false
			close(cc.stopPromptTimer)
			cc.BaseConfig.Listener = cc.Config.Listener
			cc.Input.SetConfig(cc.BaseConfig)
		}
	} else if !cc.usingConfig {
		line = append([]rune{'/'}, line...)
	}

	return line, pos, true
}

func (cc *conversationInputConfig) Install() {
	if !cc.usingConfig {
		cc.usingConfig = true
		cc.Input.SetConfig(cc.Config)
		cc.stopPromptTimer = make(chan struct{})
		go cc.updatePromptTimer()
	}
}

func (cc *conversationInputConfig) Remove() {
	cc.BaseConfig.Listener = nil

	if cc.usingConfig {
		cc.stopPromptTimer <- struct{}{}
		cc.usingConfig = false
		close(cc.stopPromptTimer)
		cc.Input.SetConfig(cc.BaseConfig)
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
		Input:      ui.Input,
		Config:     ui.baseChatConfig.Clone(),
		BaseConfig: ui.baseConfig,
		PromptFmt:  fmt.Sprintf(ui.baseChatConfig.Prompt, "%s", ui.CurrentContact.Data.Nickname),
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
