package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/goombaio/namegenerator"
	"github.com/gorilla/websocket"
)

type Session struct {
	ID             string
	clients        map[string]*Client
	clientOrder    []string
	clientsMux     sync.Mutex
	activeClientID string
	turnsCompleted int
	isRunning      bool
	startTime      time.Time
	elapsed        time.Duration
	lastLapTime    time.Duration
	lastLapClient  string
	lapHistory     []Lap
	stateMux       sync.Mutex
}

type Client struct {
	id   string
	conn *websocket.Conn
}

type Lap struct {
	Client string        `json:"client"`
	Time   time.Duration `json:"time"`
	TimeMs int64         `json:"timeMs"`
}

var (
	sessions    = make(map[string]*Session)
	sessionsMux sync.Mutex
	upgrader    = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

func generateName() string {
	seed := time.Now().UTC().UnixNano()
	nameGenerator := namegenerator.NewNameGenerator(seed)
	return nameGenerator.Generate()
}

// setContentType is a middleware to force correct content types
func setContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, ".css") {
			w.Header().Set("Content-Type", "text/css")
		} else if strings.HasSuffix(path, ".js") {
			w.Header().Set("Content-Type", "application/javascript")
		}
		next.ServeHTTP(w, r)
	})
}

// serveFiles serves static files and logs errors if the file is not found
func serveFiles(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// Check if the file exists
	absPath, err := filepath.Abs("frontend" + path)
	if err != nil {
		log.Println("Error:", err)
		http.NotFound(w, r)
		return
	}
	//Log access
	log.Printf("Serving file: %s\n", absPath)
	http.ServeFile(w, r, absPath)
}

func main() {
	// The timerLoop will now be started per session

	// Handler for the landing page
	http.HandleFunc("/", handleIndex)

	// Handler to create a new session
	http.HandleFunc("/new-session", handleNewSession)

	// Refined routing using a simple multiplexer or check in handler
	// Let's check the path in a single handler for /s/
	http.HandleFunc("/s/", handleSession)

	// Serve static files using a custom handler
	fileServer := http.HandlerFunc(serveFiles)
	// Apply the setContentType middleware
	wrappedFileServer := setContentType(fileServer)
	// Use the wrapped file server
	http.Handle("/style.css", wrappedFileServer)
	http.Handle("/script.js", wrappedFileServer)
	http.Handle("/session.css", wrappedFileServer)
	http.Handle("/session.js", wrappedFileServer)

	log.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// handleIndex serves the landing page (index.html)
func handleIndex(w http.ResponseWriter, r *http.Request) {
	// Only handle requests to the root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "./frontend/index.html")
}

// handleNewSession creates a new game session and returns its ID
func handleNewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { // Recommend POST for creating resources
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionsMux.Lock()
	defer sessionsMux.Unlock()

	// Generate a unique session ID
	sessionID := uuid.New().String()

	// Create a new session state
	session := &Session{
		ID:             sessionID,
		clients:        make(map[string]*Client),
		clientOrder:    []string{},
		activeClientID: "",
		turnsCompleted: 0,
		isRunning:      false,
		elapsed:        0,
		lastLapTime:    0,
		lastLapClient:  "",
		lapHistory:     []Lap{},
	}

	sessions[sessionID] = session
	log.Printf("Created new session: %s\n", sessionID)

	// Start the timer loop for this specific session
	go session.timerLoop()

	// Return the new session ID
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"sessionId": sessionID})
}

// handleSession routes requests based on the path after /s/
func handleSession(w http.ResponseWriter, r *http.Request) {
	// Extract the path after /s/
	sessionPath := strings.TrimPrefix(r.URL.Path, "/s/")
	pathSegments := strings.Split(sessionPath, "/")

	// The first segment should be the session ID
	if len(pathSegments) < 1 || pathSegments[0] == "" {
		http.NotFound(w, r)
		return
	}
	sessionID := pathSegments[0]

	// Check if the session exists
	sessionsMux.Lock()
	session, exists := sessions[sessionID]
	sessionsMux.Unlock()

	if !exists {
		log.Printf("Session not found: %s\n", sessionID)
		http.NotFound(w, r)
		return
	}

	// Determine if it's a WebSocket request or an HTML page request
	if len(pathSegments) == 2 && pathSegments[1] == "ws" {
		// This is a WebSocket request for a specific session
		handleSessionWS(session, w, r)
	} else if len(pathSegments) == 1 || (len(pathSegments) == 2 && pathSegments[1] == "") {
		// This is a request for the session HTML page
		handleSessionPage(w, r, session)
	} else {
		http.NotFound(w, r)
	}
}

