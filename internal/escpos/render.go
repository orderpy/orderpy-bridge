package escpos

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/orderpy/orderpy-bridge-go/internal/wire"
	"golang.org/x/text/encoding/charmap"
)

// Constants aligned with Python receipt_espos.py
const receiptWidth = 48
const colAnz = 4
const colProdukt = 17
const colZus = 12
const colPreis = 12
const feedBeforeCut = 6

var escSelectCodepagePC858 = []byte{0x1B, 0x74, 19}
var escFeedNLines = []byte{0x1B, 0x64, feedBeforeCut}

// Render builds ESC/POS bytes for a cloud print_job v3 message.
func Render(msg *wire.PrintJobFromCloud) ([]byte, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	switch msg.Kind {
	case "test":
		return buildTestPrint(), nil
	case "service_call":
		return buildServiceCall(msg.Receipt)
	case "order", "kitchen":
		tn := msg.TenantName
		if msg.Receipt != nil {
			if v, ok := msg.Receipt["tenant_name"].(string); ok && strings.TrimSpace(v) != "" {
				tn = v
			}
		}
		return buildOrderLike(msg.Kind, tn, msg.Receipt)
	case "pos_receipt":
		return buildPosReceipt(msg.Receipt)
	case "logo_provision":
		return buildLogoProvision(msg.Receipt)
	default:
		return nil, fmt.Errorf("unknown print kind %q", msg.Kind)
	}
}

func encode858(s string) []byte {
	enc := charmap.CodePage858.NewEncoder()
	out, err := enc.Bytes([]byte(s))
	if err != nil {
		enc2 := charmap.CodePage858.NewEncoder()
		out, _ = enc2.Bytes([]byte(strings.ToValidUTF8(s, "?")))
	}
	return out
}

func line(s string) []byte {
	return append(encode858(s), '\n')
}

func buildTestPrint() []byte {
	init := []byte{0x1B, 0x40}
	boldOn := []byte{0x1B, 0x45, 0x01}
	boldOff := []byte{0x1B, 0x45, 0x00}
	center := []byte{0x1B, 0x61, 0x01}
	left := []byte{0x1B, 0x61, 0x00}
	lf := []byte{'\n'}
	sep := append(bytesRepeat('-', 32), lf...)
	cut := []byte{0x1D, 0x56, 0x00}
	now := time.Now().Format("02.01.2006 15:04") + " Uhr"
	var b []byte
	b = append(b, init...)
	b = append(b, escSelectCodepagePC858...)
	b = append(b, center...)
	b = append(b, boldOn...)
	b = append(b, line("OrderPy Testdruck")...)
	b = append(b, boldOff...)
	b = append(b, line("Testseite")...)
	b = append(b, left...)
	b = append(b, lf...)
	b = append(b, sep...)
	b = append(b, line("Datum/Zeit: "+now)...)
	b = append(b, line("Drucker-Status: OK")...)
	b = append(b, line("Verbindung erfolgreich.")...)
	b = append(b, lf...)
	b = append(b, sep...)
	b = append(b, escFeedNLines...)
	b = append(b, cut...)
	return b
}

func buildServiceCall(rec map[string]any) ([]byte, error) {
	tableNum := anyString(rec["table_number"], "?")
	reason := anyString(rec["reason"], "other")
	reasonLabel := "Sonstiges"
	if reason == "payment" {
		reasonLabel = "Bezahlung"
	}
	init := []byte{0x1B, 0x40}
	boldOn := []byte{0x1B, 0x45, 0x01}
	boldOff := []byte{0x1B, 0x45, 0x00}
	center := []byte{0x1B, 0x61, 0x01}
	left := []byte{0x1B, 0x61, 0x00}
	doubleHW := []byte{0x1D, 0x21, 0x11}
	normal := []byte{0x1D, 0x21, 0x00}
	cut := []byte{0x1D, 0x56, 0x00}
	now := time.Now().Format("02.01.2006 15:04") + " Uhr"
	var b []byte
	b = append(b, init...)
	b = append(b, escSelectCodepagePC858...)
	b = append(b, center...)
	b = append(b, line(strings.Repeat("=", receiptWidth))...)
	b = append(b, doubleHW...)
	b = append(b, line("KELLNER RUFEN")...)
	b = append(b, normal...)
	b = append(b, line(strings.Repeat("=", receiptWidth))...)
	b = append(b, left...)
	b = append(b, line("")...)
	b = append(b, boldOn...)
	b = append(b, line(fmt.Sprintf("Tisch: %v", tableNum))...)
	b = append(b, line("Grund: "+reasonLabel)...)
	b = append(b, boldOff...)
	b = append(b, line("")...)
	b = append(b, line(now)...)
	b = append(b, line("")...)
	b = append(b, line(strings.Repeat("-", receiptWidth))...)
	b = append(b, escFeedNLines...)
	b = append(b, cut...)
	return b, nil
}

