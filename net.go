package main

import (
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/coderobe/securenet"
	"github.com/vmihailenco/msgpack/v4"
	"golang.org/x/crypto/bcrypt"
)

const (
	NetEventHost = iota
	NetEventRegistration
	NetEventJoin
	NetEventJoinUnknownConnection
)

type NetReqHost struct {
	Server string
}
type NetReqRegistration struct {
	Username string
	Password string
	Allow    bool
	Send     interface{}
}
type NetReqJoin struct {
	Server   string
	Username string
	Password string
}
type NetReqJoinUnknownConnection struct {
	Allow bool
}

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

func NetHandle(state Andromeda) func() {
	channel := state.NetBus

	return func() {
		for {
			request := <-channel
			fmt.Printf("Handling Net event request %d\n", request.ID)
			switch id := request.ID; id {
			case NetEventHost:
				go func() {
					fmt.Println("Trying to host on", request.Event.(NetReqHost).Server)

					listener, err := net.Listen("tcp", request.Event.(NetReqHost).Server)
					if err != nil {
						fmt.Println("Can't listen")
						return
					}

					pub, priv, elligator, err := securenet.GenerateKeys() // todo: load from config
					if err != nil {
						fmt.Println("Failed to generate host keys")
						return
					}

					*state.OurPubKey = pub[:]
					fmt.Println(state.OurPubKey)
					state.GuiBus <- Event{
						GuiEventShowHostReady,
						GuiReqShowHostReady{},
					}

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
							fmt.Println("Sending ping")
							sendMessage(packetPing, sentPing)
							fmt.Println("Sent ping")

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
									fmt.Printf("Got user auth attempt for '%s'\n", auth.Username)
									var authStatus MessageAuthStatus
									authStatus.Success = false

									userExists := false
									for _, user := range state.HostConfig.Users {
										if user.Name == auth.Username {
											// User exists
											userExists = true
											if bcrypt.CompareHashAndPassword(user.HashedPassword, []byte(auth.Password)) == nil {
												// Password correct
												authStatus.Success = true
												user.Bus = make(chan Event)
												go func() {
													for {
														message := <-user.Bus
														sendMessage(message.ID, message.Event)
													}
												}()
												state.GuiBus <- Event{
													GuiEventShowHostReady,
													GuiReqShowHostReady{},
												}
											}
											sendMessage(packetAuthStatus, authStatus)
											break
										}
									}
									fmt.Println(state.HostConfig.RegistrationEnabled)
									if !userExists && state.HostConfig.RegistrationEnabled {
										state.GuiBus <- Event{
											GuiEventShowHostUnknownConnection,
											GuiReqShowHostUnknownConnection{
												conn.GetServerPublicKey()[:],
												auth.Username,
												auth.Password,
												sendMessage,
											},
										}
									}
								default:
									fmt.Printf("Unknown packet of type %d incoming\n", messageType)
								}
							}
						}()
					}
				}()
			case NetEventRegistration:
				go func() {
					var authStatus MessageAuthStatus
					var newUser User
					hashedPw, err := bcrypt.GenerateFromPassword([]byte(request.Event.(NetReqRegistration).Password), 10)
					if err != nil {
						authStatus.Success = false
					} else {
						authStatus.Success = true
						newUser.Name = request.Event.(NetReqRegistration).Username
						newUser.HashedPassword = hashedPw
						newUser.Bus = make(chan Event)
						sendMessage := request.Event.(NetReqRegistration).Send.(func(id int, event interface{}) error)
						go func() {
							for {
								message := <-newUser.Bus
								sendMessage(message.ID, message.Event)
							}
						}()
						fmt.Println("Adding User to user list")
						state.HostConfig.Users = append(state.HostConfig.Users, newUser)
					}
					request.Event.(NetReqRegistration).Send.(func(id int, event interface{}) error)(packetAuthStatus, authStatus)
					state.GuiBus <- Event{
						GuiEventShowHostReady,
						GuiReqShowHostReady{},
					}
				}()
			case NetEventJoin:
				go func() {
					fmt.Println("Trying to join", request.Event.(NetReqJoin).Server)

					fmt.Println("as", request.Event.(NetReqJoin).Username)
					state.ClientConfig.Username = request.Event.(NetReqJoin).Username

					fmt.Println("authenticated by", request.Event.(NetReqJoin).Password)
					state.ClientConfig.Password = request.Event.(NetReqJoin).Password

					conn, err := securenet.Dial("tcp", request.Event.(NetReqJoin).Server)
					state.ClientConfig.Conn = conn
					if err != nil {
						fmt.Println("Failed to connect")
						return
					}

					*state.OurPubKey = state.ClientConfig.Conn.GetPublicKey()[:]
					state.ClientConfig.TheirPubKey = state.ClientConfig.Conn.GetServerPublicKey()[:]
					state.GuiBus <- Event{
						GuiEventShowJoinUnknownConnection,
						GuiReqShowJoinUnknownConnection{},
					}
				}()
			case NetEventJoinUnknownConnection:
				go func() {
					if !request.Event.(NetReqJoinUnknownConnection).Allow {
						fmt.Println("Connection abort")
						return
					}

					state.GuiBus <- Event{
						GuiEventShowJoinOurHostKey,
						GuiReqShowJoinOurHostKey{},
					}

					var sentPing MessagePing // hold on to last ping we sent for pong
					sendMessage := boundSendMessage(msgpack.NewEncoder(state.ClientConfig.Conn), state.ClientConfig.Conn)
					decoder := msgpack.NewDecoder(state.ClientConfig.Conn)

					var auth MessageAuth
					auth.Username = state.ClientConfig.Username
					auth.Password = state.ClientConfig.Password
					sendMessage(packetAuth, auth)

					for {
						messageByte, err := state.ClientConfig.Conn.ReadByte()
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
						case packetAuthStatus:
							var authStatus MessageAuthStatus
							decoder.Decode(&authStatus)
							if authStatus.Success {
								println("Auth success")
								state.GuiBus <- Event{
									GuiEventShowMessage,
									GuiReqShowMessage{"Join", "Authentication success"},
								}
							} else {
								println("Auth fail")
								state.GuiBus <- Event{
									GuiEventShowMessage,
									GuiReqShowMessage{"Join", "Authentication failure"},
								}
							}
						default:
							fmt.Printf("Unknown packet of type %d incoming\n", messageType)
						}
					}
				}()
			default:
				fmt.Printf("Fatal: Unknown Net event %d, this should not have happened\n", id)
				os.Exit(1)
			}
		}
		fmt.Println("NetHandle stopped")
	}
}

func boundSendMessage(encoder *msgpack.Encoder, conn net.Conn) func(packetID int, message interface{}) (err error) {
	return func(packetID int, message interface{}) (err error) {
		conn.Write([]byte{byte(packetID)})
		err = encoder.Encode(&message)
		return
	}
}
