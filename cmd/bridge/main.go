package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/orderpy/orderpy-bridge-go/internal/cloud"
	"github.com/orderpy/orderpy-bridge-go/internal/local"
	"github.com/orderpy/orderpy-bridge-go/internal/pairing"
	"github.com/orderpy/orderpy-bridge-go/internal/spooler"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sp := spooler.New(1024)
	go sp.Run(ctx)

	pairingCh := make(chan pairing.Request, 4)
	go pairing.ServeHTTPS(ctx, ":8088", pairingCh)

	go cloud.Run(ctx, sp, pairingCh)

	go func() {
		if err := local.ServeHTTP(ctx, ":8080", sp); err != nil && ctx.Err() == nil {
			log.Printf("local http: %v", err)
		}
	}()

	<-ctx.Done()
}
