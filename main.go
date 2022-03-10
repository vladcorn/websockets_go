package main

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"log"
	"math"
	"net/http"
	"strings"
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
	id         string
	clients    []*Client
	PublicKeys map[string]int
	SecretKeys map[string]int
	iteration  int
}

type Data struct {
	Id        string `json:"id,omitempty"`
	RoomId    string `json:"room_id,omitempty"`
	Action    string `json:"action,omitempty"`
	Signature string `json:"signature,omitempty"`
	PublicKey int    `json:"public_key,omitempty"`
	Secret    int    `json:"secret,omitempty"`
	UserId    string `json:"user_id,omitempty"`
}

type KeySwap struct {
	roomId  string
	userIds []string
	data    Data
}

type Hub struct {
	rooms      map[string]*Room
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	keysSwap   chan KeySwap
}

type Client struct {
	Id        string `json:"id,omitempty" `
	RoomId    string `json:"room_id,omitempty"`
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte
	signature string
}

func newHub() *Hub {
	return &Hub{
		register:   make(chan *Client, 100),
		unregister: make(chan *Client, 100),
		broadcast:  make(chan []byte, 100),
		keysSwap:   make(chan KeySwap, 100),
		rooms: map[string]*Room{
			"eb09b78f-975b-44d3-b988-60f6b8d5fb0e": {
				"eb09b78f-975b-44d3-b988-60f6b8d5fb0e",
				make([]*Client, 0),
				make(map[string]int),
				make(map[string]int),
				0,
			},
		},
		clients: make(map[*Client]bool, 100),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true

		case client := <-h.unregister:
			for _, room := range h.rooms {
				for idx, c := range room.clients {
					if c.Id == client.Id {
						room.clients = append(room.clients[:idx], room.clients[idx+1:]...)
						break
					}
				}
			}

			delete(h.clients, client)

		case chData := <-h.keysSwap:
			roomId := chData.roomId
			data := chData.data
			userIds := chData.userIds

			if roomId != "" {
				h.rooms[roomId].PublicKeys[data.UserId] = data.PublicKey
				log.Println(len(h.rooms[roomId].PublicKeys) == len(userIds))

				if len(h.rooms[roomId].PublicKeys) == len(userIds) {
					for i, k := range userIds {
						for _, client := range h.rooms[roomId].clients {
							if client.Id == k {
								var pk int
								log.Println(i, h.rooms[roomId].iteration, int(math.Abs(float64(len(userIds)-(i+h.rooms[roomId].iteration+1)))))
								if i >= len(userIds)-(1+h.rooms[roomId].iteration) {
									pk = h.rooms[roomId].PublicKeys[userIds[int(math.Abs(float64(len(userIds)-(i+h.rooms[roomId].iteration+1))))]]
								} else {
									pk = h.rooms[roomId].PublicKeys[userIds[i+h.rooms[roomId].iteration+1]]
								}
								response := struct {
									Id        string `json:"id"`
									Action    string `json:"action"`
									PublicKey int    `json:"public_key"`
								}{
									Id:        uuid.New().String(),
									Action:    "do_secret",
									PublicKey: pk,
								}
								client.conn.WriteJSON(response)
							}
						}
						if i == len(userIds)-1 {
							h.rooms[roomId].iteration += 1
							h.rooms[roomId].PublicKeys = make(map[string]int)
						}
					}
				}
			}
		case message := <-h.broadcast:

			var data struct {
				Id        string `json:"id,omitempty"`
				RoomId    string `json:"room_id,omitempty"`
				Action    string `json:"action,omitempty"`
				Signature string `json:"signature,omitempty"`
				PublicKey int    `json:"public_key,omitempty"`
				Secret    int    `json:"secret,omitempty"`
				UserId    string `json:"user_id,omitempty"`
			}
			if err := json.Unmarshal(message, &data); err != nil {
				panic(err)
			}

			if _, roomExist := h.rooms[data.RoomId]; roomExist || data.Signature != "" {
				switch data.Action {
				case "join":
					for client := range h.clients {
						if client.Id == data.UserId {
							clients := append(h.rooms[data.RoomId].clients, client)
							h.rooms[data.RoomId] = &Room{
								data.RoomId,
								clients,
								make(map[string]int),
								make(map[string]int),
								0,
							}

							response := struct {
								Status string `json:"status,omitempty"`
								Id     string `json:"id,omitempty"`
							}{
								Status: "ok",
								Id:     data.Id,
							}

							client.conn.WriteJSON(response)
							break
						}
					}
					if data.Signature == "" {
						if len(h.rooms[data.RoomId].clients) > 1 {
							signature := data.RoomId + ":"
							for i, client := range h.rooms[data.RoomId].clients {
								signature += client.Id
								if i != len(h.rooms[data.RoomId].clients)-1 {
									signature += ","
								}
							}
							for _, client := range h.rooms[data.RoomId].clients {
								response := struct {
									Id        string `json:"id"`
									Action    string `json:"action"`
									Signature string `json:"signature"`
									P         int    `json:"p"`
									Q         int    `json:"q"`
								}{
									Id:        uuid.New().String(),
									Action:    "exchange",
									Signature: signature,
									P:         123,
									Q:         234,
								}
								client.conn.WriteJSON(response)
							}
						}
					}
				case "receive_public_key":
					var roomId string
					var userIds []string
					i := strings.IndexByte(data.Signature, ':')
					if i != -1 {
						roomId = data.Signature[:i]
						userIdsStr := data.Signature[i+1:]
						userIds = strings.Split(userIdsStr, ",")
					}
					h.keysSwap <- KeySwap{roomId, userIds, data}

				case "receive_secret":
					var roomId string
					var userIds []string
					i := strings.IndexByte(data.Signature, ':')
					if i != -1 {
						roomId = data.Signature[:i]
						userIdsStr := data.Signature[i+1:]
						userIds = strings.Split(userIdsStr, ",")
					}
					if roomId != "" {
						h.rooms[roomId].SecretKeys[data.UserId] = data.Secret
						if h.rooms[roomId].iteration >= len(userIds)-1 {
							if len(h.rooms[roomId].SecretKeys) == len(userIds) {
								for _, k := range userIds {
									for _, client := range h.rooms[roomId].clients {
										if client.Id == k {
											response := struct {
												Id        string `json:"id"`
												Status    string `json:"status"`
												Signature string `json:"signature"`
											}{
												Id:        uuid.New().String(),
												Status:    "ready",
												Signature: data.Signature,
											}
											client.conn.WriteJSON(response)
										}
									}
								}
								h.rooms[roomId].iteration = 0
								h.rooms[roomId].PublicKeys = make(map[string]int)
							}
						} else {
							data.PublicKey = data.Secret
							h.keysSwap <- KeySwap{roomId, userIds, data}
						}
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
	client := &Client{hub: hub, conn: conn}
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
		m := bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		var data Data
		if err := json.Unmarshal(message, &data); err != nil {
			panic(err)
		}
		c.Id = data.UserId
		c.hub.broadcast <- m
	}
}
