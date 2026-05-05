package state

import (
	"crypto/rand"
	"crypto/subtle"
	"sync"
	"time"
)

const failThreshold = 5
const lockSeconds = 300

var mu sync.RWMutex

var claimToken string
var claimed bool
var bridgeID string
var tenantName string
var wsConnected bool
var failCount int
var lockUntil time.Time
var updateStatus string
var updateTargetVersion string

func GetClaimToken() string {
	mu.Lock()
	defer mu.Unlock()
	if claimed {
		return ""
	}
	if claimToken == "" {
		claimToken = randomHex(32)
	}
	return claimToken
}

func RotateClaimToken() string {
	mu.Lock()
	defer mu.Unlock()
	if claimed {
		return ""
	}
	claimToken = randomHex(32)
	return claimToken
}

func InvalidateClaimToken() {
	mu.Lock()
	defer mu.Unlock()
	claimToken = ""
	claimed = true
}

func Unpair() string {
	mu.Lock()
	defer mu.Unlock()
	claimed = false
	tenantName = ""
	claimToken = randomHex(32)
	return claimToken
}

func SetBridgeID(id string) {
	mu.Lock()
	defer mu.Unlock()
	bridgeID = id
}

func GetBridgeID() string {
	mu.RLock()
	defer mu.RUnlock()
	return bridgeID
}

func IsClaimed() bool {
	mu.RLock()
	defer mu.RUnlock()
	return claimed
}

func SetTenantName(name string) {
	mu.Lock()
	defer mu.Unlock()
	tenantName = name
}

func GetTenantName() string {
	mu.RLock()
	defer mu.RUnlock()
	return tenantName
}

func SetWSConnected(v bool) {
	mu.Lock()
	defer mu.Unlock()
	wsConnected = v
}

func GetWSConnected() bool {
	mu.RLock()
	defer mu.RUnlock()
	return wsConnected
}

func CheckToken(tok string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if claimToken == "" || tok == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(claimToken), []byte(tok)) == 1
}

func RecordVerifyFailure() {
	mu.Lock()
	defer mu.Unlock()
	failCount++
	if failCount >= failThreshold {
		lockUntil = time.Now().Add(lockSeconds * time.Second)
	}
}

func IsLocked() bool {
	mu.Lock()
	defer mu.Unlock()
	if !lockUntil.IsZero() && time.Now().Before(lockUntil) {
		return true
	}
	if !lockUntil.IsZero() && time.Now().After(lockUntil) {
		failCount = 0
	}
	return false
}

func SetUpdateStatus(status, targetVer string) {
	mu.Lock()
	defer mu.Unlock()
	updateStatus = status
	if targetVer != "" {
		updateTargetVersion = targetVer
	} else if status == "idle" || status == "success" || status == "failed" {
		updateTargetVersion = ""
	}
}

func GetUpdateStatus() string {
	mu.RLock()
	defer mu.RUnlock()
	if updateStatus == "" {
		return "idle"
	}
	return updateStatus
}

func GetUpdateTargetVersion() string {
	mu.RLock()
	defer mu.RUnlock()
	return updateTargetVersion
}

func randomHex(nBytes int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, nBytes)
	_, _ = randRead(b)
	out := make([]byte, nBytes*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}

// randRead wraps crypto/rand.Read for testability.
var randRead = func(b []byte) (int, error) {
	return randReadImpl(b)
}

func randReadImpl(b []byte) (int, error) {
	return rand.Read(b) // crypto/rand
}
