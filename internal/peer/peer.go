package peer

import "time"

type Peer struct {
    ID       string
    Name     string
    Addr     string
    TCPPort  int
    LastSeen time.Time
}