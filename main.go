package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/rs/xid"
)

/*
User -> Server (opens websocket connection)
Server -> User (gives the user its id)(registration message)
User -> Server (gives the server its nickname)(registration message)
*/

// MessageType message type
type MessageType string

const (
	// MessageTypeRequest request
	MessageTypeRequest MessageType = "request"
	// MessageTypeMessage message
	MessageTypeMessage MessageType = "message"
	// MessageTypeRegistration registration
	MessageTypeRegistration MessageType = "registration"
)

// Message message
type Message struct {
	FromID      string      `json:"fromID"`
	ToID        string      `json:"toID"`
	MessageType MessageType `json:"messageType"`
	Body        string      `json:"body"`
}

// User represents an user using the thing
type User struct {
	Nickname string `json:"nickname"`
	ID       string `id`
	// Something else, what comes
}

var (
	// ErrInvalidToID invalid to id
	ErrInvalidToID = errors.New("invalid to id")
)

// MainRoomID is the ID of the main room where everyone is
const MainRoomID string = "mainRoom"

// ServerRoomID is the ToID to messages directed to the server
const ServerRoomID string = "server"

var clients = map[string]*websocket.Conn{}
var onlineUsers = map[string]*User{} // This maps the ID's with the user structs
var broadcast = make(chan Message)

func main() {
	router := mux.NewRouter()
	upgrader := websocket.Upgrader{}
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	router.HandleFunc("/", hello)
	router.HandleFunc("/nickname", getAllUserS)
	router.HandleFunc("/ws", handleWs(upgrader))

	loggedRouter := handlers.LoggingHandler(os.Stdout, router)

	go handleMessages()

	log.Println("Listening...")
	log.Fatal(http.ListenAndServe(":5000", loggedRouter))

}

func getAllUserS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	users := []User{}
	for id, user := range onlineUsers {
		users = append(users, User{user.Nickname, id})
	}

	RespondJSON(w, http.StatusOK, users)
}

// RespondJSON responds with a json with the given status code and data
func RespondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

func hello(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello world!"))
}

func handleWs(upgrader websocket.Upgrader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("new client!")
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			// TODO: handle err
			log.Fatal(err)
		}

		defer ws.Close()

		guid := xid.New()
		clients[guid.String()] = ws
		onlineUsers[guid.String()] = &User{Nickname: "", ID: guid.String()}

		log.Println("new client")

		message := Message{
			FromID:      ServerRoomID,
			ToID:        guid.String(),
			MessageType: MessageTypeRegistration,
		}

		err = ws.WriteJSON(message)
		if err != nil {
			// TODO: handle error
			log.Fatal("failed to send registration message to client")
		}

		ws.SetCloseHandler(clientCloseHandler)

		for {
			var msg Message
			err = ws.ReadJSON(&msg)
			if websocket.IsUnexpectedCloseError(err) {
				delete(clients, guid.String())
				delete(onlineUsers, guid.String())
			}

			if err != nil {
				log.Println("unmarshalling_message_failed" + err.Error())
				// TODO: give feedback to the user when an error happens
				delete(clients, guid.String())
			}

			fmt.Println(msg.Body)
			log.Println("received_message")
			broadcast <- msg
		}
	}
}

func clientCloseHandler(code int, text string) error {
	fmt.Println("closeeeed!")

	return nil
}

func handleMessages() {
	for {
		msg := <-broadcast
		if msg.ToID == ServerRoomID {
			err := handleServerMessage(msg)
			if err != nil {
				log.Println("failed")
			}
		}

		if msg.ToID == MainRoomID {
			err := sendMessageToMainRoom(msg)
			if err != nil {
				// what ?
				log.Println("failed")
			}
		}

		if msg.MessageType == MessageTypeRequest {
			err := sendMessageRequest(msg)
			if err != nil {
				log.Println("failed")
			}
		}
	}
}

func sendMessageRequest(msg Message) error {
	ws, ok := clients[msg.ToID]
	if !ok {
		return ErrInvalidToID
	}

	return ws.WriteJSON(msg)
}

func sendMessageToMainRoom(msg Message) error {
	for _, ws := range clients {
		err := ws.WriteJSON(ws)
		if err != nil {
			log.Println("failed_to_send_message")
		}
	}

	return nil
}

func handleServerMessage(msg Message) error {
	// TODO: check for missing attributes
	if msg.MessageType == MessageTypeRegistration {
		fmt.Println("Registration message: " + msg.Body)
		onlineUsers[msg.FromID].Nickname = msg.Body
	}

	return nil
}
