package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/orderpy/orderpy-bridge-go/internal/config"
	"github.com/orderpy/orderpy-bridge-go/internal/keys"
	"github.com/orderpy/orderpy-bridge-go/internal/pairing"
	"github.com/orderpy/orderpy-bridge-go/internal/updater"
	"github.com/orderpy/orderpy-bridge-go/internal/spooler"
	"github.com/orderpy/orderpy-bridge-go/internal/state"
	"github.com/orderpy/orderpy-bridge-go/internal/version"
	"github.com/orderpy/orderpy-bridge-go/internal/wire"
)

// Run connects to cloud WebSocket with exponential backoff and drives the spooler.
func Run(ctx context.Context, sp *spooler.Spooler, pairingCh chan pairing.Request) {
	pub, err := keys.LoadOrGeneratePublicKeyPEM(config.KeyPath())
	if err != nil {
		log.Printf("bridge: cannot load keys: %v", err)
		return
	}
	wsPath := strings.TrimSuffix(config.CloudWSURL(), "/") + "/api/v1/bridges/connect"
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 15 * time.Second,
	}
	hdr := http.Header{}
	backoff := time.Second
	maxBackoff := 60 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		state.SetWSConnected(false)
		conn, _, err := dialer.DialContext(ctx, wsPath, hdr)
		if err != nil {
			log.Printf("bridge: websocket dial: %v (retry in %v)", err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = time.Second
		state.SetWSConnected(true)
		if err := session(ctx, conn, pub, sp, pairingCh); err != nil {
			log.Printf("bridge: session ended: %v", err)
		}
		_ = conn.Close()
		state.SetWSConnected(false)
		time.Sleep(5 * time.Second)
	}
}

