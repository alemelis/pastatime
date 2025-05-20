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

type Lap struct {
	Client string        `json:"client"`
	Time   time.Duration `json:"time"`
	TimeMs int64         `json:"timeMs"` // included for ease of use on the frontend
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
	isRunning     = false
	startTime     time.Time
	elapsed       time.Duration
	lastLapTime   time.Duration // To store the lap time duration
	lastLapClient string        // To store the ID of the client who set the last lap
	lapHistory    []Lap
	stateMux      sync.Mutex
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
	if activeClientID == "" && len(clientOrder) > 0 {
		activeClientID = clientOrder[0] // Assign first client as active
		log.Println("Setting initial active client:", activeClientID)
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
			// Find the index of the disconnected client in the *original* order list
			// (before removal) to determine who was next.
			// Simpler approach: just assign to the first client in the new list.
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
	clientsMux.Unlock() // Unlock clientsMux before potentially acquiring stateMux

	// Handle 'next' command separately as it modifies clientsMux protected state
	if cmd == "next" {
		stateMux.Lock() // Lock stateMux to read current time and reset

		// Calculate the total elapsed time for the current lap
		var currentLap time.Duration
		if isRunning {
			currentLap = elapsed + time.Since(startTime)
		} else {
			currentLap = elapsed
		}
		lastLapTime = currentLap // Store the lap time duration
		lastLapClient = clientID // Store the client ID who set the lap time

		// Append the lap to the history
		lapHistory = append(lapHistory, Lap{Client: clientID, Time: currentLap, TimeMs: currentLap.Milliseconds()})
		log.Println("Lap added to history. Current lapHistory:", lapHistory) // Log lapHistory

		// Reset the stopwatch state and start for the next client.
		isRunning = true       // Start immediately for the next client
		startTime = time.Now() // Set new start time
		elapsed = 0            // Reset elapsed time

		stateMux.Unlock() // Unlock stateMux

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
				// Fallback: assign to first client if current not found (shouldn't be needed)
				if len(clientOrder) > 0 {
					activeClientID = clientOrder[0]
				} else {
					activeClientID = "" // No clients left
				}
			}
		} else {
			log.Println("Only one client connected, cannot pass control.")
			// If only one client, reset and start for them again
			stateMux.Lock()
			isRunning = true
			startTime = time.Now()
			elapsed = 0
			lastLapTime = 0 // Clear lap time if only one client
			lastLapClient = ""
			stateMux.Unlock()
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
		lastLapTime = 0 // Reset lap time as well
		lastLapClient = ""
		lapHistory = []Lap{} // Clear lap history
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

// broadcastState sends the current timer value, active client ID, lap time, and own client ID to all clients
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
	lapMs := lastLapTime.Milliseconds() // Get lap time in milliseconds
	lapClient := lastLapClient          // Get last lap client ID
	history := lapHistory               //Get lap history
	stateMux.Unlock()

	// log.Println("Broadcasting state. Current lapHistory:", history) // Log lapHistory
	// Prepare the base message structure
	baseMsg := map[string]interface{}{
		"type":          "update",
		"time":          ms,
		"lapTime":       lapMs,     // Add lap time
		"lastLapClient": lapClient, // Add last lap client ID
		"lapHistory":    history,
		"activeClient":  activeClientID,
	}

	for id, c := range clients {
		// Clone the base message and add the client's own ID
		personalMsg := make(map[string]interface{}, len(baseMsg)+1)
		for k, v := range baseMsg {
			personalMsg[k] = v
		}
		personalMsg["yourId"] = id // Add the client's own ID

		data, err := json.Marshal(personalMsg)
		if err != nil {
			log.Println("json marshal error:", err)
			continue // Skip this client if marshal fails
		}

		// Use WriteMessage with goroutine to avoid blocking the loop
		go func(conn *websocket.Conn, data []byte) {
			err := conn.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				// log.Println("write error:", err) // Suppress frequent write errors from disconnecting clients
				// Note: Error here might indicate a disconnected client.
				// Handle client removal in handleWS read loop upon read error.
			}
		}(c.conn, data)
	}
}

// sendStateToClient sends the current timer value, active client ID, lap time, and own client ID to a specific client
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
	lapMs := lastLapTime.Milliseconds() // Get lap time in milliseconds
	lapClient := lastLapClient          // Get last lap client ID
	history := lapHistory
	//history := []Lap{} //empty array
	msg := map[string]interface{}{
		"type":          "update",
		"time":          ms,
		"lapTime":       lapMs,     // Add lap time
		"lastLapClient": lapClient, // Add last lap client ID
		"lapHistory":    history,
		"activeClient":  activeClientID,
		"yourId":        c.id, // Add the client's own ID
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
