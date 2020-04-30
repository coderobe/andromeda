package main

import (
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"strings"

	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"github.com/coderobe/securenet"
	"github.com/sethvargo/go-diceware/diceware"
	"github.com/vmihailenco/msgpack/v4"
	"golang.org/x/crypto/bcrypt"
)

type TappableLabel struct {
	widget.Label
	clipboard fyne.Clipboard
}

func (t *TappableLabel) Tapped(*fyne.PointEvent) {
	t.clipboard.SetContent(t.Text)
	println(t.Text)
}

type Message struct {
	ID     int
	Packet interface{}
}

type User struct {
	Name           string
	Password       string
	HashedPassword []byte
	Connected      bool
	SendChannel    chan Message
}

var Users []User

const (
	packetPing       = iota
	packetPong       = iota
	packetAuth       = iota
	packetAuthStatus = iota
)

type MessagePing struct {
	Token string
}
type MessagePong MessagePing
type MessageAuth struct {
	Username string
	Password string
}
type MessageAuthStatus struct {
	Success bool
}

func boundSendMessage(encoder *msgpack.Encoder, conn net.Conn) func(packetID int, message interface{}) (err error) {
	return func(packetID int, message interface{}) (err error) {
		conn.Write([]byte{byte(packetID)})
		err = encoder.Encode(&message)
		return
	}
}

var UserList []string
var UserListSelect fyne.CanvasObject
var UserListSelectElem *widget.Select
var userEditButton *widget.Button

func updateUIVars() {
	for _, User := range Users {
		UserList = append(UserList, User.Name)
	}
	UserListSelectElem = widget.NewSelect(UserList, func(username string) {
		setContainer(userConfigContainer, UserListSelect)
	})
	UserListSelect = widget.NewHBox(
		layout.NewSpacer(),
		UserListSelectElem,
		layout.NewSpacer(),
	)
	setContainer(userConfigContainer, UserListSelect)
	userEditButton.Enable()
	println(UserList)
}

