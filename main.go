package main

import (
	"fmt"

	"github.com/coderobe/securenet"
)

type Event struct {
	ID    int
	Event interface{}
}

type User struct {
	Name           string
	Password       string
	HashedPassword []byte
	Connected      bool
	Bus            chan Event
}

type HostConfig struct {
	RegistrationEnabled bool
	Users               []User
}

type ClientConfig struct {
	Conn        securenet.Conn
	TheirPubKey []byte
	Username    string
	Password    string
}

type Andromeda struct {
	GuiBus       chan Event
	NetBus       chan Event
	OurPubKey    *[]byte
	HostConfig   *HostConfig
	ClientConfig *ClientConfig
}

func main() {
	fmt.Println("Starting Andromeda")
	var state Andromeda
	state.GuiBus = make(chan Event, 1)
	state.NetBus = make(chan Event, 1)
	state.OurPubKey = &[]byte{}
	state.HostConfig = &HostConfig{}
	state.HostConfig.RegistrationEnabled = false
	state.ClientConfig = &ClientConfig{}

	state.GuiBus <- Event{
		GuiEventShowMain,
		GuiReqShowMain{},
	}

	fmt.Println("Starting NetHandle")
	go NetHandle(state)()
	fmt.Println("Starting GuiHandle")
	GuiHandle(state)()
}
