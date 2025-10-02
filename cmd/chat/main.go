package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

// Presence — объявление, которое мы шлём и принимаем по UDP.
type Presence struct {
	Name string `json:"name"`
	Addr string `json:"addr"` // "ip:port"
}

// StartDiscovery запускает discovery: слушает UDP 33333 и периодически рассылает presence.
// Блокирует до ctx.Done() и возвращает ctx.Err() (или ошибку при инициализации).
// foundPeer вызывается для каждого принятого чужого presence.
func StartDiscovery(ctx context.Context, name string, tcpPort int, foundPeer func(Presence)) error {
	// UDP listener для входящих presence
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 33333})
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	defer listener.Close()

	// UDP socket для отправки (ephemeral local port)
	sender, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return fmt.Errorf("open udp sender: %w", err)
	}
	defer sender.Close()

	// Логируем найденные интерфейсы и IP-адреса (полезно в debug)
	allLocal := getLocalIPs()
	if len(allLocal) > 0 {
		log.Printf("[discovery] local ips: %v", allLocal)
	} else {
		log.Printf("[discovery] no local ips found (fallback)")
	}

	// Выбираем предпочтительный IP для объявления (192.168*, 10*, 172.16-31*)
	localIP := chooseLocalIP(allLocal)
	if localIP == "" {
		// fallback: если ничего подходящего не найдено — берём первый
		if len(allLocal) > 0 {
			localIP = allLocal[0]
		} else {
			localIP = "127.0.0.1"
		}
	}
	log.Printf("[discovery] selected local ip=%s", localIP)

	// Формируем presence (используем выбранный локальный IP)
	pres := Presence{
		Name: name,
		Addr: net.JoinHostPort(localIP, strconv.Itoa(tcpPort)),
	}
	data, _ := json.Marshal(pres)

	// Запускаем горутину для чтения входящих пакетов
	go listenLoop(ctx, listener, foundPeer)

	// Запускаем цикл рассылающий presence
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// каждый раз пересчитываем broadcast-адреса (на случай изменений)
			bcasts, err := getBroadcastAddrs()
			if err != nil {
				log.Printf("[discovery] failed to get broadcast addrs: %v", err)
				continue
			}
			seen := make(map[string]bool)
			for _, b := range bcasts {
				key := b.IP.String() + ":" + strconv.Itoa(b.Port)
				if seen[key] {
					continue
				}
				seen[key] = true

				// Отправляем на каждый broadcast-адрес
				if _, err := sender.WriteToUDP(data, b); err != nil {
					log.Printf("[discovery] broadcast to %s failed: %v", b.String(), err)
				} else {
					log.Printf("[discovery] broadcast sent to %s", b.String())
				}
			}
		}
	}
}

// listenLoop — читает UDP-пакеты и вызывает foundPeer для чужих объявлений.
// Игнорирует пакеты, пришедшие от любого из локальных IP адресов (самообъявления).
func listenLoop(ctx context.Context, conn *net.UDPConn, foundPeer func(Presence)) {
	buf := make([]byte, 4096)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			// таймауты проверяем ctx, прочие ошибки логируем и продолжаем
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			// другие ошибки
			log.Printf("[discovery] read error: %v", err)
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		var p Presence
		if err := json.Unmarshal(buf[:n], &p); err != nil {
			log.Printf("[discovery] bad presence json from %s: %v", addr.String(), err)
			continue
		}

		// parse host from p.Addr (expected ip:port)
		host, _, err := net.SplitHostPort(p.Addr)
		if err == nil {
			// get latest local IPs at time of receipt (interfaces may change)
			localIPs := getLocalIPs()
			if ipIsLocal(host, localIPs) {
				log.Printf("[discovery] skipped self presence from %s (name=%s)", p.Addr, p.Name)
				continue
			}
		} else {
			// если не получилось распарсить p.Addr — дополнительно проверим raw sender addr
			// это защитит от случаев, когда sender использует другой формát
			if ipIsLocal(addr.IP.String(), getLocalIPs()) {
				log.Printf("[discovery] skipped self presence from remote %s (name=%s)", addr.String(), p.Name)
				continue
			}
		}

		// Наконец — это чужой peer
		log.Printf("[discovery] received presence from %s (%s)", p.Name, p.Addr)
		foundPeer(p)
	}
}

// getBroadcastAddrs собирает список broadcast адресов для всех активных интерфейсов.
// Пропускает loopback, link-local (169.254.*), а также явно виртуальные интерфейсы (docker, veth, br-).
func getBroadcastAddrs() ([]*net.UDPAddr, error) {
	var out []*net.UDPAddr
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		// интерфейс должен быть поднят и поддерживать broadcast
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		// игнорируем явно виртуальные/мостовые интерфейсы по имени
		name := strings.ToLower(iface.Name)
		if strings.HasPrefix(name, "docker") ||
			strings.HasPrefix(name, "veth") ||
			strings.HasPrefix(name, "br-") ||
			strings.HasPrefix(name, "vmnet") ||
			strings.HasPrefix(name, "vbox") {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil {
				continue
			}

			// пропускаем link-local addresses
			if ip[0] == 169 && ip[1] == 254 {
				continue
			}
			// пропускаем docker/wsl типичные подсети (если явное нежелание)
			if ip[0] == 172 && (ip[1] == 17 || ip[1] == 18) {
				continue
			}

			mask := net.IP(ipnet.Mask).To4()
			if mask == nil {
				continue
			}
			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip[i] | ^mask[i]
			}
			out = append(out, &net.UDPAddr{IP: bcast, Port: 33333})
		}
	}

	// fallback: если ничего не найдено — добавить глобальный broadcast (иногда полезно)
	if len(out) == 0 {
		out = append(out, &net.UDPAddr{IP: net.IPv4bcast, Port: 33333})
	}
	return out, nil
}

// getLocalIPs возвращает список всех IPv4 адресов текущего хоста (не loopback).
func getLocalIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.To4()
			ips = append(ips, ip.String())
		}
	}
	return ips
}

// ipIsLocal проверяет, находится ли ip в списке локальных IP
func ipIsLocal(ip string, local []string) bool {
	for _, l := range local {
		if ip == l {
			return true
		}
	}
	return false
}

// chooseLocalIP выбирает предпочтительный локальный IP из списка:
// сначала 192.168.*, затем 10.*, затем 172.16-31.*, иначе первый.
func chooseLocalIP(all []string) string {
	for _, ipS := range all {
		ip := net.ParseIP(ipS).To4()
		if ip == nil {
			continue
		}
		if ip[0] == 192 && ip[1] == 168 {
			return ipS
		}
	}
	for _, ipS := range all {
		ip := net.ParseIP(ipS).To4()
		if ip == nil {
			continue
		}
		if ip[0] == 10 {
			return ipS
		}
	}
	for _, ipS := range all {
		ip := net.ParseIP(ipS).To4()
		if ip == nil {
			continue
		}
		if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
			return ipS
		}
	}
	if len(all) > 0 {
		return all[0]
	}
	return ""
}
