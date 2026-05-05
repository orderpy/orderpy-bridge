package pairing

// Result is the outcome of a pairing attempt (from cloud).
type Result struct {
	OK     bool
	Reason string
}

// Request is sent from the pairing HTTP handler to the cloud client goroutine.
type Request struct {
	Code string
	Done chan<- Result // capacity 1; cloud sends exactly one Result
}
