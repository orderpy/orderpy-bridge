package printer

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// chunkSize / chunkDelay — application-layer pacing that keeps us below the
// drain rate of the slowest receipt component (the raster bitmap).
//
// Why pace at all if TCP already does flow control: many embedded thermal
// printer TCP/IP stacks (notably the SAM4S Gcube series, several Bixolon
// SRP-2xx revisions, and most cheap Ethernet print servers) don't honour
// zero-window properly. When their fixed-size receive buffer (the Gcube 102
// ships with 4 KB) overruns, they silently *drop* bytes instead of telling
// us to slow down. The dropped bytes land mid-bitmap, the printer falls
// out of binary-image mode, and the rest of the receipt prints as garbage
// (or not at all).
//
// Sizing: a typical 80 mm thermal head consumes raster data at ~15 KB/s
// while printing graphics. 512 bytes × 50 ms = 10 KB/s — comfortably below
// that, so even a constantly-overflowed printer drains faster than we feed
// it. For text-only sections the printer is way faster, so this only adds
// a few hundred ms to a worst-case receipt (logo + long body + cut),
// which is invisible compared to the physical print time anyway.
const chunkSize = 512
const chunkDelay = 50 * time.Millisecond
const interJobDelay = 400 * time.Millisecond
const connectTimeout = 5 * time.Second
const drainTimeout = 15 * time.Second

var locks sync.Map // key "host:port" -> *sync.Mutex

func lockFor(addr string, port int) *sync.Mutex {
	key := fmt.Sprintf("%s:%d", addr, port)
	v, _ := locks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// SendBytes sends raw ESC/POS with chunking and per-endpoint serialization.
func SendBytes(ctx context.Context, address string, port int, data []byte) error {
	if address == "" || port < 1 || port > 65535 {
		return fmt.Errorf("invalid printer address")
	}
	m := lockFor(address, port)
	m.Lock()
	defer m.Unlock()
	err := sendBytesUnlocked(ctx, address, port, data)
	if err == nil {
		time.Sleep(interJobDelay)
	}
	return err
}

func sendBytesUnlocked(ctx context.Context, address string, port int, data []byte) error {
	d := net.Dialer{Timeout: connectTimeout}
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", address, port))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(120 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	for offset := 0; offset < len(data); offset += chunkSize {
		end := offset + chunkSize
		if end > len(data) {
			end = len(data)
		}
		if _, err := conn.Write(data[offset:end]); err != nil {
			return err
		}
		if end < len(data) {
			time.Sleep(chunkDelay)
		}
	}
	return nil
}

// Reachable tries TCP connect (health check).
func Reachable(ctx context.Context, address string, port int) bool {
	if address == "" || port < 1 || port > 65535 {
		return false
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	c, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", address, port))
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