// handleSessionPage serves the session HTML page (session.html) for a specific session
func handleSessionPage(w http.ResponseWriter, r *http.Request, session *Session) {
	// Serve the session HTML file
	http.ServeFile(w, r, "./frontend/session.html")
}

// handleSessionWS handles WebSocket connections for a specific session
func (s *Session) timerLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		s.clientsMux.Lock()
		numClients := len(s.clients)
		s.clientsMux.Unlock()

		if numClients == 0 {
			continue
		}
		s.broadcastState()
	}
}

func handleSessionWS(session *Session, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Session %s: upgrade error: %v\n", session.ID, err)
		return
	}

	// Add client to the session
	session.clientsMux.Lock()
	var clientID string
	for {
		clientID = generateName()
		_, existsInSession := session.clients[clientID]
		if !existsInSession {
			break
		}
	}
	client := &Client{id: clientID, conn: conn}

	session.clients[clientID] = client
	session.clientOrder = append(session.clientOrder, clientID)

	if session.activeClientID == "" && len(session.clientOrder) > 0 {
		session.activeClientID = session.clientOrder[0]
		log.Printf("Session %s: Setting initial active client: %s\n", session.ID, session.activeClientID)
	}
	session.clientsMux.Unlock()

	log.Printf("Session %s: Client connected: %s\n", session.ID, clientID)
	log.Printf("Session %s: Current client order: %v\n", session.ID, session.clientOrder)
	log.Printf("Session %s: Active client: %s\n", session.ID, session.activeClientID)

	session.sendStateToClient(client)
	session.broadcastState()

	for {
		var data struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		if err := conn.ReadJSON(&data); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Session %s: read error for client %s: %v\n", session.ID, clientID, err)
			}
			break
		}

		if data.Type == "command" {
			session.handleCommand(clientID, data.Command)
		}
	}

	session.clientsMux.Lock()
	delete(session.clients, clientID)

	for i, id := range session.clientOrder {
		if id == clientID {
			session.clientOrder = append(session.clientOrder[:i], session.clientOrder[i+1:]...)
			break
		}
	}

	if session.activeClientID == clientID {
		if len(session.clientOrder) > 0 {
			session.activeClientID = session.clientOrder[0]
			log.Printf("Session %s: Active client disconnected, passing control to: %s\n", session.ID, session.activeClientID)

		} else {
			session.activeClientID = ""
			log.Printf("Session %s: Last client disconnected, no active client.\n", session.ID)
		}
		session.broadcastState()
	}
	session.clientsMux.Unlock()

	conn.Close()
	log.Printf("Session %s: Client disconnected: %s\n", session.ID, clientID)
	log.Printf("Session %s: Current client order: %v\n", session.ID, session.clientOrder)
	log.Printf("Session %s: Active client: %s\n", session.ID, session.activeClientID)
}

// handleCommand now operates on the Session instance
func (s *Session) handleCommand(clientID string, cmd string) {
	s.clientsMux.Lock()
	if clientID != s.activeClientID {
		log.Printf("Session %s: Client %s is not the active client. Ignoring command: %s\n", s.ID, clientID, cmd)
		s.clientsMux.Unlock()
		return
	}
	s.clientsMux.Unlock()

	if cmd == "next" {
		s.stateMux.Lock()
		var currentLap time.Duration
		if s.isRunning {
			currentLap = s.elapsed + time.Since(s.startTime)
		} else {
			currentLap = s.elapsed
		}
		s.lastLapTime = currentLap
		s.lastLapClient = clientID

		s.turnsCompleted++
		fmt.Printf("Session %s: Turns completed: %d\n", s.ID, s.turnsCompleted)

		s.lapHistory = append(s.lapHistory, Lap{Client: clientID, Time: currentLap, TimeMs: currentLap.Milliseconds()})
		log.Printf("Session %s: Lap added to history. Current lapHistory: %v\n", s.ID, s.lapHistory)

		s.isRunning = true
		s.startTime = time.Now()
		s.elapsed = 0

		s.stateMux.Unlock()

		s.clientsMux.Lock()
		if len(s.clientOrder) > 1 {
			if s.turnsCompleted >= len(s.clientOrder) {
				s.isRunning = false
				s.elapsed = 0
				s.lastLapTime = 0
				s.lastLapClient = ""
				s.turnsCompleted = 0
				log.Printf("Session %s: All clients have had their turn. Timer stopped.\n", s.ID)
			} else {
				currentIndex := -1
				for i, id := range s.clientOrder {
					if id == s.activeClientID {
						currentIndex = i
						break
					}
				}

				if currentIndex != -1 {
					nextIndex := (currentIndex + 1) % len(s.clientOrder)
					s.activeClientID = s.clientOrder[nextIndex]
					log.Printf("Session %s: Control passed to next client: %s\n", s.ID, s.activeClientID)
				} else {
					log.Printf("Session %s: Active client ID not found in client order list.\n", s.ID)
					if len(s.clientOrder) > 0 {
						s.activeClientID = s.clientOrder[0]
					} else {
						s.activeClientID = ""
					}
				}
			}
		} else {
			log.Printf("Session %s: Only one client connected, cannot pass control.\n", s.ID)
			s.stateMux.Lock()
			s.isRunning = true
			s.startTime = time.Now()
			s.elapsed = 0
			s.lastLapTime = 0
			s.lastLapClient = ""
			s.turnsCompleted = 0
			s.stateMux.Unlock()
		}
		s.clientsMux.Unlock()

		go s.broadcastState()
		return
	}

	s.stateMux.Lock()
	defer s.stateMux.Unlock()

	log.Printf("Session %s: Active client %s processing command: %s\n", s.ID, clientID, cmd)

	switch cmd {
	case "start":
		if !s.isRunning {
			s.startTime = time.Now()
			s.isRunning = true
		}
	case "pause":
		if s.isRunning {
			s.elapsed += time.Since(s.startTime)
			s.isRunning = false
		}
	case "reset":
		s.isRunning = false
		s.elapsed = 0
		s.lastLapTime = 0
		s.lastLapClient = ""
		s.lapHistory = []Lap{}
		s.turnsCompleted = 0
	}
	go s.broadcastState()
}

