package discovery

import (
    "context"
    "encoding/json"
    "log"
    "net"
    "time"

    "github.com/example/p2p-chat/internal/peer"
)

const announceInterval = 3 * time.Second

// AnnouncePacket — формат UDP broadcast пакета
type AnnouncePacket struct {
    Type    string `json:"type"`
    PeerID  string `json:"peer_id"`
    Name    string `json:"name"`
    TCPPort int    `json:"tcp_port"`
    TS      int64  `json:"ts"`
}

type Discovery struct {
    port    int
    tcpPort int
    id      string
    name    string
    pm      *peer.Manager
}

func New(port, tcpPort int, id, name string, pm *peer.Manager) *Discovery {
    return &Discovery{port: port, tcpPort: tcpPort, id: id, name: name, pm: pm}
}

func (d *Discovery) Run(ctx context.Context) error {
    // UDP listener
    addr := net.UDPAddr{Port: d.port, IP: net.IPv4zero}
    conn, err := net.ListenUDP("udp4", &addr)
    if err != nil {
        return err
    }
    defer conn.Close()

    // Start announcer
    go d.announcer(ctx)

    buf := make([]byte, 2048)
    for {
        select {
        case <-ctx.Done():
            return nil
        default:
            conn.SetReadDeadline(time.Now().Add(1 * time.Second))
            n, remote, err := conn.ReadFromUDP(buf)
            if err != nil {
                if ne, ok := err.(net.Error); ok && ne.Timeout() {
                    continue
                }
                log.Printf("udp read error: %v", err)
                continue
            }

            var pkt AnnouncePacket
            if err := json.Unmarshal(buf[:n], &pkt); err != nil {
                log.Printf("bad announce from %v: %v", remote, err)
                continue
            }

            if pkt.PeerID == d.id {
                // ignore our own announces
                continue
            }

            // add/update peer
            p := peer.Peer{
                ID:      pkt.PeerID,
                Name:    pkt.Name,
                Addr:    remote.IP.String(),
                TCPPort: pkt.TCPPort,
                LastSeen: time.Now(),
            }
            d.pm.AddOrUpdate(p)
            log.Printf("discovered peer: %s @ %s:%d", p.Name, p.Addr, p.TCPPort)
        }
    }
}

func (d *Discovery) announcer(ctx context.Context) {
    baddr := &net.UDPAddr{IP: net.IPv4bcast, Port: d.port}
    conn, err := net.DialUDP("udp4", nil, baddr)
    if err != nil {
        log.Printf("announcer dial error: %v", err)
        return
    }
    defer conn.Close()

    ticker := time.NewTicker(announceInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            pkt := AnnouncePacket{Type: "announce", PeerID: d.id, Name: d.name, TCPPort: d.tcpPort, TS: time.Now().Unix()}
            data, _ := json.Marshal(pkt)
            if _, err := conn.Write(data); err != nil {
                log.Printf("announce write error: %v", err)
            }
        }
    }
}