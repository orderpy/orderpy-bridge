# OrderPy Bridge (Go)

Die **OrderPy Bridge** verbindet lokale Thermodrucker (TCP/ESC-POS) mit der OrderPy-Cloud: WebSocket-Session zum Print-Service, lokale HTTP-API für den POS/LAN-Druck und HTTPS-Pairing für die Gerätekopplung.

## Architektur (Kurz)

| Pfad | Port (Standard) | Rolle |
|------|-----------------|--------|
| Cloud WebSocket | — | `ORDERPY_CLOUD_URL` → `ws(s)://…/api/v1/bridges/connect` |
| Lokale API | **8080** | Health, Setup-Info, `POST /print` (LAN) |
| Pairing-UI | **8088** | HTTPS (selbstsigniert), `/api/status`, `/api/pair` |
| Drucker | variabel | Rohdaten per TCP (typisch Port 9100) |

Details zur Installation (Docker, nativ, Firewall): [docs/INSTALLATION.md](docs/INSTALLATION.md).

## Varianten (`ORDERPY_BRIDGE_VARIANT`)

Wird in [internal/config/config.go](internal/config/config.go) ausgewertet und im WebSocket-Handshake sowie in `/health` mitgegeben:

| Wert | Bedeutung |
|------|-----------|
| `daemon` | Linux-Installation unter systemd; **Auto-Update** über signiertes Release-Tarball (siehe unten). |
| `docker` | Container; Image-Update erfolgt durch den Betreiber (`docker pull` o. Ä.). Die Cloud markiert nur die Update-Absicht. |
| `android` | Mobile/Capacitor-Variante (Update außerhalb dieses Repos). |
| `unknown` | Default, wenn die Variable fehlt oder nicht erkannt wird. |

## Installation

### Option A: systemd (empfohlen, `daemon`)

Release-Assets liegen unter  
`https://github.com/orderpy/orderpy-bridge-go/releases`  
(Dateien: `orderpy-bridge-VERSION.tar.gz`, `.minisig`, `.sha256`).

```bash
VER=1.2.3
curl -fsSL -O "https://github.com/orderpy/orderpy-bridge-go/releases/download/v${VER}/orderpy-bridge-${VER}.tar.gz"
tar xzf "orderpy-bridge-${VER}.tar.gz"
sudo "./orderpy-bridge-${VER}/install.sh"
sudo nano /etc/orderpy-bridge/orderpy-bridge.env   # ORDERPY_CLOUD_URL, ORDERPY_ALLOWED_ORIGINS
sudo systemctl restart orderpy-bridge
```

Voraussetzungen: **minisign** (für Auto-Updates), **curl**, **python3**, **systemd**.

### Option B: Docker

Siehe [docker-compose.yml](docker-compose.yml) und [deploy/docker/Dockerfile](deploy/docker/Dockerfile).

### Option C: aus Quellen

```bash
go build -o bridge ./cmd/bridge
```

Siehe [docs/INSTALLATION.md](docs/INSTALLATION.md) für Pfade, systemd und Umgebungsvariablen.

## Konfiguration (Umgebungsvariablen)

| Variable | Bedeutung | Standard (Auszug) |
|----------|-----------|-------------------|
| `ORDERPY_CLOUD_URL` | HTTP(S)-Basis der API; daraus wird die WS-URL abgeleitet | `http://localhost:8001` |
| `ORDERPY_KEY_PATH` | Pfad zur privaten PEM; `.pub` daneben | `./data/bridge_key.pem` |
| `ORDERPY_ALLOWED_ORIGINS` | CORS für die lokale API (kommagetrennt) | u. a. localhost:5173 |
| `ORDERPY_BRIDGE_VARIANT` | `docker` \| `daemon` \| `android` | `unknown` |
| `ORDERPY_BRIDGE_WS_MAX_INBOUND` | Max. eingehende WS-Nachricht (Bytes, ≥ 1 MiB) | 16 MiB |
| `ORDERPY_PRINTER_HEALTH_INTERVAL` | Drucker-Health-Intervall (Sekunden, ≥ 5) | 10 |
| `ORDERPY_BRIDGE_RUNTIME_DIR` | Runtime / Update-Trigger (`update-requested.json`) | `/run/orderpy-bridge` |

## Lokale HTTP-API (Port 8080)

Implementierung: [internal/local/http.go](internal/local/http.go). Zugriff nur mit **IP im `Host`-Header** und erlaubter `Origin` (CORS).

