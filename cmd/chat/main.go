package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

type Presence struct {
	ID   string
	Name string
	Addr string
}

var (
	name   = flag.String("name", "anon", "Your name")
	port   = flag.Int("tcp-port", 45454, "TCP port to listen on")
	debug  = flag.Bool("debug", false, "Enable debug logging")
	peers  = make(map[string]Presence)
	selfID = uuid.New().String()
)

// Получаем все IPv4 адреса устройства
func getLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ips = append(ips, ipnet.IP.String())
		}
	}
	return ips
}

// Получаем broadcast-адреса для всех интерфейсов
func getBroadcastAddrs() []string {
	var result []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return result
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ip := ipnet.IP.To4()
				mask := ipnet.Mask
				bcast := net.IPv4(
					ip[0]|^mask[0],
					ip[1]|^mask[1],
					ip[2]|^mask[2],
					ip[3]|^mask[3],
				)
				result = append(result, fmt.Sprintf("%s:33333", bcast.String()))
			}
		}
	}
	return result
}

// Отправляем broadcast presence
func broadcastPresence(conn *net.UDPConn) {
	localIPs := getLocalIPs()
	broadcastIPs := getBroadcastAddrs()

	for _, lip := range localIPs {
		p := Presence{ID: selfID, Name: *name, Addr: fmt.Sprintf("%s:%d", lip, *port)}
		data, _ := json.Marshal(p)

		for _, baddr := range broadcastIPs {
			host, portStr, _ := strings.Cut(baddr, ":")
			udpAddr := &net.UDPAddr{IP: net.ParseIP(host), Port: 33333}
			if *debug {
				log.Printf("[discovery] broadcast sent to %s", baddr)
			}
			conn.WriteToUDP(data, udpAddr)
			_ = portStr // порт всё равно фиксированный = 33333
		}
	}
}

// Получаем presence от других
func listenPresence(conn *net.UDPConn) {
	buf := make([]byte, 1024)
	localIPs := getLocalIPs()

	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var p Presence
		if err := json.Unmarshal(buf[:n], &p); err != nil {
			continue
		}

		// игнорируем самого себя по ID и IP
		if p.ID == selfID {
			for _, lip := range localIPs {
				if strings.HasPrefix(p.Addr, lip+":") {
					if *debug {
						log.Printf("[discovery] skipped self presence from %s", p.Addr)
					}
					goto skip
				}
			}
		}

		if _, ok := peers[p.ID]; !ok {
			peers[p.ID] = p
			log.Printf("discovered peer: %s @ %s", p.Name, p.Addr)
		}
	skip:
	}
}

func main() {
	flag.Parse()

	addr := net.UDPAddr{IP: net.IPv4zero, Port: 33333}
	udpConn, err := net.ListenUDP("udp4", &addr)
	if err != nil {
		log.Fatal(err)
	}
	defer udpConn.Close()

	log.Printf("tcp listener on :%d", *port)
	fmt.Printf("Started p2p-chat as %s (id=%s)\n", *name, selfID)
	fmt.Println("Commands: /peers  /exit")
	fmt.Println("Type message and Enter to send to all connected peers.")

	// discovery listener
	go listenPresence(udpConn)

	// discovery broadcaster
	go func() {
		for {
			broadcastPresence(udpConn)
			time.Sleep(3 * time.Second)
		}
	}()

	// graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	<-sigs
	log.Println("Shutting down...")
}