func buildOrderLike(kind, tenantName string, rec map[string]any) ([]byte, error) {
	if rec == nil {
		return nil, errors.New("empty receipt")
	}
	// Reuse guest order layout (Python build_order_receipt_bytes).
	cafe := strings.TrimSpace(tenantName)
	if cafe == "" {
		cafe = "BESTELLUNG"
	}
	tableNum := rec["table_number"]
	tableLoc, _ := rec["table_location"].(string)
	createdAt := anyString(rec["created_at"], "")
	items, _ := rec["items"].([]any)
	takeaway, _ := rec["takeaway"].(bool)

	init := []byte{0x1B, 0x40}
	boldOn := []byte{0x1B, 0x45, 0x01}
	boldOff := []byte{0x1B, 0x45, 0x00}
	center := []byte{0x1B, 0x61, 0x01}
	left := []byte{0x1B, 0x61, 0x00}
	doubleH := []byte{0x1D, 0x21, 0x01}
	doubleHW := []byte{0x1D, 0x21, 0x11}
	normal := []byte{0x1D, 0x21, 0x00}
	doubleW := []byte{0x1D, 0x21, 0x10}
	cut := []byte{0x1D, 0x56, 0x00}

	var b []byte
	b = append(b, init...)
	b = append(b, escSelectCodepagePC858...)
	b = append(b, center...)
	b = append(b, line(strings.Repeat("=", receiptWidth))...)
	b = append(b, doubleHW...)
	title := cafe
	if len(title) > receiptWidth/2 {
		title = title[:receiptWidth/2]
	}
	b = append(b, line(title)...)
	b = append(b, normal...)
	b = append(b, line(strings.Repeat("=", receiptWidth))...)
	b = append(b, left...)

	ts := formatCreated(createdAt)
	b = append(b, line(ts+"\n")...)

	orderID := anyString(rec["order_id"], "")
	short := "#?"
	if orderID != "" {
		s := strings.ReplaceAll(orderID, "-", "")
		if len(s) > 8 {
			s = s[:8]
		}
		short = "#" + strings.ToUpper(s)
	}
	b = append(b, line(short+"\n")...)

	var tableLine string
	if takeaway {
		tableLine = "Außer Haus\n"
	} else {
		tableLine = fmt.Sprintf("Tisch: #%v", tableNum)
		if tableLoc == "indoor" {
			tableLine += " (Drinnen)"
		} else if tableLoc == "outdoor" {
			tableLine += " (Draußen)"
		}
		tableLine += "\n"
	}
	if kind == "kitchen" {
		corr := anyString(rec["correlation_id"], "")
		if corr != "" {
			tableLine = "Küche " + corr + "\n" + tableLine
		}
	}
	b = append(b, doubleH...)
	b = append(b, line(tableLine)...)
	b = append(b, normal...)
	b = append(b, line(strings.Repeat("-", receiptWidth))...)
	b = append(b, boldOn...)
	header := cell("Anz", colAnz, false) + " " + cell("Produkt", colProdukt, true) + " " +
		cell("Zusätze", colZus, true) + " " + cell("Preis", colPreis, false)
	b = append(b, line(header)...)
	b = append(b, boldOff...)
	b = append(b, line(strings.Repeat("-", receiptWidth))...)

	total := 0.0
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		name := anyString(m["product_name"], "?")
		qty := int(anyFloat(m["quantity"], 1))
		unit := anyFloat(m["unit_price"], 0)
		itemTotal := float64(qty) * unit
		total += itemTotal
		extras, _ := m["extras"].([]any)
		appendItemBlock(&b, name, qty, fmt.Sprintf("%.2f €", itemTotal), extras, boldOn, boldOff)
	}
	b = append(b, doubleW...)
	b = append(b, line(fmt.Sprintf("TOTAL: %.2f €", total))...)
	b = append(b, normal...)
	footer := anyString(rec["receipt_footer"], "")
	if footer != "" {
		b = append(b, line("\n"+footer)...)
	}
	b = append(b, escFeedNLines...)
	b = append(b, cut...)
	return b, nil
}

func buildPosReceipt(rec map[string]any) ([]byte, error) {
	if raw, ok := rec["escpos_base64"].(string); ok && raw != "" {
		return base64.StdEncoding.DecodeString(raw)
	}
	// If semantic POS fields present, fall back to order-like body (simplified).
	return buildOrderLike("pos_receipt", anyString(rec["tenant_name"], ""), rec)
}

func buildLogoProvision(rec map[string]any) ([]byte, error) {
	raw := anyString(rec["image_b64"], "")
	if raw == "" {
		raw = anyString(rec["prelude_b64"], "")
	}
	if raw == "" {
		return nil, errors.New("logo_provision: missing image_b64/prelude_b64")
	}
	return base64.StdEncoding.DecodeString(raw)
}

func formatCreated(iso string) string {
	if iso == "" {
		return time.Now().Format("2006-01-02 15:04:05")
	}
	t, err := time.Parse(time.RFC3339, strings.ReplaceAll(iso, "Z", "+00:00"))
	if err != nil {
		if len(iso) >= 19 {
			return iso[:19]
		}
		return iso
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func cell(s string, width int, leftAlign bool) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > width {
		s = s[:width]
	}
	if leftAlign {
		return fmt.Sprintf("%-*s", width, s)
	}
	return fmt.Sprintf("%*s", width, s)
}

func appendItemBlock(b *[]byte, name string, qty int, priceDisplay string, extras []any, boldOn, boldOff []byte) {
	// Simplified vs Python: one line per product row + extras lines.
	*b = append(*b, line(strings.Repeat("-", receiptWidth))...)
	line1 := cell(fmt.Sprintf("%dx", qty), colAnz, false) + " " + cell(name, colProdukt, true) + " " +
		cell("", colZus, true) + " " + cell(priceDisplay, colPreis, false)
	*b = append(*b, line(line1)...)
	for _, exItem := range extras {
		em, ok := exItem.(map[string]any)
		if !ok {
			continue
		}
		en := anyString(em["additional_name"], "")
		if en == "" {
			continue
		}
		*b = append(*b, line("  + "+en)...)
	}
}

func anyString(v any, def string) string {
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case string:
		if t == "" {
			return def
		}
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

func anyFloat(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return def
		}
		return f
	default:
		return def
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
