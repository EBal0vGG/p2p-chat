package main

import (
    "context"
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/google/uuid"

    "github.com/example/p2p-chat/internal/discovery"
    "github.com/example/p2p-chat/internal/peer"
)

func main() {
    name := flag.String("name", "anon", "your display name")
    tcpPort := flag.Int("tcp-port", 45454, "local TCP port (0 for random)")
    debug := flag.Bool("debug", false, "enable debug logs")
    flag.Parse()

    if *debug {
        log.Printf("Starting with name=%s tcp-port=%d", *name, *tcpPort)
    }

    id := uuid.New().String()

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    pm := peer.NewManager(id, *name)

    // Start discovery (UDP)
    disc := discovery.New(33333, *tcpPort, id, *name, pm)
    go func() {
        if err := disc.Run(ctx); err != nil {
            log.Printf("discovery stopped: %v", err)
        }
    }()

    // Simple signal handling to exit
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

    fmt.Printf("Started p2p-chat as %s (id=%s)\n", *name, id)
    fmt.Println("Press Ctrl+C to exit")

    <-sigs
    fmt.Println("Shutting down...")
    cancel()
    time.Sleep(500 * time.Millisecond)
}