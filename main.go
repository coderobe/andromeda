package main

import (
	"fmt"
	"net"
	"net/url"
	"errors"

	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"github.com/coderobe/securenet"
	"github.com/vmihailenco/msgpack/v4"
)

type User struct {
	Name string
	Password string
}

const (
	packetPing = iota
	packetPong = iota
)

type MessagePing struct {
	Token string
}
type MessagePong MessagePing

func boundSendMessage(encoder *msgpack.Encoder, conn net.Conn) func(packetID int, message interface{}) (err error) {
	return func(packetID int, message interface{}) (err error) {
		conn.Write([]byte{byte(packetID)})
		err = encoder.Encode(&message)
		return
	}
}

func networkHost(server string) (err error) {
	listener, err := net.Listen("tcp", server)
	if err != nil {
		panic("can't listen")
	}

	for {
		pConn, err := listener.Accept()
		conn, err := securenet.Wrap(pConn)
		if err != nil {
			return err
		}
		var sentPing MessagePing // hold on to last ping we sent for pong
		sendMessage := boundSendMessage(msgpack.NewEncoder(conn), conn)
		decoder := msgpack.NewDecoder(conn)

		sentPing.Token = "Foo, bar!"
		sendMessage(packetPing, sentPing)

		for {
			messageByte, err := conn.ReadByte()
			if err != nil {
				var netErr net.Error
				if errors.As(err, netErr) {
					if netErr.Timeout() {
						fmt.Println("Read timed out")
						continue
					}
				}
				panic("BBBBBBBBBBBBBBBBBBBBBb")
			}
			switch messageType := uint8(messageByte); messageType {
			case packetPing:
				var ping MessagePing
				decoder.Decode(&ping)

				fmt.Printf("Got ping, token is '%s'\n", ping.Token)
				sendMessage(packetPong, ping) // return as pong
			case packetPong:
				var pong MessagePong
				decoder.Decode(&pong)
				fmt.Printf("Got pong %s, matches? %b\n", pong.Token, sentPing.Token == pong.Token)
			default:
				fmt.Printf("Unknown packet of type %d incoming\n", messageType)
			}
		}
	}
}

func networkJoin(server string, user User) (err error) {
	conn, err := securenet.Dial("tcp", server)
	if err != nil {
		return
	}
	var sentPing MessagePing // hold on to last ping we sent for pong
	sendMessage := boundSendMessage(msgpack.NewEncoder(conn), conn)
	decoder := msgpack.NewDecoder(conn)

	for {
		messageByte, err := conn.ReadByte()
		if err != nil {
			var netErr net.Error
			if errors.As(err, netErr) {
				if netErr.Timeout() {
					fmt.Println("Read timed out")
					continue
				}
			}
			panic("BBBBBBBBBBBBBBBBBBBBBb")
		}
		switch messageType := uint8(messageByte); messageType {
		case packetPing:
			var ping MessagePing
			decoder.Decode(&ping)

			fmt.Printf("Got ping, token is '%s'\n", ping.Token)
			sendMessage(packetPong, ping) // return as pong
		case packetPong:
			var pong MessagePong
			decoder.Decode(&pong)
			fmt.Printf("Got pong %s, matches? %b\n", pong.Token, sentPing.Token == pong.Token)
		default:
			fmt.Printf("Unknown packet of type %d incoming\n", messageType)
		}
	}
}

func parseURL(urlStr string) *url.URL {
	link, err := url.Parse(urlStr)
	if err != nil {
		fyne.LogError("Could not parse URL", err)
	}

	return link
}

func infoScreen(gui fyne.App) fyne.CanvasObject {
	return widget.NewVBox(
		widget.NewLabelWithStyle("Andromeda - A specific nebula", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),

		widget.NewHBox(layout.NewSpacer(),
			widget.NewHyperlink("code", parseURL("https://github.com/coderobe/andromeda")),
		layout.NewSpacer()),
		layout.NewSpacer(),
		widget.NewLabelWithStyle("Create a network in the `Host` tab...", fyne.TextAlignCenter, fyne.TextStyle{}),
		widget.NewLabelWithStyle("...or join one with `Join`", fyne.TextAlignCenter, fyne.TextStyle{}),
		layout.NewSpacer(),

		widget.NewGroup("Theme",
			fyne.NewContainerWithLayout(layout.NewGridLayout(2),
				widget.NewButton("Dark", func() {
					gui.Settings().SetTheme(theme.DarkTheme())
				}),
				widget.NewButton("Light", func() {
					gui.Settings().SetTheme(theme.LightTheme())
				}),
			),
		),
	)
}

func hostScreen(gui fyne.App) fyne.CanvasObject {
	server := widget.NewEntry()
	server.SetPlaceHolder("localhost:1234")
	server.SetText("localhost:1234")

	form := &widget.Form{
		OnSubmit: func() {
			fmt.Println("Hosting as", server.Text)
			go networkHost(server.Text)
		},
	}
	form.Append("Listen address:port", server)

	return widget.NewGroup("Create network", form)
}

func otherHostScreen() fyne.CanvasObject {
	return widget.NewGroup("Something", widget.NewLabel("hello world"))
}

func joinScreen(gui fyne.App) fyne.CanvasObject {
	server := widget.NewEntry()
	server.SetPlaceHolder("localhost:1234")
	server.SetText("localhost:1234")
	username := widget.NewEntry()
	username.SetPlaceHolder("JohnDoe")
	username.SetText("cdr") //todo remove
	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("*******")
	password.SetText("hunter2") //todo remove

	form := &widget.Form{
		OnSubmit: func() {
			fmt.Println("Joining", server.Text)
			fmt.Println("Name:", username.Text)
			fmt.Println("Pass:", password.Text)
			var user User
			user.Name = username.Text
			user.Password = password.Text
			setContainer(hostContainer, otherHostScreen())
			go networkJoin(server.Text, user)
		},
	}
	form.Append("Server", server)
	form.Append("Username", username)
	form.Append("Password", password)

	return widget.NewGroup("Login", form)
}

var hostContainer *widget.Box

func setContainer(box *widget.Box, inner fyne.CanvasObject) {
	box.Children = []fyne.CanvasObject{inner}
	box.Refresh()
}

func main() {
	hostContainer = widget.NewHBox()

	gui := app.NewWithID("net.in.rob.andromeda")
	win := gui.NewWindow("rob.in.net andromeda")
	win.SetMaster()

	setContainer(hostContainer, hostScreen(gui))

	tabs := widget.NewTabContainer(
		widget.NewTabItemWithIcon("Info", theme.InfoIcon(), infoScreen(gui)),
		widget.NewTabItemWithIcon("Host", theme.HomeIcon(), hostContainer),
		widget.NewTabItemWithIcon("Join", theme.NavigateNextIcon(), joinScreen(gui)))
	tabs.SetTabLocation(widget.TabLocationLeading)
	win.SetContent(tabs)

	win.ShowAndRun()
}
