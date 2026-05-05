// Package wire defines bridge↔cloud protocol v3 (semantic print payloads).
package wire

const ProtocolVersion = "3"

// PrintJobFromCloud is sent as JSON with action "print_job".
type PrintJobFromCloud struct {
	Action         string         `json:"action"`
	PrintJobID     string         `json:"print_job_id"`
	Kind           string         `json:"kind"`
	PrinterID      string         `json:"printer_id"`
	PrinterAddress string         `json:"printer_address"`
	PrinterPort    int            `json:"printer_port"`
	TenantName     string         `json:"tenant_name"`
	LogoRef        *LogoRef       `json:"logo_ref,omitempty"`
	Receipt        map[string]any `json:"receipt"`
}

type LogoRef struct {
	Slot     int    `json:"slot"`
	Hash     string `json:"hash"`
	ImageB64 string `json:"image_b64,omitempty"`
}

// HandshakeOut is the first message from bridge to cloud.
type HandshakeOut struct {
	PubKey           string `json:"pubKey"`
	Version          string `json:"version"`
	ProtocolVersion  string `json:"protocol_version"`
	Variant          string `json:"variant"`
	Platform         string `json:"platform"`
	Unpair           bool   `json:"unpair,omitempty"`
}

// PrintAck is bridge → cloud.
type PrintAck struct {
	Action       string `json:"action"`
	PrintJobID   string `json:"print_job_id"`
	Status       string `json:"status"`
	ErrorClass   string `json:"error_class,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}