func networkHost(server string) (err error) {
	defer setContainer(hostContainer, hostScreen())

	if UserListSelectElem == nil {
		userEditButton.Disable()
	}

	listener, err := net.Listen("tcp", server)
	if err != nil {
		fmt.Println("Can't listen")
		return
	}

	pub, priv, elligator, err := securenet.GenerateKeys()
	if err != nil {
		fmt.Println("Failed to generate host keys")
		return
	}

	serverKeyWords := strings.Split(bytesToDiceware(pub[:]), " ")
	serverKey := ""
	for i, k := range serverKeyWords {
		serverKey += k
		if (i+1)%4 == 0 {
			serverKey += "\n"
		} else {
			serverKey += " "
		}
	}

	registrationEnabled := false
	setContainer(hostContainer, widget.NewVBox(
		widget.NewGroup("Accepting connections",
			widget.NewVBox(
				widget.NewLabelWithStyle("Your host is presenting this key:", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
				layout.NewSpacer(),
				&TappableLabel{
					Label:     *widget.NewLabelWithStyle(serverKey, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
					clipboard: win.Clipboard(),
				},
				layout.NewSpacer(),
				widget.NewLabelWithStyle("Share this with your users.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
			),
		),
		layout.NewSpacer(),
		widget.NewGroup("Configuration",
			widget.NewHBox(
				layout.NewSpacer(),
				widget.NewCheck("Enable registration requests", func(b bool) {
					registrationEnabled = b
				}),
				layout.NewSpacer(),
			),
			fyne.NewContainerWithLayout(layout.NewGridLayout(2),
				widget.NewGroup("User",
					userConfigContainer,
					fyne.NewContainerWithLayout(layout.NewGridLayout(1), userEditButton),
				),
				widget.NewGroup("Host",
					widget.NewLabelWithStyle("Local configuration", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
					widget.NewButton("Edit", func() {
						println("clicked host edit")
					}),
				),
			),
		),
	))

	for {
		pConn, err := listener.Accept()
		if err != nil {
			continue
		}
		fmt.Println("Got new connection")
		go func() {
			conn, err := securenet.WrapWithKeys(pConn, pub, priv, elligator)
			if err != nil {
				return
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
					if errors.As(err, &netErr) {
						if netErr.Timeout() {
							fmt.Println("Read timed out")
							continue
						}
					}
					return
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
					fmt.Printf("Got pong, token is '%s', matches? %b\n", pong.Token, sentPing.Token == pong.Token)
				case packetAuth:
					var auth MessageAuth
					decoder.Decode(&auth)
					var authStatus MessageAuthStatus
					authStatus.Success = false

					userExists := false
					for _, user := range Users {
						if user.Name == auth.Username {
							// User exists
							userExists = true
							if bcrypt.CompareHashAndPassword(user.HashedPassword, []byte(auth.Password)) == nil {
								// Password correct
								authStatus.Success = true
								user.SendChannel = make(chan Message)
								go func() {
									for {
										message := <-user.SendChannel
										sendMessage(message.ID, message.Packet)
									}
								}()
							}
							break
						}
					}
					if !userExists && registrationEnabled {
						registerPermitChan := make(chan bool)

						clientKeyWords := strings.Split(bytesToDiceware(conn.GetServerPublicKey()[:]), " ")
						clientKey := ""
						for i, k := range clientKeyWords {
							clientKey += k
							if (i+1)%4 == 0 {
								clientKey += "\n"
							} else {
								clientKey += " "
							}
						}
						oldContainer := getContainer(hostContainer)

						setContainer(hostContainer, widget.NewGroup("Unknown user connection",
							widget.NewVBox(
								widget.NewLabelWithStyle("A previously unknown user has connected", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
								layout.NewSpacer(),
								widget.NewLabelWithStyle("Username: '"+auth.Username+"'", fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
								layout.NewSpacer(),
								widget.NewLabelWithStyle("The user is presenting this key:", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
								layout.NewSpacer(),
								&TappableLabel{
									Label:     *widget.NewLabelWithStyle(clientKey, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
									clipboard: win.Clipboard(),
								},
								layout.NewSpacer(),

								widget.NewGroup("Allow registration?",
									fyne.NewContainerWithLayout(layout.NewGridLayout(2),
										widget.NewButton("Deny", func() {
											fmt.Println("Disallowed registration for", auth.Username)
											registerPermitChan <- false
										}),
										widget.NewButton("Allow", func() {
											fmt.Println("Allowed registration for", auth.Username)
											registerPermitChan <- true
										}),
									),
								),
							),
						))
						if <-registerPermitChan {
							var newUser User
							hashedPw, err := bcrypt.GenerateFromPassword([]byte(auth.Password), 10)
							if err != nil {
								authStatus.Success = false
							} else {
								authStatus.Success = true
								newUser.Name = auth.Username
								newUser.HashedPassword = hashedPw
								newUser.SendChannel = make(chan Message)
								go func() {
									for {
										message := <-newUser.SendChannel
										sendMessage(message.ID, message.Packet)
									}
								}()
								Users = append(Users, newUser)
								updateUIVars()
							}
						}
						setContainer(hostContainer, oldContainer)
					}

					sendMessage(packetAuthStatus, authStatus)
				default:
					fmt.Printf("Unknown packet of type %d incoming\n", messageType)
				}
			}
		}()
	}
}

func bytesToDiceware(in []byte) (output string) {
	wordlist := diceware.WordListEffLarge()
	digits := wordlist.Digits()
	input := big.NewInt(0)
	input.SetBytes(in)

	outword := 0
	outlen := digits
	for _, c := range input.Text(6) {
		if outlen > 0 {
			outword = outword*10 + (int(c) - 47)
			outlen--
		} else {
			outlen = digits
			output += " "
			output += wordlist.WordAt(outword)
			outword = 0
		}
	}
	if outlen > 0 {
		for outlen > 0 {
			outword = outword*10 + 1
			outlen--
		}
		output += " "
		output += wordlist.WordAt(outword)
	}
	return output[1:]
}

func networkJoin(server string, user User) (err error) {
	defer setContainer(joinContainer, joinScreen())
	fmt.Println("Joining", server)
	conn, err := securenet.Dial("tcp", server)
	if err != nil {
		return
	}

	abortChan := make(chan bool)
	serverKeyWords := strings.Split(bytesToDiceware(conn.GetServerPublicKey()[:]), " ")
	serverKey := ""
	for i, k := range serverKeyWords {
		serverKey += k
		if (i+1)%4 == 0 {
			serverKey += "\n"
		} else {
			serverKey += " "
		}
	}

	setContainer(joinContainer, widget.NewGroup("Confirm network keys",
		widget.NewVBox(
			widget.NewLabelWithStyle("The host is presenting this key:", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			layout.NewSpacer(),
			&TappableLabel{
				Label:     *widget.NewLabelWithStyle(serverKey, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
				clipboard: win.Clipboard(),
			},
			layout.NewSpacer(),
			widget.NewLabelWithStyle("If this is not the same key the host sees,\nyour connection might be intercepted.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
			layout.NewSpacer(),

			widget.NewGroup("Continue connecting?",
				fyne.NewContainerWithLayout(layout.NewGridLayout(2),
					widget.NewButton("Abort", func() {
						fmt.Println("Cancelled connection")
						abortChan <- true
					}),
					widget.NewButton("Continue", func() {
						fmt.Println("Continuing connection")
						abortChan <- false
					}),
				),
			),
		),
	))
	if <-abortChan {
		return
	}

	publicKeyWords := strings.Split(bytesToDiceware(conn.GetPublicKey()[:]), " ")
	publicKey := ""
	for i, k := range publicKeyWords {
		publicKey += k
		if (i+1)%4 == 0 {
			publicKey += "\n"
		} else {
			publicKey += " "
		}
	}
	setContainer(joinContainer, widget.NewGroup("Confirm network keys",
		widget.NewVBox(
			widget.NewLabelWithStyle("Your client is identifying as:", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			layout.NewSpacer(),
			&TappableLabel{
				Label:     *widget.NewLabelWithStyle(publicKey, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
				clipboard: win.Clipboard(),
			},
			layout.NewSpacer(),
			widget.NewLabelWithStyle("Please share this with your host\nto verify your connection.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
		),
	))

	var sentPing MessagePing // hold on to last ping we sent for pong
	sendMessage := boundSendMessage(msgpack.NewEncoder(conn), conn)
	decoder := msgpack.NewDecoder(conn)

	var auth MessageAuth
	auth.Username = user.Name
	auth.Password = user.Password
	sendMessage(packetAuth, auth)

	for {
		messageByte, err := conn.ReadByte()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) {
				if netErr.Timeout() {
					fmt.Println("Read timed out")
					continue
				}
			}
			return err
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
			fmt.Printf("Got pong, token is '%s', matches? %b\n", pong.Token, sentPing.Token == pong.Token)
		case packetAuthStatus:
			var authStatus MessageAuthStatus
			decoder.Decode(&authStatus)
			if authStatus.Success {
				println("Auth success")
				setContainer(joinContainer, messageScreen("Join", "Authenticated"))
			} else {
				println("Auth fail")
				setContainer(joinContainer, messageScreen("Join", "Authentication failure"))
			}
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

func hostScreen() fyne.CanvasObject {
	server := widget.NewEntry()
	server.SetPlaceHolder("localhost:1234")
	server.SetText("localhost:1234")

	form := &widget.Form{
		OnSubmit: func() {
			setContainer(hostContainer, messageScreen("Host", "Starting server..."))
			fmt.Println("Hosting as", server.Text)
			go networkHost(server.Text)
		},
	}
	form.Append("Listen address:port", server)

	return widget.NewGroup("Create network", form)
}

func messageScreen(title string, message string) fyne.CanvasObject {
	return widget.NewGroup(title, widget.NewLabelWithStyle(message, fyne.TextAlignCenter, fyne.TextStyle{}))
}

func joinScreen() fyne.CanvasObject {
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
			setContainer(joinContainer, messageScreen("Join", "Connecting to network..."))
			fmt.Println("Name:", username.Text)
			fmt.Println("Pass:", password.Text)
			var user User
			user.Name = username.Text
			user.Password = password.Text
			go networkJoin(server.Text, user)
		},
	}
	form.Append("Server", server)
	form.Append("Username", username)
	form.Append("Password", password)

	return widget.NewGroup("Login", form)
}

var hostContainer *widget.Box
var joinContainer *widget.Box
var userConfigContainer *widget.Box

func setContainer(box *widget.Box, inner fyne.CanvasObject) {
	box.Children = []fyne.CanvasObject{inner}
	box.Refresh()
}
func getContainer(box *widget.Box) fyne.CanvasObject {
	return box.Children[0]
}

var win fyne.Window

func main() {
	hostContainer = widget.NewHBox()
	joinContainer = widget.NewHBox()
	userConfigContainer = widget.NewHBox(widget.NewLabelWithStyle("No known users", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}))
	userEditButton = widget.NewButton("Edit", func() {
		println("clicked user edit", UserListSelectElem.Selected)
	})

	gui := app.NewWithID("net.in.rob.andromeda")
	win = gui.NewWindow("rob.in.net andromeda")
	win.SetMaster()

	setContainer(hostContainer, hostScreen())
	setContainer(joinContainer, joinScreen())

	tabs := widget.NewTabContainer(
		widget.NewTabItemWithIcon("Info", theme.InfoIcon(), infoScreen(gui)),
		widget.NewTabItemWithIcon("Host", theme.HomeIcon(), hostContainer),
		widget.NewTabItemWithIcon("Join", theme.NavigateNextIcon(), joinContainer))
	tabs.SetTabLocation(widget.TabLocationLeading)
	win.SetContent(tabs)

	win.ShowAndRun()
}
