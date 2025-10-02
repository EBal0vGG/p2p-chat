package peer

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/example/p2p-chat/internal/messages"
)

type Manager struct {
	selfID   string
	selfName string

	mu    sync.RWMutex
	m     map[string]Peer
	conns map[string]net.Conn
}

func NewManager(selfID, selfName string) *Manager {
	return &Manager{
		selfID:   selfID,
		selfName: selfName,
		m:        make(map[string]Peer),
		conns:    make(map[string]net.Conn),
	}
}

func (pm *Manager) AddOrUpdate(p Peer) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	p.LastSeen = time.Now()
	pm.m[p.ID] = p
}

func (pm *Manager) Remove(id string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.m, id)
	if c, ok := pm.conns[id]; ok {
		c.Close()
		delete(pm.conns, id)
	}
}

func (pm *Manager) List() []Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	out := make([]Peer, 0, len(pm.m))
	for _, v := range pm.m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (pm *Manager) String() string {
	peers := pm.List()
	if len(peers) == 0 {
		return "(no peers)"
	}
	s := "peers:\n"
	for _, p := range peers {
		s += fmt.Sprintf(" - %s @ %s:%d (last=%v)\n", p.Name, p.Addr, p.TCPPort, p.LastSeen.Format(time.RFC3339))
	}
	return s
}

// --- connection management ---

// SetConn registers (or replaces) a net.Conn for given peer ID.
func (pm *Manager) SetConn(peerID string, c net.Conn) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// close old if exists
	if old, ok := pm.conns[peerID]; ok {
		_ = old.Close()
	}
	pm.conns[peerID] = c
}

// HasConn reports whether we have a live conn to peerID.
func (pm *Manager) HasConn(peerID string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	_, ok := pm.conns[peerID]
	return ok
}

// RemoveConn closes and removes connection for peerID.
func (pm *Manager) RemoveConn(peerID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if c, ok := pm.conns[peerID]; ok {
		_ = c.Close()
		delete(pm.conns, peerID)
	}
}

// Broadcast — отправляет сообщение всем активным соединениям (не блокирующе для основного map).
func (pm *Manager) Broadcast(msg messages.Message) {
	pm.mu.RLock()
	conns := make([]net.Conn, 0, len(pm.conns))
	for _, c := range pm.conns {
		conns = append(conns, c)
	}
	pm.mu.RUnlock()

	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	for _, c := range conns {
		// Best-effort send; errors ignored in MVP.
		_, _ = c.Write(data)
	}
}

// CloseAll — закрывает все соединения.
func (pm *Manager) CloseAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for id, c := range pm.conns {
		_ = c.Close()
		delete(pm.conns, id)
	}
}
