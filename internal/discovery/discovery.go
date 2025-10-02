package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

// Presence сообщение, которое узлы рассылают в локальной сети
type Presence struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// StartDiscovery запускает механизм обнаружения соседей через broadcast.
func StartDiscovery(ctx context.Context, name string, tcpPort int, foundPeer func(Presence)) error {
	conn, err := net.ListenPacket("udp4", ":33333")
	if err != nil {
		return err
	}
	defer conn.Close()

	go listenLoop(ctx, conn, foundPeer)
	go broadcastLoop(ctx, name, tcpPort)

	<-ctx.Done()
	return ctx.Err()
}

func listenLoop(ctx context.Context, conn net.PacketConn, foundPeer func(Presence)) {
	buf := make([]byte, 2048)
	for {
		select {
		case <-ctx.Done():
			log.Println("discovery stopped")
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			// таймауты/прочие ошибки — продолжаем
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			continue
		}

		var p Presence
		if err := json.Unmarshal(buf[:n], &p); err == nil {
			log.Printf("[discovery] received presence from %s (%s)", p.Name, addr.String())
			foundPeer(p)
		}
	}
}

func broadcastLoop(ctx context.Context, name string, tcpPort int) {
	presence := Presence{
		Name: name,
		Addr: net.JoinHostPort(GetLocalIP(), fmt.Sprintf("%d", tcpPort)),
	}
	data, _ := json.Marshal(presence)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		addrs, err := getBroadcastAddrs()
		if err != nil {
			log.Printf("[discovery] failed to get broadcast addresses: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, addr := range addrs {
			// явно посылаем на каждый broadcast-адрес интерфейса
			conn, err := net.DialUDP("udp4", nil, addr)
			if err != nil {
				continue
			}
			_, err = conn.Write(data)
			conn.Close()
			if err == nil {
				log.Printf("[discovery] broadcast sent to %s", addr.String())
			}
		}

		time.Sleep(3 * time.Second)
	}
}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}

func getBroadcastAddrs() ([]*net.UDPAddr, error) {
	var addrs []*net.UDPAddr
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		// пропускаем down и loopback
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrsList, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, a := range addrsList {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}

			ip := ipnet.IP.To4()

			// пропускаем ненужные диапазоны
			if ip[0] == 169 && ip[1] == 254 { // link-local
				continue
			}
			if ip[0] == 172 && (ip[1] == 17 || ip[1] == 18) { // docker/wsl
				continue
			}

			// оставляем только нормальные подсети
			broadcast := make(net.IP, len(ip))
			for i := 0; i < 4; i++ {
				broadcast[i] = ip[i] | ^ipnet.Mask[i]
			}

			addr := &net.UDPAddr{IP: broadcast, Port: 33333}
			addrs = append(addrs, addr)
		}
	}
	return addrs, nil
}
