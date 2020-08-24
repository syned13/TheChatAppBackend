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
	"github.com/subosito/gotenv"
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
	// MessageTypeError error
	MessageTypeError MessageType = "error"
	// MessageTypeNewUser new user
	MessageTypeNewUser MessageType = "newUser"
	// MessageTypeGoneUser gone user
	MessageTypeGoneUser MessageType = "goneUser"

	// MainRoomID is the ID of the main room where everyone is
	MainRoomID string = "mainRoom"
	// ServerRoomID is the ToID to messages directed to the server
	ServerRoomID string = "server"
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
	ID       string `json:"id"`
	// Something else, what comes
}

// BroadcastBody is the struct sent to the broadcast channel
type BroadcastBody struct {
	msg Message
	ws  *websocket.Conn
}

var (
	// ErrInvalidToID invalid to id
	ErrInvalidToID = errors.New("invalid to id")
	// ErrMissingFromID missing from id
	ErrMissingFromID = errors.New("missing from id")
	// ErrMissingBody missing body
	ErrMissingBody = errors.New("missing body")
)

var (
	port        string
	clients     = map[string]*websocket.Conn{}
	onlineUsers = map[string]*User{} // This maps the ID's with the user structs
	broadcast   = make(chan BroadcastBody)
)

func init() {
	gotenv.Load()
	port = os.Getenv("PORT")
}
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
	log.Fatal(http.ListenAndServe(":"+port, loggedRouter))
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
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("upgrading_connection_failed: " + err.Error())
			return
		}

		defer ws.Close()

		// Registers the new client
		guid := xid.New()
		clients[guid.String()] = ws
		onlineUsers[guid.String()] = &User{Nickname: "", ID: guid.String()}

		ws.SetCloseHandler(clientCloseHandler)

		log.Println("new client: " + guid.String())

		err = sendRegistrationMessage(guid.String())
		if err != nil {
			log.Println("sending_registration_message_to_client_failed: " + err.Error())

			delete(clients, guid.String())
			delete(onlineUsers, guid.String())
			sendGoneUserMessage(guid.String())
			ws.Close()
		}

		listenForMessages(ws, guid.String())
	}
}

func listenForMessages(ws *websocket.Conn, id string) {
	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if websocket.IsUnexpectedCloseError(err) {
			delete(clients, id)
			delete(onlineUsers, id)
			sendGoneUserMessage(id)

			return
		}

		if err != nil {
			log.Println("unmarshalling_message_failed: " + err.Error())
			err = sendErrorMessage(id, "invalid_message_format")
			if err != nil {
				delete(clients, id)
				delete(onlineUsers, id)
				sendGoneUserMessage(id)
				ws.Close()
			}

			return
		}

		broadcast <- BroadcastBody{msg: msg, ws: ws}
	}
}

func clientExists(id string) bool {
	_, ok := clients[id]

	return ok
}

func sendRegistrationMessage(id string) error {
	if !clientExists(id) {
		return ErrInvalidToID
	}

	message := Message{
		FromID:      ServerRoomID,
		ToID:        id,
		MessageType: MessageTypeRegistration,
		Body:        id,
	}

	err := clients[id].WriteJSON(message)
	if err != nil {
		log.Print("sending_registration_message_to_client_failed: " + err.Error())

		err = clients[id].Close()
		if err != nil {
			log.Println("closing_connection_failed: " + err.Error())
		}

		return err
	}

	return nil
}

func sendErrorMessage(id string, errorMessage string) error {
	msg := Message{
		FromID:      ServerRoomID,
		ToID:        id,
		MessageType: MessageTypeError,
		Body:        errorMessage,
	}

	if _, ok := clients[id]; !ok {
		return ErrInvalidToID
	}

	return clients[id].WriteJSON(msg)
}

func clientCloseHandler(code int, text string) error {
	fmt.Println("closeeeed!")

	return nil
}

func findClientID(ws *websocket.Conn) string {
	for k, v := range clients {
		if v == ws {
			return k
		}
	}

	return ""
}

func validateMessage(msg Message) error {
	if msg.FromID == "" {
		return ErrMissingFromID
	}

	if msg.ToID == "" {
		return ErrMissingFromID
	}

	if _, ok := clients[msg.ToID]; !ok && msg.ToID != MainRoomID && msg.ToID != ServerRoomID {
		return ErrInvalidToID
	}

	if msg.Body == "" {
		return ErrMissingBody
	}

	return nil
}

func handleMessages() {
	for {
		body := <-broadcast
		err := validateMessage(body.msg)
		if errors.Is(err, ErrMissingFromID) {
			log.Println("missing_client_id_in_message")
			id := findClientID(body.ws)
			if id == "" {
				log.Println("could_not_find_client_id")
			}

			if id != "" {
				delete(clients, id)
				delete(onlineUsers, id)
				sendGoneUserMessage(id)
			}

			body.ws.Close()

			continue
		}

		if err != nil {
			log.Println("invalid_message_received: " + err.Error())
			log.Println(body)

			sendErrorMessage(body.msg.FromID, "invalid message")

			continue
		}

		if body.msg.ToID == ServerRoomID {
			err := handleServerMessage(body.msg)
			if err != nil {
				log.Println("handling_server_message_failed")
				sendErrorMessage(body.msg.FromID, "error")

				continue
			}
		}

		if body.msg.ToID == MainRoomID {
			err := sendMessageToMainRoom(body.msg)
			if err != nil {
				// what ?
				log.Println("sending_message_to_main_room_failed: " + err.Error())
				sendErrorMessage(body.msg.FromID, "error")
				continue
			}
		}

		if body.msg.MessageType == MessageTypeRequest {
			err := sendMessageRequest(body.msg)
			if err != nil {
				log.Println("sending_request_message_failed: " + err.Error())
				sendErrorMessage(body.msg.FromID, "error")
				continue
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
	msgToSend := Message{
		FromID:      msg.FromID,
		ToID:        MainRoomID,
		MessageType: MessageTypeMessage,
		Body:        msg.Body,
	}

	for _, ws := range clients {
		err := ws.WriteJSON(msgToSend)
		if err != nil {
			log.Println("failed_to_send_message")
		}
	}

	return nil
}

func sendNewUserMessage(id string) error {
	msgToSend := Message{
		FromID:      ServerRoomID,
		MessageType: MessageTypeNewUser,
		Body:        id,
	}

	for id, ws := range clients {
		msgToSend.ToID = id
		err := ws.WriteJSON(msgToSend)
		if err != nil {
			log.Println("failed_to_send_message")
		}
	}

	return nil
}

func sendGoneUserMessage(id string) error {
	msgToSend := Message{
		FromID:      ServerRoomID,
		MessageType: MessageTypeGoneUser,
		Body:        id,
	}

	for id, ws := range clients {
		msgToSend.ToID = id
		err := ws.WriteJSON(msgToSend)
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
		sendNewUserMessage(msg.FromID)
	}

	return nil
}
