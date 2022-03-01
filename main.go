package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

func main() {
	setupRoutes()
	log.Fatal(http.ListenAndServe(":8000", nil))
}

func homePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func wsEndpoint(w http.ResponseWriter, r *http.Request) {
	hub := newHub()
	go hub.run()
	upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}

	log.Println("Client Successfully Connected...")

	serveWs(hub, w, r)
}

func setupRoutes() {
	http.HandleFunc("/", homePage)
	http.HandleFunc("/ws", wsEndpoint)
}

type Room struct {
	id      string
	clients []*Client
}

type Hub struct {
	rooms      map[string]Room
	register   chan *Client
	unregister chan *Client
}

type Client struct {
	Id     string `json:"id,omitempty" `
	RoomId string `json:"room_id,omitempty"`
	hub    *Hub
	conn   *websocket.Conn
}

func newHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		rooms:      make(map[string]Room),
	}
}

func newRoom() Room {
	room := Room{id: uuid.New().String(), clients: make([]*Client, 0)}
	return room
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.rooms[client.RoomId] = Room{client.RoomId, append(h.rooms[client.RoomId].clients, client)}
			fmt.Printf("%+v \n", h.rooms)
			json, _ := json.Marshal(client)
			client.conn.WriteJSON(string(json))

		case client := <-h.unregister:

			if _, ok := h.rooms[client.RoomId]; ok {
				var updateClients []*Client
				for _, item := range h.rooms[client.RoomId].clients {
					if item != nil {
						updateClients = append(updateClients, item)
					}
				}

				if len(updateClients) == 0 {
					delete(h.rooms, client.RoomId)
				} else {
					h.rooms[client.RoomId] = Room{
						h.rooms[client.RoomId].id,
						updateClients,
					}
				}

			}
		}
	}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{hub: hub, conn: conn, Id: uuid.New().String()}
	for k, item := range hub.rooms {
		if len(item.clients) < 6 {
			client.RoomId = k
			break
		}
	}
	if client.RoomId == "" {
		room := newRoom()
		client.RoomId = room.id
		hub.rooms[room.id] = room
	}
	log.Println(client.RoomId)
	client.hub.register <- client

	go client.connectionPump()
	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
}

func (c *Client) connectionPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
	}
}
