// Live collaboration presence: each connected tab can broadcast its user
// name, cursor position (board mm coordinates) and active tool; the hub
// relays the message to every other subscriber of the same project and
// synthesizes join/leave events. This turns the shared progress socket into
// a lightweight multiplayer channel without any extra endpoint.
package websocket

import (
	"encoding/json"
)

// presenceCursor is the board-space position of a collaborator pointer.
type presenceCursor struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// presenceMessage is the relayed payload for "presence" events.
type presenceMessage struct {
	Type      string         `json:"type"` // "presence"
	Event     string         `json:"event"`
	ProjectID string         `json:"project_id"`
	User      string         `json:"user"`
	Cursor    presenceCursor `json:"cursor"`
	Tool      string         `json:"tool"`
}

// inboundPresence is the client-sent presence update.
type inboundPresence struct {
	Type      string         `json:"type"`
	User      string         `json:"user"`
	Cursor    presenceCursor `json:"cursor"`
	Tool      string         `json:"tool"`
}

// maxUserName bounds the display name sent by clients.
const maxUserName = 40

// handlePresence processes an inbound presence update: it registers the
// user name (synthesizing a join on first sight), then relays the message
// to every other subscriber of the project.
func (h *Hub) handlePresence(c *client, raw []byte) {
	var msg inboundPresence
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	user := sanitizeUserName(msg.User)
	if user == "" {
		return
	}

	projectID := c.project()
	if projectID == "" {
		// Sans abonnement projet il n'y a personne à qui montrer la présence.
		return
	}

	event := "move"
	join := false
	c.mu.Lock()
	if c.user == "" {
		c.user = user
		join = true
	}
	c.mu.Unlock()
	if join {
		event = "join"
	}

	payload, err := json.Marshal(presenceMessage{
		Type:      "presence",
		Event:     event,
		ProjectID: projectID,
		User:      user,
		Cursor:    msg.Cursor,
		Tool:      msg.Tool,
	})
	if err != nil {
		return
	}
	h.relayPresence(c, projectID, payload)
}

// leavePresence broadcasts the leave event of a named client on
// disconnection.
func (h *Hub) leavePresence(c *client) {
	c.mu.RLock()
	user, projectID := c.user, c.project()
	c.mu.RUnlock()
	if user == "" || projectID == "" {
		return
	}
	payload, err := json.Marshal(presenceMessage{
		Type:      "presence",
		Event:     "leave",
		ProjectID: projectID,
		User:      user,
	})
	if err != nil {
		return
	}
	h.relayPresence(c, projectID, payload)
}

// relayPresence sends a presence payload to every subscriber of projectID
// except the emitter. Non-blocking like Publish.
func (h *Hub) relayPresence(emitter *client, projectID string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c == emitter {
			continue
		}
		filter := c.project()
		if filter != "" && filter != projectID {
			continue
		}
		select {
		case c.send <- payload:
		default:
			// Abonné lent : événement perdu, jamais bloqué.
		}
	}
}

// sanitizeUserName trims, bounds and empties control characters.
func sanitizeUserName(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 32 || r == 127 {
			continue
		}
		out = append(out, r)
		if len(out) >= maxUserName {
			break
		}
	}
	return string(out)
}