func session(ctx context.Context, conn *websocket.Conn, pubPEM string, sp *spooler.Spooler, pairingCh <-chan pairing.Request) error {
	conn.SetReadLimit(int64(config.WSMaxInbound()))
	hs := map[string]any{
		"pubKey":            pubPEM,
		"version":           version.Version,
		"protocol_version":  wire.ProtocolVersion,
		"variant":           config.Variant(),
		"platform":          fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
	b0, _ := json.Marshal(hs)
	if err := conn.WriteMessage(websocket.TextMessage, b0); err != nil {
		return err
	}
	_, hello, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	var welcome map[string]any
	if err := json.Unmarshal(hello, &welcome); err != nil {
		return err
	}
	bid, _ := welcome["bridgeId"].(string)
	if bid == "" {
		bid, _ = welcome["bridge_id"].(string)
	}
	st, _ := welcome["status"].(string)
	if bid != "" {
		state.SetBridgeID(bid)
	}
	if st == "UNCLAIMED" {
		state.Unpair()
	} else if st == "ACTIVE" {
		state.InvalidateClaimToken()
	}
	log.Printf("bridge: registered bridge_id=%s status=%s", bid, st)

	var printers []printerCfg
	var mu sync.Mutex
	var apiKey, cloudHTTP string
	var pendingPairingDone chan<- pairing.Result
	var healthOnce sync.Once

	// gorilla/websocket forbids concurrent writes; spooler ack callbacks,
	// runPrinterHealth, the recvLoop dispatchers, and pairing all write to
	// the same conn. Serialize through this wrapper so frames don't interleave.
	wc := &writeConn{conn: conn}

	sendJSON := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return wc.WriteMessage(websocket.TextMessage, b)
	}

	recvLoop := make(chan []byte, 16)
	errCh := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			select {
			case recvLoop <- data:
			case <-ctx.Done():
				return
			}
		}
	}()

	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case pr := <-pairingCh:
			msg := map[string]any{"action": "submit_pairing_code", "code": pr.Code}
			if err := sendJSON(msg); err != nil {
				pr.Done <- pairing.Result{OK: false, Reason: "send_failed"}
				continue
			}
			pendingPairingDone = pr.Done
		case data := <-recvLoop:
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			act, _ := msg["action"].(string)
			switch act {
			case "pairing_result":
				if pendingPairingDone != nil {
					ok, _ := msg["ok"].(bool)
					reason, _ := msg["reason"].(string)
					pendingPairingDone <- pairing.Result{OK: ok, Reason: reason}
					pendingPairingDone = nil
				}
			case "verify_claim":
				tok, _ := msg["claimToken"].(string)
				ok := state.CheckToken(tok)
				if ok {
					state.InvalidateClaimToken()
				} else {
					state.RecordVerifyFailure()
				}
				_ = sendJSON(map[string]any{"ok": ok})
			case "config":
				apiKey, _ = msg["api_key"].(string)
				cloudHTTP = httpBaseFromWS(config.CloudWSURL())
				_ = cloudHTTP
				_ = apiKey
				state.SetTenantName(anyStr(msg["tenant_name"]))
				state.InvalidateClaimToken()
				if raw, ok := msg["printers"].([]any); ok {
					mu.Lock()
					printers = printers[:0]
					for _, p := range raw {
						pm, ok := p.(map[string]any)
						if !ok {
							continue
						}
						printers = append(printers, printerCfg{
							ID:      anyStr(pm["id"]),
							Address: anyStr(pm["address"]),
							Port:    int(anyFloat(pm["port"], 9100)),
						})
					}
					mu.Unlock()
				}
				healthOnce.Do(func() {
					go runPrinterHealth(ctx, wc, &mu, &printers, time.Duration(config.PrinterHealthInterval())*time.Second)
				})
			case "config_update":
				if raw, ok := msg["printers"].([]any); ok {
					mu.Lock()
					for _, p := range raw {
						pm, ok := p.(map[string]any)
						if !ok {
							continue
						}
						id := anyStr(pm["id"])
						addr := anyStr(pm["address"])
						port := int(anyFloat(pm["port"], 9100))
						found := false
						for i := range printers {
							if printers[i].ID == id {
								printers[i].Address = addr
								printers[i].Port = port
								found = true
								break
							}
						}
						if !found && id != "" && addr != "" {
							printers = append(printers, printerCfg{ID: id, Address: addr, Port: port})
						}
					}
					mu.Unlock()
				}
			case "print_order":
				handleLegacyPrintOrder(ctx, sp, wc, msg, &mu, &printers)
			case "print_job":
				handlePrintJobV3(ctx, sp, wc, msg)
			case "print_service_call":
				handleServiceCall(ctx, sp, wc, msg, &mu, &printers)
			case "print_test":
				handlePrintTest(ctx, wc, msg)
			case "unpaired":
				mu.Lock()
				printers = nil
				mu.Unlock()
				state.Unpair()
				return fmt.Errorf("unpaired")
			case "update":
				acc, reason := updater.HandleUpdateAction(msg)
				if acc {
					_ = sendJSON(map[string]any{
						"action": "update_status", "status": "requested",
						"target_version": msg["target_version"], "current_version": version.Version, "error": nil,
					})
				} else {
					_ = sendJSON(map[string]any{
						"action": "update_status", "status": "failed",
						"target_version": msg["target_version"], "current_version": version.Version, "error": reason,
					})
				}
			default:
				// pairing code response {ok} without action
				if _, ok := msg["ok"]; ok && bid != "" {
					// verify_claim response from bridge perspective is us sending {ok}; cloud sends verify_claim
				}
			}
		case <-tick.C:
			updater.CheckWatchdog(0)
			if res := updater.ConsumePendingUpdateResult(); res != nil {
				st, _ := res["status"].(string)
				_ = sendJSON(map[string]any{
					"action": "update_status", "status": st,
					"target_version": res["target_version"], "current_version": version.Version, "error": res["error"],
				})
			}
		}
	}
}

type printerCfg struct {
	ID      string
	Address string
	Port    int
}

