package spooler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/orderpy/orderpy-bridge-go/internal/escpos"
	"github.com/orderpy/orderpy-bridge-go/internal/printer"
	"github.com/orderpy/orderpy-bridge-go/internal/wire"
)

// nvFlashSettleDelay is the time we wait between an FS q "Define NV bit image"
// upload and the next print, so the printer's NV-flash has time to complete.
//
// Epson ESC/POS reference: "NV bit image programming might take a maximum of
// 4–5 seconds. Sending data during this period might cause incorrect
// operation." We err on the safe side at 4s — overruns mean the receipt that
// follows is silently dropped (or the printer wedges until power-cycled).
const nvFlashSettleDelay = 4 * time.Second

// Job is one unit of work for the spooler (semantic print).
type Job struct {
	CloudMsg wire.PrintJobFromCloud
	// SendAck sends print_ack or order_printed/failed to cloud when non-nil.
	SendAck func(ok bool, errClass, errMsg string)
}

// Spooler receives jobs and prints sequentially (global FIFO for simplicity; per-printer lock is inside printer.SendBytes).
type Spooler struct {
	ch chan Job

	// Per-printer NV-logo cache: "addr:port|slot" -> last loaded logo_hash.
	// Reset on bridge restart; the cloud sets logo_ref.image_b64 on every job
	// so a cache miss is recoverable in-band (FS q upload prepended).
	logoMu    sync.Mutex
	logoCache map[string]string
}

func New(buf int) *Spooler {
	if buf < 1 {
		buf = 1024
	}
	return &Spooler{ch: make(chan Job, buf), logoCache: make(map[string]string)}
}

func (s *Spooler) Enqueue(j Job) {
	s.ch <- j
}

func (s *Spooler) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-s.ch:
			s.handle(ctx, j)
		}
	}
}

func (s *Spooler) handle(ctx context.Context, j Job) {
	addr := j.CloudMsg.PrinterAddress
	port := j.CloudMsg.PrinterPort
	if port == 0 {
		port = 9100
	}
	data, err := escpos.Render(&j.CloudMsg)
	if err != nil {
		log.Printf("spooler: render error: %v", err)
		if j.SendAck != nil {
			j.SendAck(false, "format_error", err.Error())
		}
		return
	}

	// NV-logo provisioning: when the printer's NV slot doesn't hold this hash,
	// upload the FS q payload in its OWN TCP transaction, then wait for the
	// firmware to finish flashing before pushing the receipt. Otherwise the
	// receipt arrives during NV programming and is silently discarded — and
	// some firmwares get wedged until power-cycled.
	cacheKey, willUpdate, prelude := s.logoPrelude(addr, port, j.CloudMsg.LogoRef)
	if len(prelude) > 0 {
		if err := printer.SendBytes(ctx, addr, port, prelude); err != nil {
			log.Printf("spooler: NV logo upload %s:%d failed: %v", addr, port, err)
			if j.SendAck != nil {
				j.SendAck(false, "print_error", "logo_upload_failed: "+err.Error())
			}
			return
		}
		log.Printf("spooler: NV logo uploaded %s:%d (%d bytes); waiting %s for flash", addr, port, len(prelude), nvFlashSettleDelay)
		select {
		case <-time.After(nvFlashSettleDelay):
		case <-ctx.Done():
			return
		}
	}

	if err := printer.SendBytes(ctx, addr, port, data); err != nil {
		log.Printf("spooler: send %s:%d: %v", addr, port, err)
		if j.SendAck != nil {
			j.SendAck(false, "print_error", err.Error())
		}
		return
	}
	if willUpdate {
		s.logoMu.Lock()
		s.logoCache[cacheKey] = j.CloudMsg.LogoRef.Hash
		s.logoMu.Unlock()
	}
	if j.SendAck != nil {
		j.SendAck(true, "", "")
	}
}

// logoPrelude inspects the LogoRef and returns (cacheKey, willUpdate, preludeBytes).
// preludeBytes is empty when no upload is needed (no LogoRef, missing image, or cache hit).
// cacheKey/willUpdate let the caller commit the cache entry only after a successful send.
func (s *Spooler) logoPrelude(addr string, port int, ref *wire.LogoRef) (string, bool, []byte) {
	if ref == nil || ref.Hash == "" {
		return "", false, nil
	}
	key := fmt.Sprintf("%s:%d|%d", addr, port, ref.Slot)
	s.logoMu.Lock()
	cached, ok := s.logoCache[key]
	s.logoMu.Unlock()
	if ok && cached == ref.Hash {
		return key, false, nil
	}
	if ref.ImageB64 == "" {
		// Cache miss but cloud did not include the upload payload — nothing we can do.
		// (Receipt's FS p reference will print blank until the next job carries the upload.)
		log.Printf("spooler: logo cache miss %s but no image_b64; printer NV slot may be empty", key)
		return key, false, nil
	}
	prelude, err := base64.StdEncoding.DecodeString(ref.ImageB64)
	if err != nil || len(prelude) == 0 {
		log.Printf("spooler: logo decode error %s: %v", key, err)
		return key, false, nil
	}
	return key, true, prelude
}

// DecodePrintJob parses JSON into wire.PrintJobFromCloud.
func DecodePrintJob(raw []byte) (wire.PrintJobFromCloud, error) {
	var m wire.PrintJobFromCloud
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	return m, nil
}
