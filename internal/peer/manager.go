package peer

import (
    "fmt"
    "sort"
    "sync"
    "time"
)

// Manager — thread-safe менеджер пиров
type Manager struct {
    selfID string
    selfName string
    mu    sync.RWMutex
    m     map[string]Peer
}

func NewManager(selfID, selfName string) *Manager {
    return &Manager{selfID: selfID, selfName: selfName, m: make(map[string]Peer)}
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