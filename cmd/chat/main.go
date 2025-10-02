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

// Отправляем broadcast presence
func broadcastPresence(conn *net.UDPConn) {
	localIPs := getLocalIPs()
	for _, lip := range localIPs {
		p := Presence{ID: selfID, Name: *name, Addr: fmt.Sprintf("%s:%d", lip, *port)}
		data, _ := json.Marshal(p)

		broadcastIPs := []string{"192.168.1.255:33333", "172.18.255.255:33333"}
		for _, baddr := range broadcastIPs {
			if *debug {
				log.Printf("[discovery] broadcast sent to %s", baddr)
			}
			conn.WriteToUDP(data, &net.UDPAddr{IP: net.ParseIP(strings.Split(baddr, ":")[0]), Port: 33333})
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
			skip := false
			for _, lip := range localIPs {
				if strings.HasPrefix(p.Addr, lip) {
					skip = true
					break
				}
			}
			if skip {
				if *debug {
					log.Printf("[discovery] skipped self presence from %s", p.Addr)
				}
				continue
			}
		}

		if _, ok := peers[p.ID]; !ok {
			peers[p.ID] = p
			log.Printf("discovered peer: %s @ %s", p.Name, p.Addr)
		}
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
