package pairing

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/orderpy/orderpy-bridge-go/internal/config"
	"github.com/orderpy/orderpy-bridge-go/internal/state"
	"github.com/orderpy/orderpy-bridge-go/internal/version"
)

var hostIPPattern = regexp.MustCompile(`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?::\d+)?$`)

// ServeHTTPS runs the pairing UI on addr (e.g. :8088). pairingCh sends pairing codes to cloud client.
func ServeHTTPS(ctx context.Context, addr string, pairingCh chan<- Request) {
	cert, err := selfSignedCert()
	if err != nil {
		log.Printf("pairing: cert: %v", err)
		return
	}
	srv := &http.Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		Handler: pairingHandler(pairingCh),
	}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	log.Printf("pairing: HTTPS listening on %s", addr)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("pairing: listen: %v", err)
		return
	}
	tlsLn := tls.NewListener(ln, srv.TLSConfig)
	if err := srv.Serve(tlsLn); err != nil && err != http.ErrServerClosed {
		log.Printf("pairing: server stopped: %v", err)
	}
}

func pairingHandler(pairingCh chan<- Request) http.Handler {
	mux := http.NewServeMux()
	// Register specific paths before "/" so they are not swallowed by the root pattern.
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if !hostIPPattern.MatchString(r.Host) {
			http.Error(w, "Forbidden: Direct IP access only", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writePairJSON(w, map[string]any{
			"paired":      state.IsClaimed(),
			"tenant_name": state.GetTenantName(),
			"version":     version.Version,
			"variant":     config.Variant(),
		})
	})
	mux.HandleFunc("/api/pair", func(w http.ResponseWriter, r *http.Request) {
		if !hostIPPattern.MatchString(r.Host) {
			http.Error(w, "Forbidden: Direct IP access only", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		code := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, body.Code)
		if len(code) != 6 {
			writePairJSON(w, map[string]any{"ok": false, "reason": "invalid_code"})
			return
		}
		done := make(chan Result, 1)
		select {
		case pairingCh <- Request{Code: code, Done: done}:
		default:
			writePairJSON(w, map[string]any{"ok": false, "reason": "busy"})
			return
		}
		select {
		case res := <-done:
			writePairJSON(w, map[string]any{"ok": res.OK, "reason": res.Reason})
		case <-time.After(15 * time.Second):
			writePairJSON(w, map[string]any{"ok": false, "reason": "timeout"})
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !hostIPPattern.MatchString(r.Host) {
			http.Error(w, "Forbidden: Direct IP access only", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pairingHTML))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	})
}

func writePairJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func selfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"orderpy.app Bridge"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(0, 0, 0, 0)},
		DNSNames:     []string{},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return tls.X509KeyPair(certPEM, keyPEM)
}

