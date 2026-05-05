# OrderPy Bridge (Go) – Installation

Die Go-Bridge verbindet den lokalen Druckspooler mit der OrderPy-Cloud per WebSocket, stellt die **lokale HTTP-API** für den Druck bereit und den **Pairing-Server** (HTTPS, selbstsigniert) für die Kopplung mit dem Konto.

| Dienst | Port (Standard) | Protokoll |
|--------|------------------|-----------|
| Lokale Druck-/Setup-API | **8080** | HTTP |
| Pairing-UI | **8088** | HTTPS (Zertifikat wird beim Start erzeugt) |

---

## Voraussetzungen

- **OrderPy-Backend** (App-API) erreichbar, inkl. WebSocket-Endpunkt für die Bridge (typisch über `ORDERPY_CLOUD_URL`).
- Auf dem Rechner, auf dem die Bridge läuft, freie Ports **8080** und **8088** (oder eigene Adressen, siehe unten – derzeit sind Ports im Quellcode fest; für andere Ports ist ein Build/Anpassung nötig).
- **Schlüsseldatei**: Beim ersten Start wird unter `ORDERPY_KEY_PATH` ein RSA-Schlüsselpaar angelegt (analog zur Python-Bridge), sofern noch keine `.pub` existiert.

---

## Option A: Docker Compose (empfohlen für Entwicklung)

Im Repository-Root:

```bash
docker compose build
docker compose up -d
```

Standard aus `docker-compose.yml`:

- `./data` wird nach `/app/data` gemountet; dort liegt `bridge_key.pem` (bzw. generierte Schlüssel).
- **`ORDERPY_CLOUD_URL`**: Zeigt auf die **HTTP(S)-Basis-URL des App-Backends** (ohne trailing slash). Aus dieser URL wird die WebSocket-Adresse abgeleitet (`http` → `ws`, `https` → `wss`).

**Linux:** `host.docker.internal` ist nicht immer verfügbar. Die Compose-Datei setzt `extra_hosts: host.docker.internal:host-gateway`. Passt die URL an, wenn dein Backend woanders läuft, z. B.:

```yaml
environment:
  - ORDERPY_CLOUD_URL=http://192.168.1.10:8001
```

**CORS / Browser:** `ORDERPY_ALLOWED_ORIGINS` muss die Origins enthalten, von denen aus die lokale API (`http://<bridge-ip>:8080`) im Browser aufgerufen wird (z. B. POS- oder Setup-Frontend).

Logs:

```bash
docker compose logs -f orderpy-bridge
```

Health (lokal vom Host):

```bash
curl -s http://127.0.0.1:8080/health
```

---

## Option B: Nur Docker-Image bauen

```bash
docker build -f deploy/docker/Dockerfile -t orderpy-bridge-go:local .
docker run --rm -p 8080:8080 -p 8088:8088 \
  -v "$(pwd)/data:/app/data" \
  -e ORDERPY_KEY_PATH=/app/data/bridge_key.pem \
  -e ORDERPY_CLOUD_URL=http://host.docker.internal:8001 \
  -e ORDERPY_ALLOWED_ORIGINS=http://localhost:5173 \
  orderpy-bridge-go:local
```

Unter Linux ggf. `--add-host=host.docker.internal:host-gateway` statt oder zusätzlich zur `host.docker.internal`-URL.

---

## Option C: Nativ (Binary ohne Container)

### 1. Bauen

Voraussetzung: Go **1.22+** (siehe `go.mod`).

```bash
cd orderpy-bridge-go
go build -o bridge ./cmd/bridge
```

### 2. Verzeichnis und Umgebung

```bash
sudo mkdir -p /opt/orderpy-bridge /var/lib/orderpy-bridge
sudo cp bridge /opt/orderpy-bridge/bridge
sudo chmod 755 /opt/orderpy-bridge/bridge
```

Beispiel **`/etc/orderpy-bridge/orderpy-bridge.env`** (Rechte z. B. `640`, root:root):

```bash
ORDERPY_BRIDGE_VARIANT=daemon
ORDERPY_KEY_PATH=/var/lib/orderpy-bridge/bridge_key.pem
ORDERPY_CLOUD_URL=https://api.example.com
ORDERPY_ALLOWED_ORIGINS=https://pos.example.com,http://localhost:5173
# optional:
# ORDERPY_BRIDGE_WS_MAX_INBOUND=16777216
# ORDERPY_PRINTER_HEALTH_INTERVAL=10
# ORDERPY_BRIDGE_RUNTIME_DIR=/run/orderpy-bridge
```

