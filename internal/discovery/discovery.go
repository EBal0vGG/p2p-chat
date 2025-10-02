package discovery

import (
	"encoding/json"
	"log"
	"net"
	"time"

	"github.com/example/p2p-chat/internal/peer"
)

const (
	discoveryPort = 33333
	broadcastIntv = 3 * time.Second
)

type Broadcaster struct {
	self     peer.Peer
	stopChan chan struct{}
	outChan  chan peer.Peer
}

type presenceMsg struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Addr    string `json:"addr"`
	TCPPort int    `json:"tcp_port"`
}

func NewBroadcaster(self peer.Peer) *Broadcaster {
	return &Broadcaster{
		self:     self,
		stopChan: make(chan struct{}),
		outChan:  make(chan peer.Peer, 10),
	}
}

// Публичный канал для получения найденных пиров
func (b *Broadcaster) Peers() <-chan peer.Peer {
	return b.outChan
}

func (b *Broadcaster) Start() {
	go b.listen()
	go b.broadcastLoop()
}

func (b *Broadcaster) Stop() {
	close(b.stopChan)
}

// отправка своих presence
func (b *Broadcaster) broadcastLoop() {
	ticker := time.NewTicker(broadcastIntv)
	defer ticker.Stop()

	msg := presenceMsg{
		ID:      b.self.ID,
		Name:    b.self.Name,
		Addr:    b.self.Addr,
		TCPPort: b.self.TCPPort,
	}
	data, _ := json.Marshal(msg)

	for {
		select {
		case <-b.stopChan:
			return
		case <-ticker.C:
			b.sendBroadcast(data)
		}
	}
}

func (b *Broadcaster) sendBroadcast(data []byte) {
	// broadcast адреса для всех интерфейсов
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Println("[discovery] failed to list interfaces:", err)
		return
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}

		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			bcast := make(net.IP, len(ipnet.IP.To4()))
			for i := 0; i < 4; i++ {
				bcast[i] = ipnet.IP[i] | ^ipnet.Mask[i]
			}
			dst := &net.UDPAddr{IP: bcast, Port: discoveryPort}
			conn, err := net.DialUDP("udp4", nil, dst)
			if err != nil {
				continue
			}
			_, _ = conn.Write(data)
			conn.Close()
			log.Printf("[discovery] broadcast sent to %s", dst)
		}
	}
}

// приём presence от других
func (b *Broadcaster) listen() {
	addr := net.UDPAddr{Port: discoveryPort, IP: net.IPv4zero}
	conn, err := net.ListenUDP("udp4", &addr)
	if err != nil {
		log.Println("[discovery] listen failed:", err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	for {
		select {
		case <-b.stopChan:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, raddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			log.Println("[discovery] read error:", err)
			continue
		}

		var msg presenceMsg
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}

		if msg.ID == b.self.ID {
			// это мы сами
			continue
		}

		peer := peer.Peer{
			ID:       msg.ID,
			Name:     msg.Name,
			Addr:     raddr.IP.String(),
			TCPPort:  msg.TCPPort,
			LastSeen: time.Now(),
		}

		select {
		case b.outChan <- peer:
			log.Printf("discovered peer: %s @ %s:%d", peer.Name, peer.Addr, peer.TCPPort)
		default:
		}
	}
}
