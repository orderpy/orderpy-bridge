package local

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"

	"github.com/orderpy/orderpy-bridge-go/internal/config"
	"github.com/orderpy/orderpy-bridge-go/internal/spooler"
	"github.com/orderpy/orderpy-bridge-go/internal/state"
	"github.com/orderpy/orderpy-bridge-go/internal/version"
	"github.com/orderpy/orderpy-bridge-go/internal/wire"
)

var hostIPPattern = regexp.MustCompile(`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?::\d+)?$`)

// ServeHTTP runs the local API on addr until ctx is cancelled.
func ServeHTTP(ctx context.Context, addr string, sp *spooler.Spooler) error {
	mux := http.NewServeMux()
	allowed := map[string]struct{}{}
	for _, o := range config.AllowedOrigins() {
		allowed[o] = struct{}{}
	}

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"version":      version.Version,
			"variant":      config.Variant(),
			"update_status": state.GetUpdateStatus(),
		})
	})

	mux.HandleFunc("/setup/info", func(w http.ResponseWriter, r *http.Request) {
		if state.IsLocked() {
			http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		bid := state.GetBridgeID()
		if bid == "" {
			http.Error(w, "Bridge not yet registered with Cloud; wait a few seconds and retry", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"bridgeId":    bid,
			"claimToken":  state.GetClaimToken(),
			"pairing_ui":  true,
			"version":     version.Version,
			"variant":     config.Variant(),
		})
	})

	mux.HandleFunc("/print", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var pj wire.PrintJobFromCloud
		if err := json.NewDecoder(r.Body).Decode(&pj); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if pj.Action == "" {
			pj.Action = "print_job"
		}
		sp.Enqueue(spooler.Job{CloudMsg: pj, SendAck: nil})
		w.WriteHeader(http.StatusAccepted)
	})

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostIPPattern.MatchString(r.Host) {
			http.Error(w, "Forbidden: Direct IP access only", http.StatusForbidden)
			return
		}
		// CORS: echo allowed Origin (browser blocks otherwise; allowlist via ORDERPY_ALLOWED_ORIGINS).
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Add("Vary", "Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "600")
			} else {
				http.Error(w, "Forbidden: Origin not allowed", http.StatusForbidden)
				return
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})

	srv := &http.Server{Addr: addr, Handler: h}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	log.Printf("local: HTTP listening on %s", addr)
	return srv.ListenAndServe()
}
