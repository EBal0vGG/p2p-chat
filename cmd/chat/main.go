package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/example/p2p-chat/internal/discovery"
	"github.com/example/p2p-chat/internal/messages"
	"github.com/example/p2p-chat/internal/peer"
	"github.com/example/p2p-chat/internal/transport"
)

func main() {
	name := flag.String("name", "anon", "your display name")
	tcpPort := flag.Int("tcp-port", 45454, "local TCP port (0 for random)")
	debug := flag.Bool("debug", false, "enable debug logs")
	flag.Parse()

	if *debug {
		log.Printf("Starting with name=%s tcp-port=%d", *name, *tcpPort)
	}

	id := uuid.NewString()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pm := peer.NewManager(id, *name)
	incoming := make(chan messages.Message, 32)

	// 1) Start TCP listener first so we know real port to announce
	actualTCP, err := transport.StartListener(ctx, pm, *tcpPort, id, *name, incoming)
	if err != nil {
		log.Fatalf("start tcp listener failed: %v", err)
	}

	// 2) Start discovery (uses StartDiscovery)
	go func() {
		err := discovery.StartDiscovery(ctx, *name, actualTCP, func(p discovery.Presence) {
			host, portStr, err := net.SplitHostPort(p.Addr)
			if err != nil {
				return
			}
			port, _ := strconv.Atoi(portStr)

			// --- фильтр: игнорируем себя ---
			if p.Name == *name && host == discovery.GetLocalIP() {
				if *debug {
					log.Printf("[discovery] skipped self presence from %s", p.Addr)
				}
				return
			}
			// -------------------------------

			peerID := p.Addr
			pm.AddOrUpdate(peer.Peer{
				ID:       peerID,
				Name:     p.Name,
				Addr:     host,
				TCPPort:  port,
				LastSeen: time.Now(),
			})
			log.Printf("discovered peer: %s @ %s:%d", p.Name, host, port)
		})
		if err != nil && err != context.Canceled {
			log.Printf("discovery stopped: %v", err)
		}
	}()

	// 3) Periodically try to connect to known peers (if not connected)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				peers := pm.List()
				for _, p := range peers {
					if p.ID == id {
						continue
					}
					if pm.HasConn(p.ID) {
						continue
					}
					// try connect (best-effort)
					go transport.ConnectToPeer(ctx, pm, p, id, *name, incoming)
				}
			}
		}
	}()

	// 4) Print incoming messages
	go func() {
		for m := range incoming {
			if m.Type == "msg" {
				fmt.Printf("[%s] %s\n", m.Name, m.Text)
			}
		}
	}()

	// 5) Read stdin — отправка сообщений / команды
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("Started p2p-chat as %s (id=%s)\n", *name, id)
	fmt.Println("Commands: /peers  /exit")
	fmt.Println("Type message and Enter to send to all connected peers.")
loop:
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "/") {
			switch strings.TrimSpace(line) {
			case "/peers":
				fmt.Println(pm.String())
			case "/exit":
				fmt.Println("exiting...")
				cancel()
				break loop
			default:
				fmt.Println("unknown command:", line)
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		msg := messages.NewChat(line, id, *name)
		fmt.Printf("[you] %s\n", line)
		pm.Broadcast(msg)
	}

	pm.CloseAll()
	time.Sleep(200 * time.Millisecond)
}
