package config

import (
	"os"
	"strings"
)

func CloudWSURL() string {
	raw := strings.TrimSpace(os.Getenv("ORDERPY_CLOUD_URL"))
	if raw == "" {
		raw = "http://localhost:8001"
	}
	raw = strings.TrimSuffix(raw, "/")
	if strings.HasPrefix(raw, "https://") {
		return "wss://" + strings.TrimPrefix(raw, "https://")
	}
	if strings.HasPrefix(raw, "http://") {
		return "ws://" + strings.TrimPrefix(raw, "http://")
	}
	return "ws://" + raw
}

func KeyPath() string {
	if p := strings.TrimSpace(os.Getenv("ORDERPY_KEY_PATH")); p != "" {
		return p
	}
	return "./data/bridge_key.pem"
}

func AllowedOrigins() []string {
	raw := os.Getenv("ORDERPY_ALLOWED_ORIGINS")
	if raw == "" {
		raw = "http://localhost:3000,http://127.0.0.1:3000,http://localhost:5173,http://127.0.0.1:5173"
	}
	var out []string
	for _, o := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(o); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func Variant() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ORDERPY_BRIDGE_VARIANT")))
	switch v {
	case "docker", "daemon", "android":
		return v
	default:
		return "unknown"
	}
}

func WSMaxInbound() int {
	// Default 16 MiB (same as Python bridge).
	const def = 16 * 1024 * 1024
	raw := strings.TrimSpace(os.Getenv("ORDERPY_BRIDGE_WS_MAX_INBOUND"))
	if raw == "" {
		return def
	}
	var n int
	for _, c := range raw {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n < 1<<20 {
		return def
	}
	return n
}

func PrinterHealthInterval() int {
	// seconds, default 10
	const def = 10
	// reuse env name from Python
	raw := strings.TrimSpace(os.Getenv("ORDERPY_PRINTER_HEALTH_INTERVAL"))
	if raw == "" {
		return def
	}
	var n int
	for _, c := range raw {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n < 5 {
		return 5
	}
	return n
}
