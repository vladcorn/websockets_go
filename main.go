package main

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

func main() {
	hub := newHub()
	go hub.run()
	http.HandleFunc("/", homePage)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Client Successfully Connected...")
		serveWs(hub, w, r)
	})

	log.Fatal(http.ListenAndServe(":8000", nil))
}

func homePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

type Room struct {
	id      string
	clients []*Client
}

type Hub struct {
	rooms      map[string]*Room
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
}

type Client struct {
	Id     string `json:"id,omitempty" `
	RoomId string `json:"room_id,omitempty"`
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
}

func newHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte),
		rooms:      map[string]*Room{"eb09b78f-975b-44d3-b988-60f6b8d5fb0e": {"eb09b78f-975b-44d3-b988-60f6b8d5fb0e", make([]*Client, 0)}},
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			json, _ := json.Marshal(client)
			client.conn.WriteJSON(string(json))

		case client := <-h.unregister:
			delete(h.clients, client)

		case message := <-h.broadcast:

			var data struct {
				Id     string `json:"id,omitempty" `
				RoomId string `json:"room_id,omitempty"`
				Action string `json:"action"`
				Ident  string `json:"ident"`
			}
			if err := json.Unmarshal(message, &data); err != nil {
				panic(err)
			}

			if _, ok := h.rooms[data.RoomId]; ok && data.Action == "join" {

				for client := range h.clients {
					clientData := *client

					if clientData.Id == data.Id {
						clients := append(h.rooms[data.RoomId].clients, client)
						h.rooms[data.RoomId] = &Room{data.RoomId, clients}

						response := struct {
							Status string `json:"status,omitempty"`
							Id     string `json:"id,omitempty"`
						}{Status: "ok", Id: clientData.Id}
						log.Println(h.rooms[data.RoomId])
						json, _ := json.Marshal(response)
						client.conn.WriteJSON(string(json))
						break
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
	client.hub.register <- client

	go client.connectionPump()
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
		c.hub.broadcast <- message
	}
}