// broadcastState sends the current timer value, active client ID, lap time, and own client ID to all clients in this session
func (s *Session) broadcastState() {
	s.clientsMux.Lock()
	currentClients := make(map[string]*Client, len(s.clients))
	for id, client := range s.clients {
		currentClients[id] = client
	}
	s.clientsMux.Unlock()

	s.stateMux.Lock()
	var total time.Duration
	if s.isRunning {
		total = s.elapsed + time.Since(s.startTime)
	} else {
		total = s.elapsed
	}
	ms := total.Milliseconds()
	lapMs := s.lastLapTime.Milliseconds()
	lapClient := s.lastLapClient
	history := s.lapHistory
	s.stateMux.Unlock()

	clientIDs := make([]string, 0, len(currentClients))
	for id := range currentClients {
		clientIDs = append(clientIDs, id)
	}

	baseMsg := map[string]interface{}{
		"type":          "update",
		"time":          ms,
		"lapTime":       lapMs,
		"lastLapClient": lapClient,
		"lapHistory":    history,
		"activeClient":  s.activeClientID,
		"clients":       clientIDs,
	}

	for id, c := range currentClients {
		personalMsg := make(map[string]interface{}, len(baseMsg)+1)
		for k, v := range baseMsg {
			personalMsg[k] = v
		}
		personalMsg["yourId"] = id

		data, err := json.Marshal(personalMsg)
		if err != nil {
			log.Printf("Session %s: json marshal error for client %s: %v\n", s.ID, id, err)
			continue
		}

		go func(conn *websocket.Conn, data []byte) {
			err := conn.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				//log.Printf("Session %s: write error for client %s: %v\n", s.ID, id, err)
			}
		}(c.conn, data)
	}
}

// sendStateToClient sends the current timer value, active client ID, lap time, and own client ID to a specific client in this session
func (s *Session) sendStateToClient(c *Client) {
	s.clientsMux.Lock()
	currentClients := make(map[string]*Client, len(s.clients))
	for id, client := range s.clients {
		currentClients[id] = client
	}
	s.clientsMux.Unlock()

	s.stateMux.Lock()
	defer s.stateMux.Unlock()

	var total time.Duration
	if s.isRunning {
		total = s.elapsed + time.Since(s.startTime)
	} else {
		total = s.elapsed
	}
	ms := total.Milliseconds()
	lapMs := s.lastLapTime.Milliseconds()
	lapClient := s.lastLapClient
	history := s.lapHistory

	clientIDs := make([]string, 0, len(currentClients))
	for id := range currentClients {
		clientIDs = append(clientIDs, id)
	}

	msg := map[string]interface{}{
		"type":          "update",
		"time":          ms,
		"lapTime":       lapMs,
		"lastLapClient": lapClient,
		"lapHistory":    history,
		"activeClient":  s.activeClientID,
		"yourId":        c.id,
		"clients":       clientIDs,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Session %s: json marshal error for client %s: %v\n", s.ID, c.id, err)
		return
	}

	err = c.conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		log.Printf("Session %s: write error for client %s: %v\n", s.ID, c.id, err)
	}
}
