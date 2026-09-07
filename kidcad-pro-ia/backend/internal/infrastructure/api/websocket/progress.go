// Package websocket implements the real-time progress hub of the backend
// (/ws/v1/progress). Clients subscribe per project (query string or
// "subscribe" message) and receive the AI job progress events published by
// the application layer through the ProgressPublisher port (contracts.md §3).
package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	gwebsocket "github.com/gorilla/websocket"
	layoutapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/layout"
)

// Connection tuning constants.
const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second // ping applicatif toutes les 30 s (contrat §3)
	sendBufferSize = 64
)

// upgrader accepts every origin: the backend sits behind a reverse proxy in
// production and the CORS policy is enforced at the HTTP layer.
var upgrader = gwebsocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// progressMessage is the JSON payload pushed to subscribers (contrat §3).
type progressMessage struct {
	Type       string  `json:"type"` // "progress"
	JobID      string  `json:"job_id"`
	ProjectID  string  `json:"project_id"`
	Stage      string  `json:"stage"`
	CurrentNet string  `json:"current_net"`
	Percent    float64 `json:"percent"`
	Message    string  `json:"message"`
	Done       bool    `json:"done"`
	Err        string  `json:"error"`
}

// pongMessage is the reply to the application-level {"type":"ping"}.
type pongMessage struct {
	Type string `json:"type"` // "pong"
}

// controlMessage is an inbound client message (subscribe / ping).
type controlMessage struct {
	Type      string `json:"type"`
	ProjectID string `json:"project_id"`
}

// client is one connected browser tab.
type client struct {
	conn      *gwebsocket.Conn
	send      chan []byte
	mu        sync.RWMutex
	projectID string
	user      string // nom de présence (collaboration live), vide si absent
	closed    atomic.Bool
	closeOnce sync.Once
}

// setProject updates the subscription filter (thread-safe).
func (c *client) setProject(id string) {
	c.mu.Lock()
	c.projectID = id
	c.mu.Unlock()
}

// project returns the current subscription filter (thread-safe).
func (c *client) project() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.projectID
}

// closeOnce closes the send channel exactly once and marks the client dead.
func (c *client) close() {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.send)
	})
}

// Hub is the registry of connected clients plus the Publish port used by
// the application layer.
type Hub struct {
	log     *slog.Logger
	mu      sync.RWMutex
	clients map[*client]struct{}
}

// NewHub builds an empty hub.
func NewHub(log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{log: log, clients: make(map[*client]struct{})}
}

// Publish forwards a progress event to the subscribers of projectID.
// A client subscribed without project filter receives everything. Sends are
// non-blocking: a slow client drops the event instead of stalling the job.
// It implements layoutapp.ProgressPublisher.
func (h *Hub) Publish(jobID, projectID string, p layoutapp.RouteProgress) {
	payload, err := json.Marshal(progressMessage{
		Type:       "progress",
		JobID:      jobID,
		ProjectID:  projectID,
		Stage:      p.Stage,
		CurrentNet: p.CurrentNet,
		Percent:    p.Percent,
		Message:    p.Message,
		Done:       p.Done,
		Err:        p.Err,
	})
	if err != nil {
		h.log.Error("ws : sérialisation de l'événement impossible", "err", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		filter := c.project()
		if filter != "" && filter != projectID {
			continue
		}
		select {
		case c.send <- payload:
		default:
			// Abonné trop lent : l'événement est perdu, jamais bloqué.
		}
	}
}

// Broadcast publishes a server-originated event (type + payload) to the
// subscribers of projectID. Used by the collaboration layer to relay
// applied CRDT operations in real time; semantics identical to Publish
// (non-blocking, slow subscribers drop).
func (h *Hub) Broadcast(projectID string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		h.log.Error("ws : sérialisation du broadcast impossible", "err", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		filter := c.project()
		if filter != "" && filter != projectID {
			continue
		}
		select {
		case c.send <- data:
		default:
		}
	}
}

// ServeWS upgrades the HTTP connection and starts the read/write pumps.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws : upgrade impossible", "err", err, "remote", r.RemoteAddr)
		return
	}
	c := &client{
		conn:      conn,
		send:      make(chan []byte, sendBufferSize),
		projectID: r.URL.Query().Get("project_id"),
	}

	h.register(c)

	h.log.Debug("ws : client connecté", "remote", r.RemoteAddr, "project_id", c.project())

	go h.writePump(c)
	go h.readPump(c)
}

// register adds a client to the hub.
func (h *Hub) register(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

// unregister removes a client and releases its resources exactly once.
func (h *Hub) unregister(c *client) {
	h.leavePresence(c) // prévenir les collaborateurs avant de partir
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
	}
	h.mu.Unlock()
	c.close()
	_ = c.conn.Close()
}

// readPump consumes inbound control messages until the socket dies.
func (h *Hub) readPump(c *client) {
	defer h.unregister(c)

	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if gwebsocket.IsUnexpectedCloseError(err,
				gwebsocket.CloseNormalClosure, gwebsocket.CloseGoingAway) {
				h.log.Debug("ws : lecture interrompue", "err", err)
			}
			return
		}

		var msg controlMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue // message non JSON : ignoré silencieusement
		}
		switch msg.Type {
		case "subscribe":
			c.setProject(msg.ProjectID)
			h.log.Debug("ws : abonnement", "project_id", msg.ProjectID)
		case "presence":
			h.handlePresence(c, raw)
		case "ping":
			if payload, err := json.Marshal(pongMessage{Type: "pong"}); err == nil {
				select {
				case c.send <- payload:
				default:
				}
			}
		}
	}
}

// writePump serialises every write on the socket: queued payloads plus the
// 30 s protocol-level ping keeping intermediaries alive.
func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		h.unregister(c)
	}()

	for {
		select {
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Canal fermé par unregister : le serveur clôt proprement.
				_ = c.conn.WriteMessage(gwebsocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(gwebsocket.TextMessage, payload); err != nil {
				h.log.Debug("ws : écriture impossible", "err", err)
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(gwebsocket.PingMessage, nil); err != nil {
				h.log.Debug("ws : ping impossible", "err", err)
				return
			}
		}
	}
}
