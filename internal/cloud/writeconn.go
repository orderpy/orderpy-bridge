package cloud

import (
	"sync"

	"github.com/gorilla/websocket"
)

// writeConn serializes all writes on a single WebSocket connection.
//
// gorilla/websocket explicitly requires that no two goroutines write
// concurrently — interleaved frames silently corrupt the stream and the peer
// then closes the connection. The bridge has several concurrent writers
// (recvLoop dispatch, runPrinterHealth, spooler print_ack callbacks, pairing,
// updates), so every write must go through this wrapper.
type writeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *writeConn) WriteMessage(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(messageType, data)
}
