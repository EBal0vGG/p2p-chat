package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/example/p2p-chat/internal/messages"
	"github.com/example/p2p-chat/internal/peer"
)

// StartListener — запускает TCP-listener на requestedPort (0 = random).
// Возвращает фактический порт и ошибку (если не удалось слушать).
// Принимаемые подключения обрабатываются в handleConn и сообщения идут в incoming.
func StartListener(ctx context.Context, pm *peer.Manager, requestedPort int, selfID, selfName string, incoming chan<- messages.Message) (int, error) {
	addr := fmt.Sprintf(":%d", requestedPort)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, err
	}
	tcpAddr := l.Addr().(*net.TCPAddr)
	actualPort := tcpAddr.Port
	log.Printf("tcp listener on :%d", actualPort)

	go func() {
		defer l.Close()
		for {
			conn, err := l.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("tcp accept error: %v", err)
					continue
				}
			}
			go handleConn(conn, pm, selfID, selfName, incoming)
		}
	}()
	return actualPort, nil
}

// ConnectToPeer — пытается установить исходящее TCP-соединение к peer (по IP:port).
// Если успех — вызывает handleConn (т.е. обмен hello + чтение сообщений).
func ConnectToPeer(ctx context.Context, pm *peer.Manager, p peer.Peer, selfID, selfName string, incoming chan<- messages.Message) {
	// если уже есть conn — ничего не делаем
	if pm.HasConn(p.ID) {
		return
	}
	addr := net.JoinHostPort(p.Addr, strconv.Itoa(p.TCPPort))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		// best-effort: если не достучались — пропускаем
		log.Printf("dial %s failed: %v", addr, err)
		return
	}
	go handleConn(conn, pm, selfID, selfName, incoming)
}

// handleConn — обмен hello (обе стороны посылают hello сразу) и чтение последующих сообщений.
// Все полученные сообщения (msg type == "msg") отправляются в incoming канал.
func handleConn(conn net.Conn, pm *peer.Manager, selfID, selfName string, incoming chan<- messages.Message) {
	defer func() {
		_ = conn.Close()
	}()

	// 1) отправим наш hello сразу
	hello := messages.NewHello(selfID, selfName)
	if err := sendJSONLine(conn, hello); err != nil {
		log.Printf("send hello failed: %v", err)
		return
	}

	// 2) начнём читать строки (newline-delimited JSON)
	scanner := bufio.NewScanner(conn)
	// увеличить буфер на случай длинных сообщений
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var remotePeerID string
	for scanner.Scan() {
		line := scanner.Text()
		var msg messages.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Printf("bad json from %s: %v", conn.RemoteAddr().String(), err)
			continue
		}

		switch msg.Type {
		case "hello":
			// remote представился
			remotePeerID = msg.From
			// remote address/port можно взять из conn.RemoteAddr()
			hostPort := conn.RemoteAddr().String()
			host, portStr, _ := net.SplitHostPort(hostPort)
			portInt := 0
			if portStr != "" {
				if p, err := strconv.Atoi(portStr); err == nil {
					portInt = p
				}
			}
			pm.AddOrUpdate(peer.Peer{
				ID:       remotePeerID,
				Name:     msg.Name,
				Addr:     host,
				TCPPort:  portInt,
				LastSeen: time.Now(),
			})
			pm.SetConn(remotePeerID, conn)
			log.Printf("tcp connected: %s (%s) @ %s", msg.Name, remotePeerID, hostPort)

		case "msg":
			// forward chat message to main via channel
			incoming <- msg

		// можно добавить другие типы (goodbye, peers_response и т.п.)
		default:
			// игнорируем неизвестные типы в MVP
		}
	}

	// on exit (remote closed / error)
	if err := scanner.Err(); err != nil {
		// log.Printf("connection read error: %v", err)
		_ = err // ignore detailed logging here
	}

	if remotePeerID != "" {
		pm.RemoveConn(remotePeerID)
	}
}

// sendJSONLine — marshal + append newline + write
func sendJSONLine(conn net.Conn, msg messages.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
}
