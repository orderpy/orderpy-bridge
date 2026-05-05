package cloud

import (
	"context"

	"github.com/orderpy/orderpy-bridge-go/internal/printer"
)

func sendBytesToPrinter(ctx context.Context, addr string, port int, data []byte) error {
	return printer.SendBytes(ctx, addr, port, data)
}
