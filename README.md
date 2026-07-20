# PiGrow-Client

Go client that runs on a Raspberry Pi controller. Talks to a
[PiGrow-Server](https://github.com/Outlaw-ZA/PiGrow-Server) over MQTT and
HTTP. Auto-discovers the server, advertises itself over the LAN, and
zero-touch-claims via a 6-digit PIN.

The server owns all logic (thresholds, schedules, automation); the client
reads sensors, publishes telemetry, executes device commands, and reports
a heartbeat.

---

## Two modes

| Mode      | Trigger                                                | Behavior                                                       |
| --------- | ------------------------------------------------------ | -------------------------------------------------------------- |
| unclaimed | no `state.json` on disk, or `--unclaimed` / `--reset-claim` flag | advertises itself over UDP 9999 + mDNS `_pigrow._tcp.local`, waits for a `ClaimResponse` over MQTT topic `provision/<mac>/claim`, writes `state.json`, restarts in active mode |
| active    | `state.json` exists (or legacy `config.yaml` has a `controllerId`) | heartbeat, telemetry, device commands — same as before    |

The client always **prefers `state.json`** over `config.yaml`. A legacy Pi
with only a manually-edited `config.yaml` keeps working — no migration
required.

---

## Build

```sh
make build          # local arch
make build-arm      # ARMv7 (Pi 3 / Zero 2)
make build-arm64    # ARM64 (Pi 4 / 5)
```

Binary: `bin/pigrow-client-arm64`.

## Configure

Two files, both optional. The client boots and figures out which to use.

### `config.yaml` (legacy / advanced)

Only needed if you want manual control **before** claiming — the server
still supports the old pre-provisioning flow.

```yaml
mqtt:
  broker: "tcp://192.168.1.10:1883"
  client_id: "pigrow-tent1"

# server heartbeat — optional. After claiming, this is auto-populated
# from the ClaimResponse into state.json.
server:
  http_url: "http://192.168.1.10:4000/api"
  controller_id: "<uuid>"

sensors:
  - id: "sensor-uuid"
    type: "TEMP_HUMIDITY"
    i2c_bus: "/dev/i2c-1"
    i2c_address: 0x44
    interval: "30s"
```

### `hardware.yaml` (recommended)

Drives **automatic Sensor and Device creation** on the server at claim
time — you no longer need to copy UUIDs back and forth.

```yaml
sensors:
  - type: BME280
    protocol: I2C
    i2c_bus: 1
    i2c_addr: 118        # 0x76
    interval: 30

relays:
  - type: LIGHT
    pin: 17
    name: Main Light     # optional; server defaults to "LIGHT 17"
  - type: EXHAUST_FAN
    pin: 18
```

Supported protocols: `I2C` (`i2c_bus` + `i2c_addr`), `GPIO` / `ONEWIRE`
(`pin`). Empty file = no sensors/relays advertised; the client still
beacons and claims fine.

Pass its location with `--hardware /opt/pigrow-client/hardware.yaml`.

### State file (auto-managed)

After a successful claim, the client writes `state.json`:

```json
{
  "controllerId": "8e7c1f2a-...",
  "controllerMac": "AA:BB:CC:DD:EE:FF",
  "mqttBrokerUrl": "mqtt://192.168.1.10:1883",
  "mqttUsername": "pigrow-8e7c1f2a",
  "mqttPassword": "<persisted-for-phase-2-broker-Acl>",
  "serverHttpUrl": "http://192.168.1.10:4000",
  "provisionState": "ACTIVE",
  "pairedAt": 1737000000000
}
```

Don't edit this by hand. Delete it (`rm /opt/pigrow-client/state.json`) to
re-enter unclaimed mode and re-pair (e.g., to a different server), or pass
`--reset-claim` at startup.

## Command-line flags

| Flag               | Default                     | Purpose                                                       |
| ------------------ | --------------------------- | ------------------------------------------------------------- |
| `-config`          | `config.yaml`               | Legacy config file path                                       |
| `--hardware`       | `./hardware.yaml`           | Hardware manifest for auto-creation of Sensor/Device rows    |
| `--serial`         | (auto)                      | Override the persisted device serial                          |
| `--state`          | `./state.json`              | Override the state file path                                  |
| `--fw-version`     | build-time                  | Advertised in the beacon for UI visibility                   |
| `--unclaimed`      | (auto)                      | Force unclaimed mode (override state.json)                    |
| `--reset-claim`    | (off)                       | Delete state.json and run unclaimed                           |

The "auto" defaults boot unclaimed if no `state.json` exists, else active.

## Prerequisites on the Pi

```sh
# Enable I2C
sudo raspi-config nonint do_i2c 0

# Create the pigrow system user
sudo useradd --system --group pigrow
sudo usermod -a -G gpio,i2c pigrow
```

## Deploy

### New (zero-touch) install

```sh
scp bin/pigrow-client-arm64 pi@<pi-ip>:/tmp/
scp hardware.yaml            pi@<pi-ip>:/tmp/
ssh pi@<pi-ip> sudo /tmp/pigrow-client-arm64 --install   # convenience, or:
```

Or install manually:

```sh
ssh pi@<pi-ip> <<'EOF'
  sudo install -d -o pigrow -g pigrow -m 750 /opt/pigrow-client
  sudo install -m 750 /tmp/pigrow-client-arm64 /opt/pigrow-client/pigrow-client
  sudo install -m 640 /tmp/hardware.yaml            /opt/pigrow-client/hardware.yaml
  # link the systemd unit (see pigrow-client.service)
  sudo systemctl daemon-reload
  sudo systemctl enable --now pigrow-client
EOF
```

The client will boot in **unclaimed mode**, advertise itself, and wait for
you to click **Scan for Controllers** in the UI. See the top-level
[SETUP.md](../SETUP.md) for the full walkthrough.

### Legacy manual install

If you're running an existing deployment that predates auto-provisioning,
keep using your existing `config.yaml` + manual UUID flow. The client
honors `config.yaml` exactly as before when `state.json` is absent and
`server.controller_id` is set.

## Verify

On the Pi:

```sh
sudo journalctl -u pigrow-client -f
# Look for:
#   beacon started; PIN=123456; pinExpiresAt=...
#   discovery: advertising _pigrow._tcp.local with TXT pgpin=123456 ...
# Then later (after claim):
#   heartbeat: PATCH /api/controllers/<uuid>/heartbeat 200
```

The PIN rotates every 5 minutes. Use it in the UI's Scan for Controllers
within that window.

## Protocol reference

The full provisioning protocol is documented in
[PiGrow-Server/docs/provisioning-protocol.md](../PiGrow-Server/docs/provisioning-protocol.md).
Both PiGrow-Client and PiGrow-UI implement against that spec.

## Tests

```sh
go build ./...
go vet ./...
go test -race ./...
```

The `internal/provision/` package covers the beacon UDP round-trip, PIN
generation/rotation, ClaimResponse parsing, state.json atomicity, and the
beacon-goroutine shutdown on claim (regression test for the bug where the
beacon kept broadcasting after a successful claim).