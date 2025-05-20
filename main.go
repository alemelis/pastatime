package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	id   string
	conn *websocket.Conn
}

var (
	clients    = make(map[string]*Client)
	clientsMux sync.Mutex

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Stopwatch state
	isRunning   = false
	startTime   time.Time
	elapsed     time.Duration
	stateMux    sync.Mutex
	broadcastCh = make(chan int64, 10)
)

var (
	names      = []string{"ciccio", "pluto", "topolino"}
	adjectives = []string{"pasticcio", "rognoso", "noioso"}
)

func generateName() string {
	return names[time.Now().Unix()%int64(len(names))] + "_" + adjectives[time.Now().Unix()%int64(len(adjectives))]
}

func main() {
	go timerLoop()

	http.HandleFunc("/ws", handleWS)
	http.Handle("/", http.FileServer(http.Dir("./frontend")))

	log.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}
	defer conn.Close()

	var id string
	for {
		id = generateName()
		clientsMux.Lock()
		_, exists := clients[id]
		clientsMux.Unlock()
		if !exists {
			break
		}
	}
	client := &Client{id: id, conn: conn}

	clientsMux.Lock()
	clients[id] = client
	clientsMux.Unlock()

	log.Println("Client connected:", id)

	// Send initial state
	stateMux.Lock()
	initial := elapsed
	if isRunning {
		initial += time.Since(startTime)
	}
	stateMux.Unlock()
	sendUpdateToClient(client, initial.Milliseconds())

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Println("read error:", err)
			break
		}

		var data struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		if err := json.Unmarshal(msg, &data); err != nil {
			log.Println("bad JSON:", err)
			continue
		}

		if data.Type == "command" {
			handleCommand(data.Command)
		}
	}

	clientsMux.Lock()
	delete(clients, id)
	clientsMux.Unlock()
	log.Println("Client disconnected:", id)
}

func handleCommand(cmd string) {
	stateMux.Lock()
	defer stateMux.Unlock()

	switch cmd {
	case "start":
		if !isRunning {
			startTime = time.Now()
			isRunning = true
		}
	case "pause":
		if isRunning {
			elapsed += time.Since(startTime)
			isRunning = false
		}
	case "reset":
		isRunning = false
		elapsed = 0
	}
}

func timerLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	for range ticker.C {
		stateMux.Lock()
		var total time.Duration
		if isRunning {
			total = elapsed + time.Since(startTime)
		} else {
			total = elapsed
		}
		ms := total.Milliseconds()
		stateMux.Unlock()

		broadcastUpdate(ms)
	}
}

func broadcastUpdate(ms int64) {
	clientsMux.Lock()
	defer clientsMux.Unlock()

	for _, c := range clients {
		sendUpdateToClient(c, ms)
	}
}

func sendUpdateToClient(c *Client, ms int64) {
	msg := map[string]interface{}{
		"type": "update",
		"time": ms,
	}
	data, _ := json.Marshal(msg)
	c.conn.WriteMessage(websocket.TextMessage, data)
}