`ORDERPY_BRIDGE_RUNTIME_DIR` wird u. a. für Update-/Runtime-Status genutzt; das Verzeichnis muss für den Bridge-User beschreibbar sein.

### 3. Systemd (Referenz)

Im Repo liegt eine Beispiel-Unit unter `deploy/systemd/orderpy-bridge.service`. Ablauf in Kurzform:

1. Systembenutzer anlegen, z. B. `orderpy-bridge`, Besitzer von `/var/lib/orderpy-bridge` und ggf. `/run/orderpy-bridge`.
2. Unit nach `/etc/systemd/system/orderpy-bridge.service` kopieren und Pfade prüfen.
3. `sudo systemctl daemon-reload && sudo systemctl enable --now orderpy-bridge`

Die Unit erwartet `EnvironmentFile=/etc/orderpy-bridge/orderpy-bridge.env` – Inhalt wie oben.

---

## Umgebungsvariablen (Überblick)

| Variable | Bedeutung | Standard |
|----------|-----------|----------|
| `ORDERPY_CLOUD_URL` | Basis-URL des App-Backends (HTTP/HTTPS), für WS-Ableitung | `http://localhost:8001` |
| `ORDERPY_KEY_PATH` | Pfad zur privaten PEM; `.pub` wird daneben erwartet/generiert | `./data/bridge_key.pem` |
| `ORDERPY_ALLOWED_ORIGINS` | Komma-getrennte Origins für CORS der lokalen API | u. a. localhost:3000 / :5173 |
| `ORDERPY_BRIDGE_VARIANT` | Kennzeichnung in `/health` (`docker`, `daemon`, `android`, …) | `unknown` |
| `ORDERPY_BRIDGE_WS_MAX_INBOUND` | Max. Größe eingehender WS-Nachrichten (Bytes, ≥ 1 MiB) | 16 MiB |
| `ORDERPY_PRINTER_HEALTH_INTERVAL` | Drucker-Health-Intervall in Sekunden (≥ 5) | 10 |
| `ORDERPY_BRIDGE_RUNTIME_DIR` | Runtime-/Update-Pfade | leer |

Im Docker-Image setzt das Dockerfile `ORDERPY_BRIDGE_VARIANT=docker`.

---

## Pairing (Port 8088)

- Die Pairing-Oberfläche läuft über **HTTPS** mit **selbstsigniertem** Zertifikat. Im Browser einmalig Zertifikat akzeptieren bzw. Ausnahme setzen.
- Zugriff ist so ausgelegt, dass der **Host-Header eine IP-Adresse** enthält (z. B. `https://192.168.1.42:8088/`). Aufruf über einen DNS-Namen kann mit „Forbidden: Direct IP access only“ beantwortet werden – dann die LAN-IP des Bridge-Rechners verwenden.

---

## Firewall

Eingehend auf dem Bridge-Host freigeben (falls Druck von anderen Geräten im LAN):

- **TCP 8080** – lokale API / Health  
- **TCP 8088** – Pairing-HTTPS  

Ausgehend: HTTPS/WSS zum `ORDERPY_CLOUD_URL`-Host und ggf. zum Drucker (TCP je nach Konfiguration).

---

## Kurz-Checkliste nach der Installation

1. `curl http://<bridge-ip>:8080/health` → JSON mit `status`, `version`, `variant`.  
2. Öffentlichen Schlüssel (`.pub` neben `ORDERPY_KEY_PATH`) in der Cloud/ beim Gerät hinterlegt, falls dein Setup das vorsieht.  
3. `ORDERPY_CLOUD_URL` vom Bridge-Host aus per `curl` erreichbar; WebSocket-Verbindung im Log ohne dauerhaften Fehler.  
4. `ORDERPY_ALLOWED_ORIGINS` enthält exakt die Origin(s) deiner Web-Oberfläche (Schema + Host + Port).

---

## Sicherheit

- Verzeichnis mit privatem Schlüssel (`*.pem` / `*.priv`) nur für den Bridge-Prozess lesbar halten (`chmod 600`).  
- `./data` bzw. `/var/lib/orderpy-bridge` nicht in öffentliche Backups ohne Verschlüsselung legen, wenn der Schlüssel produktiv ist.  
- Produktiv `ORDERPY_CLOUD_URL` auf **HTTPS** setzen, damit die abgeleitete WebSocket-Verbindung **WSS** nutzt.
