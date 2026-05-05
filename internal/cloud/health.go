package cloud

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/orderpy/orderpy-bridge-go/internal/printer"
)

func runPrinterHealth(ctx context.Context, wc *writeConn, mu *sync.Mutex, printers *[]printerCfg, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			mu.Lock()
			list := append([]printerCfg(nil), (*printers)...)
			mu.Unlock()
			if len(list) == 0 {
				continue
			}
			var statuses []map[string]any
			for _, p := range list {
				if p.ID == "" || p.Address == "" {
					continue
				}
				port := p.Port
				if port < 1 {
					port = 9100
				}
				ok := printer.Reachable(ctx, p.Address, port)
				statuses = append(statuses, map[string]any{"printer_id": p.ID, "reachable": ok})
			}
			if len(statuses) == 0 {
				continue
			}
			b, _ := json.Marshal(map[string]any{"action": "printer_status", "statuses": statuses})
			_ = wc.WriteMessage(websocket.TextMessage, b)
		}
	}
}