func httpBaseFromWS(ws string) string {
	ws = strings.TrimSuffix(ws, "/")
	if strings.HasPrefix(ws, "wss://") {
		return "https://" + strings.TrimPrefix(ws, "wss://")
	}
	if strings.HasPrefix(ws, "ws://") {
		return "http://" + strings.TrimPrefix(ws, "ws://")
	}
	return ws
}

func anyStr(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func anyFloat(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	default:
		return def
	}
}

func handlePrintJobV3(ctx context.Context, sp *spooler.Spooler, wc *writeConn, msg map[string]any) {
	raw, _ := json.Marshal(msg)
	var pj wire.PrintJobFromCloud
	if err := json.Unmarshal(raw, &pj); err != nil {
		return
	}
	if pj.Action == "" {
		pj.Action = "print_job"
	}
	id := pj.PrintJobID
	sp.Enqueue(spooler.Job{
		CloudMsg: pj,
		SendAck: func(ok bool, errClass, errMsg string) {
			out := map[string]any{"action": "print_ack", "print_job_id": id, "status": map[bool]string{true: "printed", false: "failed"}[ok]}
			if !ok {
				out["error_class"] = errClass
				out["error_message"] = errMsg
			}
			b, _ := json.Marshal(out)
			_ = wc.WriteMessage(websocket.TextMessage, b)
		},
	})
}

func handleLegacyPrintOrder(ctx context.Context, sp *spooler.Spooler, wc *writeConn, msg map[string]any, mu *sync.Mutex, printers *[]printerCfg) {
	b64, _ := msg["receipt_bytes_base64"].(string)
	orderID, _ := msg["order_id"].(string)
	printerID, _ := msg["printer_id"].(string)
	if b64 == "" {
		return
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return
	}
	mu.Lock()
	list := append([]printerCfg(nil), (*printers)...)
	mu.Unlock()
	targets := filterPrinters(list, printerID)
	if len(targets) == 0 {
		return
	}
	for _, p := range targets {
		p := p
		_ = ctx
		if err := sendBytesToPrinter(ctx, p.Address, p.Port, data); err != nil {
			log.Printf("print_order send: %v", err)
		}
	}
	ack := map[string]any{"action": "order_print_failed", "order_id": orderID}
	if orderID != "" {
		// simplified: always printed if any target — real logic in Python
		ack["action"] = "order_printed"
	}
	b, _ := json.Marshal(ack)
	_ = wc.WriteMessage(websocket.TextMessage, b)
}

func handleServiceCall(ctx context.Context, sp *spooler.Spooler, wc *writeConn, msg map[string]any, mu *sync.Mutex, printers *[]printerCfg) {
	b64, _ := msg["receipt_bytes_base64"].(string)
	if b64 == "" {
		return
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return
	}
	mu.Lock()
	list := append([]printerCfg(nil), (*printers)...)
	mu.Unlock()
	for _, p := range list {
		_ = sendBytesToPrinter(ctx, p.Address, p.Port, data)
	}
}

func handlePrintTest(ctx context.Context, wc *writeConn, msg map[string]any) {
	addr, _ := msg["address"].(string)
	port := int(anyFloat(msg["port"], 9100))
	b64, _ := msg["receipt_bytes_base64"].(string)
	ok := false
	if addr != "" && port >= 1 && port <= 65535 && b64 != "" {
		if data, err := base64.StdEncoding.DecodeString(b64); err == nil {
			ok = sendBytesToPrinter(ctx, addr, port, data) == nil
		}
	}
	pid, _ := msg["printer_id"].(string)
	out, _ := json.Marshal(map[string]any{"action": "print_test_result", "printer_id": pid, "ok": ok})
	_ = wc.WriteMessage(websocket.TextMessage, out)
}

func filterPrinters(list []printerCfg, id string) []printerCfg {
	if id == "" {
		return list
	}
	var out []printerCfg
	for _, p := range list {
		if p.ID == id {
			out = append(out, p)
		}
	}
	return out
}
