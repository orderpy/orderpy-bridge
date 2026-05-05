package escpos

import "testing"

func TestBuildTestPrint(t *testing.T) {
	b := buildTestPrint()
	if len(b) < 20 {
		t.Fatalf("short output: %d", len(b))
	}
	if b[0] != 0x1b || b[1] != 0x40 {
		t.Fatalf("expected init ESC @")
	}
}
