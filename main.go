package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/rs/xid"
)

// MessageType message type
type MessageType string

const (
	// MessageTypeRequest request
	MessageTypeRequest MessageType = "request"
	// MessageTypeMessage message
	MessageTypeMessage MessageType = "message"
)

// Message message
type Message struct {
	FromID      string      `json:"fromID"`
	ToID        string      `json:"toID"`
	MessageType MessageType `json:"messageType"`
	Body        string      `json:"body"`
}

// UserResponse represents an user using the thing
type UserResponse struct {
	Nickname string `json:"nickname"`
	ID       string `id`
}

var (
	// ErrInvalidToID invalid to id
	ErrInvalidToID = errors.New("invalid to id")
)

// MainRoomID is the ID of the main room where everyone is
const MainRoomID string = "mainRoom"

var clients = map[string]*websocket.Conn{}
var nicknames = map[string]string{} // This maps the ID's with the nicknames
var broadcast = make(chan Message)

func main() {
	router := mux.NewRouter()
	upgrader := websocket.Upgrader{}
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	router.HandleFunc("/", hello)
	router.HandleFunc("/nickname", getAllUserS)
	router.HandleFunc("/ws", handleWs(upgrader))
	log.Println("Listening...")
	log.Fatal(http.ListenAndServe(":5000", router))
}

func getAllUserS(w http.ResponseWriter, r *http.Request) {
	users := []UserResponse{}
	for id, nickname := range nicknames {
		users = append(users, UserResponse{nickname, id})
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
		nicknames[guid.String()] = "syned13"

		log.Println("new client")

		for {
			var msg Message
			err := ws.ReadJSON(&msg)
			if err != nil {
				log.Println("unmarshalling_message_failed")
				// TODO: give feedback to the user when an error happens
				delete(clients, guid.String())
			}

			log.Println("received_message")
			broadcast <- msg
		}
	}
}

func handleMessages() {
	for {
		msg := <-broadcast
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
