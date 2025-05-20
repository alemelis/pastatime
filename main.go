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
	clients     = make(map[string]*Client)
	clientOrder []string // Keep track of client connection order
	clientsMux  sync.Mutex

	activeClientID string // ID of the client currently in control

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Stopwatch state
	isRunning = false
	startTime time.Time
	elapsed   time.Duration
	stateMux  sync.Mutex
	// broadcastCh is no longer needed as updates are sent directly
)

var (
	names      = []string{"pippo", "pluto", "topolino"}
	adjectives = []string{"schifoso", "rognoso", "noioso"}
)

func generateName() string {
	// Use current nanoseconds to ensure a unique name even if multiple clients connect in the same second
	nano := time.Now().UnixNano()
	name := names[nano%int64(len(names))]
	adjective := adjectives[nano%int64(len(adjectives))]
	return name + "_" + adjective
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
	// defer conn.Close() // Close is handled after the read loop

	var id string
	clientsMux.Lock()
	// Generate a unique ID
	for {
		id = generateName()
		_, exists := clients[id]
		if !exists {
			break
		}
	}
	client := &Client{id: id, conn: conn}

	clients[id] = client
	clientOrder = append(clientOrder, id) // Add to order

	// If no client is active, make this one active
	if activeClientID == "" {
		activeClientID = id
		log.Println("Setting initial active client:", id)
	}
	clientsMux.Unlock()

	log.Println("Client connected:", id)
	log.Println("Current client order:", clientOrder)
	log.Println("Active client:", activeClientID)

	// Send initial state to the new client
	sendStateToClient(client)
	// Broadcast state to all clients to inform them of the new active client/order
	broadcastState()

	for {
		var data struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		// Use conn.ReadJSON for convenience
		if err := conn.ReadJSON(&data); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Println("read error:", err)
			}
			break // Exit the read loop on error
		}

		if data.Type == "command" {
			handleCommand(client.id, data.Command) // Pass client ID to handleCommand
		}
	}

	// Clean up on disconnection
	clientsMux.Lock()
	delete(clients, id)

	// Remove client from order
	for i, clientID := range clientOrder {
		if clientID == id {
			clientOrder = append(clientOrder[:i], clientOrder[i+1:]...)
			break
		}
	}

	// If the disconnected client was the active one, pass control
	if activeClientID == id {
		if len(clientOrder) > 0 {
			// Find the next client in the *remaining* list
			// The current activeClientID is already removed from clientOrder
			// So the next client is simply the first one in the new list
			activeClientID = clientOrder[0] // Pass to the next in line (wrap around to first of remaining)
			log.Println("Active client disconnected, passing control to:", activeClientID)
		} else {
			activeClientID = "" // No clients left
			log.Println("Last client disconnected, no active client.")
		}
		// Broadcast the state change
		broadcastState()
	}
	clientsMux.Unlock()

	conn.Close() // Close connection after cleanup
	log.Println("Client disconnected:", id)
	log.Println("Current client order:", clientOrder)
	log.Println("Active client:", activeClientID)
}

// handleCommand now accepts the clientID
func handleCommand(clientID string, cmd string) {
	clientsMux.Lock() // Lock clientsMux to access activeClientID
	if clientID != activeClientID {
		log.Printf("Client %s is not the active client. Ignoring command: %s\n", clientID, cmd)
		clientsMux.Unlock()
		return // Only active client can send commands
	}
	clientsMux.Unlock() // Unlock clientsMux before acquiring stateMux if not handling 'next'

	// Handle 'next' command separately as it modifies clientsMux protected state
	if cmd == "next" {
		clientsMux.Lock() // Re-acquire clientsMux lock for 'next' command
		if len(clientOrder) > 1 {
			// Find the current active client's index
			currentIndex := -1
			for i, id := range clientOrder {
				if id == activeClientID {
					currentIndex = i
					break
				}
			}

			if currentIndex != -1 {
				// Calculate next index (wraps around)
				nextIndex := (currentIndex + 1) % len(clientOrder)
				activeClientID = clientOrder[nextIndex]
				log.Println("Control passed to next client:", activeClientID)
			} else {
				// Should not happen if activeClientID is always in clientOrder
				log.Println("Active client ID not found in client order list.")
			}
		} else {
			log.Println("Only one client connected, cannot pass control.")
		}
		clientsMux.Unlock() // Unlock clientsMux

		// Broadcast the state change outside the mutexes
		go broadcastState() // Use a goroutine to avoid blocking handleCommand
		return              // 'next' command handled
	}

	// Handle other commands (start, pause, reset) which require stateMux lock
	stateMux.Lock()
	defer stateMux.Unlock() // Ensure stateMux is unlocked

	log.Printf("Active client %s processing command: %s\n", clientID, cmd)

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
	// State change occurred, broadcast update
	go broadcastState() // Use a goroutine
}

// timerLoop sends the current timer state periodically
func timerLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop() // Good practice to stop the ticker

	for range ticker.C {
		// Broadcast state to all clients
		broadcastState()
	}
}

// broadcastState sends the current timer value, active client ID, and own client ID to all clients
func broadcastState() {
	clientsMux.Lock()
	defer clientsMux.Unlock()

	if len(clients) == 0 {
		// No clients connected, no need to broadcast
		return
	}

	stateMux.Lock()
	var total time.Duration
	if isRunning {
		total = elapsed + time.Since(startTime)
	} else {
		total = elapsed
	}
	ms := total.Milliseconds()
	stateMux.Unlock()

	// Prepare the base message structure
	baseMsg := map[string]interface{}{
		"type":         "update",
		"time":         ms,
		"activeClient": activeClientID,
	}

	for id, c := range clients {
		// Clone the base message and add the client's own ID
		personalMsg := make(map[string]interface{}, len(baseMsg)+1)
		for k, v := range baseMsg {
			personalMsg[k] = v
		}
		personalMsg["yourId"] = id // Add the client's own ID

		personalData, err := json.Marshal(personalMsg)
		if err != nil {
			log.Println("json marshal error:", err)
			continue // Skip this client if marshal fails
		}

		// Use WriteMessage with goroutine to avoid blocking the loop
		go func(conn *websocket.Conn, data []byte) {
			err := conn.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				log.Println("write error:", err)
				// Note: Error here might indicate a disconnected client.
				// Handle client removal in handleWS read loop upon read error.
			}
		}(c.conn, personalData)
	}
}

// sendStateToClient sends the current timer value, active client ID, and own client ID to a specific client
func sendStateToClient(c *Client) {
	clientsMux.Lock() // Lock clientsMux to access activeClientID
	stateMux.Lock()
	defer stateMux.Unlock()
	defer clientsMux.Unlock() // Ensure clientsMux is unlocked

	var total time.Duration
	if isRunning {
		total = elapsed + time.Since(startTime)
	} else {
		total = elapsed
	}
	ms := total.Milliseconds()

	msg := map[string]interface{}{
		"type":         "update",
		"time":         ms,
		"activeClient": activeClientID,
		"yourId":       c.id, // Add the client's own ID
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Println("json marshal error:", err)
		return // Don't attempt to write if marshal fails
	}

	err = c.conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		log.Println("write error:", err)
	}
}
