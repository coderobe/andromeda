package main

import (
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strings"

	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"github.com/sethvargo/go-diceware/diceware"
	"robpike.io/filter"
)

const (
	GuiEventShowMain = iota
	GuiEventShowMessage
	GuiEventShowHost
	GuiEventShowHostReady
	GuiEventShowHostUnknownConnection
	GuiEventShowJoin
	GuiEventShowJoinUnknownConnection
	GuiEventShowJoinOurHostKey
)

type GuiReqShowMain struct {
}
type GuiReqShowMessage struct {
	Title   string
	Content string
}
type GuiReqShowHost struct {
}
type GuiReqShowHostReady struct {
}
type GuiReqShowHostUnknownConnection struct {
	PubKey   []byte
	Username string
	Password string
	Send     interface{}
}
type GuiReqShowJoin struct {
}
type GuiReqShowJoinUnknownConnection struct {
}
type GuiReqShowJoinOurHostKey struct {
}

func GuiHandle(state Andromeda) func() {
	channel := state.GuiBus

	gui := app.NewWithID("net.in.rob.andromeda")
	win := gui.NewWindow("rob.in.net andromeda")
	win.SetMaster()

	go func() {
		for {
			request := <-channel
			fmt.Printf("Handling GUI event request %d\n", request.ID)
			switch id := request.ID; id {
			case GuiEventShowMain:
				win.SetContent(widget.NewVBox(
					widget.NewLabelWithStyle("Andromeda - A specific nebula", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
					layout.NewSpacer(),
					widget.NewHBox(
						layout.NewSpacer(),
						widget.NewHyperlink("code", parseURL("https://github.com/coderobe/andromeda")),
						layout.NewSpacer(),
					),
					layout.NewSpacer(),
					widget.NewLabelWithStyle("Create a network by selecting `Host` ...", fyne.TextAlignCenter, fyne.TextStyle{}),
					widget.NewLabelWithStyle("...or join one with `Join`", fyne.TextAlignCenter, fyne.TextStyle{}),
					layout.NewSpacer(),
					fyne.NewContainerWithLayout(layout.NewGridLayout(2),
						widget.NewButtonWithIcon("Host", theme.HomeIcon(), func() {
							channel <- Event{
								GuiEventShowHost,
								GuiReqShowHost{},
							}
						}),
						widget.NewButtonWithIcon("Join", theme.NavigateNextIcon(), func() {
							channel <- Event{
								GuiEventShowJoin,
								GuiReqShowJoin{},
							}
						}),
					),
				))
				win.CenterOnScreen()
			case GuiEventShowMessage:
				win.SetContent(widget.NewGroup(
					request.Event.(GuiReqShowMessage).Title,
					widget.NewLabelWithStyle(
						request.Event.(GuiReqShowMessage).Content,
						fyne.TextAlignCenter,
						fyne.TextStyle{},
					),
				))
			case GuiEventShowHost:
				server := widget.NewEntry()
				server.SetPlaceHolder("localhost:1234")
				server.SetText("localhost:1234")

				form := &widget.Form{
					OnSubmit: func() {
						channel <- Event{
							GuiEventShowMessage,
							GuiReqShowMessage{
								"Host",
								"Starting server...",
							},
						}
						state.NetBus <- Event{
							NetEventHost,
							NetReqHost{
								server.Text,
							},
						}
					},
					OnCancel: func() {
						channel <- Event{
							GuiEventShowMain,
							GuiReqShowMain{},
						}
					},
				}
				form.Append("Listen address:port", server)

				win.SetContent(widget.NewGroup("Create network", form))
			case GuiEventShowHostReady:
				fmt.Println(state.OurPubKey)
				fmt.Println(state.HostConfig.Users)
				win.SetContent(widget.NewVBox(
					widget.NewGroup("Accepting connections",
						widget.NewVBox(
							widget.NewLabelWithStyle("Your host is presenting this key:", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
							layout.NewSpacer(),
							widget.NewLabelWithStyle(addNewlineEvery(4, bytesToDiceware(*state.OurPubKey)), fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
							layout.NewSpacer(),
							widget.NewLabelWithStyle("Share this with your users.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
						),
					),
					layout.NewSpacer(),
					widget.NewGroup("Configuration",
						widget.NewHBox(
							layout.NewSpacer(),
							widget.NewCheck("Enable registration requests", func(b bool) {
								state.HostConfig.RegistrationEnabled = b
							}),
							layout.NewSpacer(),
						),
						fyne.NewContainerWithLayout(layout.NewGridLayout(2),
							widget.NewGroup("User Config",
								widget.NewHBox(
									layout.NewSpacer(),
									widget.NewSelect(
										filter.Apply(state.HostConfig.Users, func(u User) string {
											return u.Name
										}).([]string),
										func(username string) {
											fmt.Println("Selected", username) // todo handle
										},
									),
									layout.NewSpacer(),
								),
							),
							widget.NewGroup("Host Config",
								widget.NewButton("Edit", func() {
									fmt.Println("Host edit") // todo handle
								}),
							),
						),
					),
				))
			case GuiEventShowHostUnknownConnection:
				win.SetContent(widget.NewGroup("Unknown user connection",
					widget.NewVBox(
						widget.NewLabelWithStyle("A previously unknown user has connected", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
						layout.NewSpacer(),
						widget.NewLabelWithStyle("Username: '"+request.Event.(GuiReqShowHostUnknownConnection).Username+"'", fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
						layout.NewSpacer(),
						widget.NewLabelWithStyle("The user is presenting this key:", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
						layout.NewSpacer(),
						widget.NewLabelWithStyle(addNewlineEvery(4, bytesToDiceware(request.Event.(GuiReqShowHostUnknownConnection).PubKey)), fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
						layout.NewSpacer(),

						widget.NewGroup("Allow registration?",
							fyne.NewContainerWithLayout(layout.NewGridLayout(2),
								widget.NewButton("Deny", func() {
									fmt.Println("Disallowed registration for", request.Event.(GuiReqShowHostUnknownConnection).Username)
									state.NetBus <- Event{
										NetEventRegistration,
										NetReqRegistration{
											request.Event.(GuiReqShowHostUnknownConnection).Username,
											request.Event.(GuiReqShowHostUnknownConnection).Password,
											false,
											request.Event.(GuiReqShowHostUnknownConnection).Send,
										},
									}
								}),
								widget.NewButton("Allow", func() {
									fmt.Println("Allowed registration for", request.Event.(GuiReqShowHostUnknownConnection).Username)
									state.NetBus <- Event{
										NetEventRegistration,
										NetReqRegistration{
											request.Event.(GuiReqShowHostUnknownConnection).Username,
											request.Event.(GuiReqShowHostUnknownConnection).Password,
											true,
											request.Event.(GuiReqShowHostUnknownConnection).Send,
										},
									}
								}),
							),
						),
					),
				))
			case GuiEventShowJoin:
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
						channel <- Event{
							GuiEventShowMessage,
							GuiReqShowMessage{
								"Join",
								"Connecting to network...",
							},
						}
						state.NetBus <- Event{
							NetEventJoin,
							NetReqJoin{
								server.Text,
								username.Text,
								password.Text,
							},
						}
					},
					OnCancel: func() {
						channel <- Event{
							GuiEventShowMain,
							GuiReqShowMain{},
						}
					},
				}
				form.Append("Server", server)
				form.Append("Username", username)
				form.Append("Password", password)

				win.SetContent(widget.NewGroup("Login", form))
			case GuiEventShowJoinUnknownConnection:
				win.SetContent(widget.NewGroup("Confirm network keys",
					widget.NewVBox(
						widget.NewLabelWithStyle("The host is presenting this key:", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
						layout.NewSpacer(),
						widget.NewLabelWithStyle(addNewlineEvery(4, bytesToDiceware(state.ClientConfig.TheirPubKey)), fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
						layout.NewSpacer(),
						widget.NewLabelWithStyle("If this is not the same key the host sees,\nyour connection might be intercepted.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
						layout.NewSpacer(),

						widget.NewGroup("Continue connecting?",
							fyne.NewContainerWithLayout(layout.NewGridLayout(2),
								widget.NewButton("Abort", func() {
									fmt.Println("Cancelling connection")
									state.NetBus <- Event{
										NetEventJoinUnknownConnection,
										NetReqJoinUnknownConnection{false},
									}
								}),
								widget.NewButton("Continue", func() {
									fmt.Println("Continuing connection")
									state.NetBus <- Event{
										NetEventJoinUnknownConnection,
										NetReqJoinUnknownConnection{true},
									}
								}),
							),
						),
					),
				))
			case GuiEventShowJoinOurHostKey:
				win.SetContent(widget.NewGroup("Confirm network keys",
					widget.NewVBox(
						widget.NewLabelWithStyle("Your client is identifying as:", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
						layout.NewSpacer(),
						widget.NewLabelWithStyle(addNewlineEvery(4, bytesToDiceware(*state.OurPubKey)), fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
						layout.NewSpacer(),
						widget.NewLabelWithStyle("Please share this with your host\nto verify your connection.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
					),
				))
			default:
				fmt.Printf("Fatal: Unknown GUI event %d, this should not have happened\n", id)
				os.Exit(1)
			}
		}
	}()
	return func() {
		win.ShowAndRun()
		fmt.Println("GuiHandle stopped")
	}
}

func parseURL(urlStr string) *url.URL {
	link, err := url.Parse(urlStr)
	if err != nil {
		fyne.LogError("Could not parse URL", err)
	}

	return link
}

func addNewlineEvery(n int, input string) (output string) {
	words := strings.Split(input, " ")
	output = ""
	for i, k := range words {
		output += k
		if (i+1)%n == 0 {
			output += "\n"
		} else {
			output += " "
		}
	}
	return
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