| Methode | Pfad | Antwort / Verhalten |
|---------|------|---------------------|
| GET | `/health` | `status`, `version`, `variant`, `update_status` |
| GET | `/setup/info` | `bridgeId`, `claimToken`, `pairing_ui`, `version`, `variant` (wenn Bridge registriert) |
| POST | `/print` | Body: `wire.PrintJobFromCloud` → lokaler Spooler (ohne Cloud-`print_ack`) |

## Pairing (Port 8088)

- HTTPS mit selbstsigniertem Zertifikat.
- `GET /api/status` — `paired`, `tenant_name`, `version`, `variant`
- `POST /api/pair` — JSON `{ "code": "123456" }`

## Wire-Protokoll v3

Zentrale Go-Typen: [internal/wire/v3.go](internal/wire/v3.go)

- `PrintJobFromCloud`, `LogoRef`, `HandshakeOut`, `PrintAck`
- Semantik der `receipt`-Felder: [docs/wire-protocol-v3.md](docs/wire-protocol-v3.md)

**Bridge → Cloud** (Auswahl): `print_ack`, `printer_status`, `order_printed`, `order_print_failed`, `update_status`, `submit_pairing_code`, `unpair_me`, …

**Cloud → Bridge** (Auswahl): `config`, `config_update`, `print_job`, `print_order`, `print_service_call`, `print_test`, `unpaired`, `update`, `verify_claim`, `pairing_result`, …

## Auto-Updates (`daemon`)

1. Das SaaS-Backend liest das Release-Manifest von **GitHub** (`orderpy/orderpy-bridge-go`, konfigurierbar über `BRIDGE_RELEASE_REPO`; Cache ca. **10 Minuten** — bei sofortigem Rollout ggf. `BRIDGE_LATEST_VERSION` setzen).
2. Bei freigegebenem Update sendet der Print-Service an die Bridge eine WebSocket-Nachricht `action: "update"` mit `target_version`, `tarball_url`, `minisig_url`, `tarball_sha256`.
3. Die Bridge prüft URLs gegen eine feste Allowlist ([internal/updater/trigger.go](internal/updater/trigger.go)) und schreibt `/run/orderpy-bridge/update-requested.json`.
4. **systemd** `orderpy-bridge-updater.path` startet `orderpy-bridge-updater.service`.
5. [deploy/updater/orderpy-bridge-updater.sh](deploy/updater/orderpy-bridge-updater.sh) lädt Tarball + Signatur, prüft **SHA-256** und **minisign** mit `updater/minisign.pub`, tauscht `/opt/orderpy-bridge` atomar aus, startet `orderpy-bridge` neu, schreibt `update-result.json`.
6. Die Bridge meldet den Status per `update_status` zurück.

Manuelle vs. automatische Updates, POS- und Kassen-Gates: Cloud-Backend-Modul `bridge_updater_service` (OrderPy SaaS).

## Releases für Maintainer

1. Repository-Secrets setzen: **`MINISIGN_SECRET_KEY`** (vollständiger Inhalt der Datei `minisign.key`) und **`MINISIGN_PASSWORD`** (Passphrase des Schlüssels). Öffentlicher Schlüssel: committed unter `deploy/updater/minisign.pub` (muss zum Secret-Key passen).
2. Neuen Schlüssel erzeugen (einmalig / Rotation):

   ```bash
   minisign -G -p deploy/updater/minisign.pub -s deploy/updater/minisign.key
   ```

   `minisign.key` **nicht** committen (siehe [.gitignore](.gitignore)).

3. Tag pushen:

   ```bash
   git tag v1.2.3
   git push origin v1.2.3
   ```

   Workflow [.github/workflows/release.yml](.github/workflows/release.yml): `go test` / `go vet`, Build **linux/amd64**, Tarball, SHA256, Signatur, GitHub Release.

   Alternativ: **Actions → release → Run workflow** mit Eingabe `version` (ohne `v`).

## Sicherheit

- **minisign**: Tarball-Integrität und Authentizität; Public Key in jedem Release unter `updater/minisign.pub`.
- **Allowlist**: Download-URLs nur von `github.com/orderpy/orderpy-bridge-go/releases/download/vX.Y.Z/…`.
- **Schlüsselrotation**: neuen Key generieren, Secrets + `minisign.pub` im Repo aktualisieren, neues Release — ältere Installationen ohne manuelles Update vertrauen ggf. nicht mehr der neuen Signaturkette für automatische Updates.
- **`ORDERPY_KEY_PATH`**: private PEM nur für den Bridge-User lesbar halten (z. B. `chmod 600`).

## CI

- [.github/workflows/go.yml](.github/workflows/go.yml): `go test`, `go vet`, `go build` auf Push/PR.
- [.github/workflows/release.yml](.github/workflows/release.yml): Release bei Tags `v*.*.*` oder manuell.