const pairingHTML = `<!DOCTYPE html>
<html lang="de">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>orderpy.app – Bridge</title>
<style>
  :root {
    --bg: #f3f4f6;
    --card: #ffffff;
    --text: #111827;
    --muted: #6b7280;
    --border: #e5e7eb;
    --orange: #ea580c;
    --orange-hover: #c2410c;
    --orange-soft: #fff7ed;
    --orange-ring: rgba(249, 115, 22, 0.35);
    --ok: #15803d;
    --err: #dc2626;
    --wait: #b45309;
    --shadow: 0 25px 50px -12px rgba(15, 23, 42, 0.12);
    font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  }
  * { box-sizing: border-box; }
  html, body {
    margin: 0;
    min-height: 100vh;
    min-height: 100dvh;
  }
  body {
    background: var(--bg);
    color: var(--text);
    line-height: 1.5;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1.25rem;
  }
  .card {
    width: 100%;
    max-width: 26rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 1.25rem;
    box-shadow: var(--shadow);
    padding: 2rem 1.75rem;
  }
  .brand {
    margin-bottom: 1.25rem;
  }
  .brand-text {
    font-weight: 500;
    font-size: 1.05rem;
    letter-spacing: -0.02em;
    color: #374151;
  }
  .brand-tld {
    color: var(--orange);
    font-weight: 500;
  }
  .pair-intro {
    font-size: 1.05rem;
    font-weight: 500;
    color: #4b5563;
    margin: 0 0 1.3rem;
    line-height: 1.45;
    letter-spacing: -0.015em;
    text-align: center;
  }
  .code-digits {
    display: flex;
    gap: 0.4rem;
    justify-content: center;
    flex-wrap: nowrap;
  }
  .code-digit {
    width: 2.55rem;
    height: 3rem;
    box-sizing: border-box;
    font-size: 1.2rem;
    font-weight: 600;
    text-align: center;
    border-radius: 0.75rem;
    border: 1px solid var(--border);
    background: #f9fafb;
    color: var(--text);
    transition: border-color 0.15s, box-shadow 0.15s, background 0.15s;
  }
  .code-digit:focus {
    outline: none;
    border-color: var(--orange);
    box-shadow: 0 0 0 3px var(--orange-ring);
    background: #fff;
  }
  @media (min-width: 380px) {
    .code-digit { width: 2.75rem; height: 3.15rem; font-size: 1.25rem; }
  }
  .btn-confirm {
    width: 100%;
    margin-top: 1.4rem;
    padding: 0.95rem 1.5rem;
    font-size: 0.9375rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    border: none;
    border-radius: 9999px;
    color: #fff;
    cursor: pointer;
    background: linear-gradient(165deg, #fb923c 0%, #ea580c 42%, #c2410c 100%);
    box-shadow:
      0 1px 0 rgba(255, 255, 255, 0.22) inset,
      0 4px 14px rgba(234, 88, 12, 0.28),
      0 14px 32px -10px rgba(194, 65, 12, 0.45);
    transition: transform 0.14s ease, box-shadow 0.14s ease, filter 0.14s ease;
  }
  .btn-confirm:hover:not(:disabled) {
    filter: brightness(1.05);
    transform: translateY(-1px);
    box-shadow:
      0 1px 0 rgba(255, 255, 255, 0.25) inset,
      0 6px 18px rgba(234, 88, 12, 0.32),
      0 18px 40px -10px rgba(194, 65, 12, 0.5);
  }
  .btn-confirm:active:not(:disabled) {
    transform: translateY(0);
    filter: brightness(0.97);
  }
  .btn-confirm:focus-visible {
    outline: none;
    box-shadow:
      0 1px 0 rgba(255, 255, 255, 0.22) inset,
      0 4px 14px rgba(234, 88, 12, 0.28),
      0 0 0 3px #fff,
      0 0 0 5px var(--orange);
  }
  .btn-confirm:disabled {
    opacity: 0.52;
    cursor: not-allowed;
    transform: none;
    filter: none;
    box-shadow: none;
  }
  .is-hidden { display: none !important; }
  #msg { margin-top: 1rem; font-size: 0.9rem; min-height: 1.35em; }
  #msg.ok { color: var(--ok); }
  #msg.err { color: var(--err); }
  #msg.wait { color: var(--wait); }
  .boot-text { color: var(--muted); font-size: 0.95rem; text-align: center; margin: 0; }
  .card--info {
    position: relative;
    overflow: hidden;
    padding-top: 1.65rem;
  }
  .card--info::before {
    content: "";
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 3px;
    background: linear-gradient(90deg, #fdba74, var(--orange), #fb923c);
  }
  .card--info .brand { margin-bottom: 0.35rem; }
  .info-header {
    text-align: center;
    padding: 0.5rem 0 1.35rem;
    position: relative;
  }
  .info-header::after {
    content: "";
    position: absolute;
    bottom: 0;
    left: 8%;
    right: 8%;
    height: 1px;
    background: linear-gradient(90deg, transparent, #e5e7eb 20%, #e5e7eb 80%, transparent);
  }
  .info-icon-wrap {
    width: 4.5rem;
    height: 4.5rem;
    margin: 0 auto 1rem;
    border-radius: 50%;
    background: radial-gradient(circle at 30% 25%, #fff 0%, #fff7ed 45%, #ffedd5 100%);
    box-shadow:
      0 0 0 1px rgba(251, 146, 60, 0.45),
      0 10px 28px -8px rgba(234, 88, 12, 0.35),
      inset 0 1px 0 rgba(255, 255, 255, 0.85);
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .info-icon-svg {
    width: 2.4rem;
    height: 2.4rem;
    color: var(--orange);
  }
  .info-kicker {
    font-size: 0.65rem;
    font-weight: 700;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: #c2410c;
    margin: 0 0 0.3rem;
  }
  .info-heading {
    font-size: 1.4rem;
    font-weight: 800;
    margin: 0;
    letter-spacing: -0.035em;
    line-height: 1.2;
    color: var(--text);
  }
  .info-tenant-panel {
    margin: 0 0 1.1rem;
    padding: 1.2rem 1.25rem 1.15rem;
    border-radius: 1rem;
    background: linear-gradient(165deg, #ffffff 0%, #f9fafb 55%, #f3f4f6 100%);
    border: 1px solid var(--border);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.9);
    text-align: center;
  }
  .info-tenant-label {
    display: block;
    font-size: 0.65rem;
    font-weight: 700;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: #9ca3af;
    margin-bottom: 0.5rem;
  }
  .info-tenant {
    font-size: 1.35rem;
    font-weight: 800;
    margin: 0;
    letter-spacing: -0.03em;
    line-height: 1.25;
    word-break: break-word;
    color: #111827;
  }
  .info-tenant.info-tenant--placeholder {
    font-size: 1rem;
    font-weight: 600;
    font-style: italic;
    color: var(--muted);
  }
  .info-dl { margin: 0; padding: 0; }
  .info-dl-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
    padding: 0.8rem 1rem;
    margin-top: 0.45rem;
    background: #fafafa;
    border: 1px solid #ececec;
    border-radius: 0.85rem;
    font-size: 0.8125rem;
  }
  .info-dl-row:first-child { margin-top: 0; }
  .info-dl-row .info-k {
    font-weight: 600;
    color: #6b7280;
    flex-shrink: 0;
  }
  .info-dl-row .info-v {
    margin: 0;
    font-family: ui-monospace, "SF Mono", Menlo, Monaco, Consolas, monospace;
    font-size: 0.8125rem;
    font-weight: 600;
    color: #1f2937;
    text-align: right;
    word-break: break-all;
    max-width: 58%;
  }
</style>
</head>
<body>
<div id="viewBoot" class="card">
  <div class="brand"><span class="brand-text">orderpy<span class="brand-tld">.app</span></span></div>
  <p class="boot-text">Status wird geladen …</p>
</div>
<div id="viewPair" class="card is-hidden">
  <div class="brand"><span class="brand-text">orderpy<span class="brand-tld">.app</span></span></div>
  <p class="pair-intro" id="pairIntro">Bridge-Pairingcode eingeben …</p>
  <form id="f" autocomplete="off">
    <div class="code-digits" role="group" aria-labelledby="pairIntro" aria-describedby="msg">
      <input class="code-digit" id="code0" name="code0" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="1" autocorrect="off" spellcheck="false" autocomplete="one-time-code">
      <input class="code-digit" id="code1" name="code1" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="1" autocorrect="off" spellcheck="false" autocomplete="off">
      <input class="code-digit" id="code2" name="code2" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="1" autocorrect="off" spellcheck="false" autocomplete="off">
      <input class="code-digit" id="code3" name="code3" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="1" autocorrect="off" spellcheck="false" autocomplete="off">
      <input class="code-digit" id="code4" name="code4" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="1" autocorrect="off" spellcheck="false" autocomplete="off">
      <input class="code-digit" id="code5" name="code5" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="1" autocorrect="off" spellcheck="false" autocomplete="off">
    </div>
    <button type="submit" class="btn-confirm" id="btn">Bestätigen</button>
  </form>
  <p id="msg" role="status"></p>
</div>
<div id="viewInfo" class="card card--info is-hidden">
  <div class="brand"><span class="brand-text">orderpy<span class="brand-tld">.app</span></span></div>
  <header class="info-header">
    <div class="info-icon-wrap" aria-hidden="true">
      <svg class="info-icon-svg" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path d="M5 13l4 4L19 7" stroke="currentColor" stroke-width="2.25" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
    </div>
    <p class="info-kicker">Aktiv gekoppelt</p>
    <h1 class="info-heading">Bridge verbunden</h1>
  </header>
  <div class="info-tenant-panel">
    <span class="info-tenant-label">Mandant</span>
    <p id="infoTenant" class="info-tenant"></p>
  </div>
  <div class="info-dl">
    <div class="info-dl-row">
      <span class="info-k">Bridge-Version</span>
      <span id="infoVersion" class="info-v"></span>
    </div>
    <div id="infoVariantRow" class="info-dl-row is-hidden">
      <span class="info-k">Variante</span>
      <span id="infoVariant" class="info-v"></span>
    </div>
  </div>
</div>
<script>
(function () {
  var boot = document.getElementById("viewBoot");
  var pair = document.getElementById("viewPair");
  var info = document.getElementById("viewInfo");
  var form = document.getElementById("f");
  var CODE_LEN = 6;
  var digitInputs = [];
  for (var ci = 0; ci < CODE_LEN; ci++) {
    digitInputs.push(document.getElementById("code" + ci));
  }
  var codeGroup = form.querySelector(".code-digits");
  var msg = document.getElementById("msg");
  var btn = document.getElementById("btn");
  var infoTenant = document.getElementById("infoTenant");
  var infoVersion = document.getElementById("infoVersion");
  var infoVariantRow = document.getElementById("infoVariantRow");
  var infoVariantVal = document.getElementById("infoVariant");
  var pending = false;
  var pollTimer = null;
  var pollAttempts = 0;
  var maxPollAttempts = 15;

  function show(el) { el.classList.remove("is-hidden"); }
  function hide(el) { el.classList.add("is-hidden"); }

  function reasonText(r) {
    var m = {
      invalid_code: "Bitte genau 6 Ziffern eingeben.",
      busy: "Bitte kurz warten und erneut versuchen.",
      timeout: "Keine Antwort von der Cloud. Netzwerk prüfen und erneut versuchen.",
      send_failed: "Senden fehlgeschlagen. Verbindung zur Cloud prüfen.",
      code_expired_or_used:
        "Dieser Code ist abgelaufen oder wurde schon verwendet. Bitte in der Verwaltung einen neuen Pairing-Code anfordern.",
      code_for_other_bridge:
        "Dieser Code gehört zu einem anderen Gerät. Bitte einen neuen Code anfordern oder das richtige Gerät koppeln.",
      bridge_already_claimed:
        "Diese Bridge ist bereits mit einem Standort verbunden. Bitte dort zuerst trennen oder die Verwaltung kontaktieren."
    };
    if (!r) return "Unbekannter Fehler.";
    return m[r] || r;
  }
  function setMsg(text, cls) {
    msg.textContent = text;
    msg.className = cls || "";
  }
  function digitsOnly(v) {
    return String(v).replace(/\D/g, "");
  }
  function getCode() {
    var s = "";
    for (var i = 0; i < CODE_LEN; i++) {
      var d = digitsOnly(digitInputs[i].value).slice(-1);
      s += d;
    }
    return s;
  }
  function setCodeBoxes(str) {
    var d = digitsOnly(str).slice(0, CODE_LEN);
    for (var i = 0; i < CODE_LEN; i++) {
      digitInputs[i].value = d[i] || "";
    }
  }
  function clearCodeBoxes() {
    for (var i = 0; i < CODE_LEN; i++) {
      digitInputs[i].value = "";
    }
  }
  function focusFirstDigit() {
    digitInputs[0].focus();
  }

  function applyInfoView(data) {
    var name = (data.tenant_name && String(data.tenant_name).trim()) || "";
    if (name) {
      infoTenant.textContent = name;
      infoTenant.classList.remove("info-tenant--placeholder");
    } else {
      infoTenant.textContent = "Name wird geladen …";
      infoTenant.classList.add("info-tenant--placeholder");
    }
    infoVersion.textContent = data.version || "–";
    var v = data.variant && String(data.variant).trim();
    if (v) {
      infoVariantVal.textContent = v;
      infoVariantRow.classList.remove("is-hidden");
    } else {
      infoVariantVal.textContent = "";
      infoVariantRow.classList.add("is-hidden");
    }
  }

  function stopPoll() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    pollAttempts = 0;
  }

  function maybeStartPoll(data) {
    stopPoll();
    if (!data.paired) return;
    var name = (data.tenant_name && String(data.tenant_name).trim()) || "";
    if (name) return;
    pollTimer = setInterval(function () {
      pollAttempts++;
      if (pollAttempts > maxPollAttempts) {
        stopPoll();
        return;
      }
      fetch("/api/status").then(function (r) { return r.json(); }).then(function (d) {
        if (d && d.tenant_name && String(d.tenant_name).trim()) {
          applyInfoView(d);
          stopPoll();
        }
      }).catch(function () {});
    }, 2500);
  }

  function showMainView(data) {
    hide(boot);
    if (data.paired) {
      hide(pair);
      show(info);
      applyInfoView(data);
      maybeStartPoll(data);
    } else {
      hide(info);
      show(pair);
      stopPoll();
      focusFirstDigit();
    }
  }

  async function loadStatus() {
    try {
      var res = await fetch("/api/status");
      if (!res.ok) throw new Error("status");
      var data = await res.json();
      showMainView(data);
      return data;
    } catch (e) {
      hide(boot);
      show(pair);
      setMsg("Status konnte nicht geladen werden. Du kannst trotzdem koppeln.", "err");
      focusFirstDigit();
      return null;
    }
  }

  async function submitCode() {
    var code = getCode();
    if (code.length !== CODE_LEN) {
      setMsg(reasonText("invalid_code"), "err");
      return;
    }
    if (pending) return;
    pending = true;
    btn.disabled = true;
    setMsg("Code wird gesendet …", "wait");
    try {
      var res = await fetch("/api/pair", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code: code })
      });
      var text = await res.text();
      var data = null;
      try { data = JSON.parse(text); } catch (e1) {}
      if (!res.ok) {
        setMsg("Fehler (" + res.status + "): " + (text || res.statusText), "err");
        return;
      }
      if (!data) {
        setMsg("Ungültige Antwort vom Server.", "err");
        return;
      }
      if (data.ok) {
        clearCodeBoxes();
        setMsg("", "");
        try {
          var stRes = await fetch("/api/status");
          var st = stRes.ok ? await stRes.json() : null;
          showMainView(st || { paired: true, tenant_name: "", version: "", variant: "" });
        } catch (e3) {
          showMainView({ paired: true, tenant_name: "", version: "", variant: "" });
        }
      } else {
        setMsg(reasonText(data.reason), "err");
      }
    } catch (e2) {
      setMsg("Netzwerkfehler. Seite neu laden und erneut versuchen.", "err");
    } finally {
      pending = false;
      btn.disabled = false;
    }
  }

  form.addEventListener("submit", function (e) {
    e.preventDefault();
    submitCode();
  });
  codeGroup.addEventListener("paste", function (e) {
    e.preventDefault();
    var pasted = digitsOnly(e.clipboardData.getData("text")).slice(0, CODE_LEN);
    if (!pasted) return;
    setCodeBoxes(pasted);
    var end = Math.min(pasted.length, CODE_LEN) - 1;
    digitInputs[end].focus();
    if (getCode().length === CODE_LEN) submitCode();
  });
  for (var idx = 0; idx < CODE_LEN; idx++) {
    (function (index) {
      var el = digitInputs[index];
      el.addEventListener("keydown", function (e) {
        if (e.key === "Backspace" && !el.value && index > 0) {
          digitInputs[index - 1].value = "";
          digitInputs[index - 1].focus();
          e.preventDefault();
        } else if (e.key === "ArrowLeft" && index > 0) {
          digitInputs[index - 1].focus();
          e.preventDefault();
        } else if (e.key === "ArrowRight" && index < CODE_LEN - 1) {
          digitInputs[index + 1].focus();
          e.preventDefault();
        }
      });
      el.addEventListener("input", function () {
        var raw = digitsOnly(el.value);
        if (raw.length > 1) {
          setCodeBoxes(raw);
          var last = Math.min(raw.length, CODE_LEN) - 1;
          digitInputs[last].focus();
        } else {
          el.value = raw.slice(-1) || "";
          if (raw && index < CODE_LEN - 1) {
            digitInputs[index + 1].focus();
          }
        }
        if (getCode().length === CODE_LEN) submitCode();
      });
    })(idx);
  }

  loadStatus();
})();
</script>
</body>
</html>`
