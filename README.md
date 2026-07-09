# PiGrow-Client

Go client for Raspberry Pi. Reads SHT40 temperature/humidity sensors over I2C and controls GPIO-connected relays via MQTT.

Talks to a [PiGrow-Server](https://github.com/Outlaw-ZA/PiGrow-Server) backend — the server owns all logic (thresholds, schedules), the client only publishes telemetry and executes device commands.

## Build

```sh
make build          # local arch
make build-arm      # ARMv7 (Pi 3/Zero 2)
make build-arm64    # ARM64 (Pi 4/5)
```

## Configure

Copy `config.yaml` and fill in the server-assigned UUIDs:

```yaml
mqtt:
  broker: "tcp://<server-ip>:1883"
  client_id: "pigrow-tent1"

sensors:
  - id: "sensor-uuid"
    type: "TEMP_HUMIDITY"
    i2c_bus: "/dev/i2c-1"
    i2c_address: 0x44
    interval: "30s"

devices:
  - id: "device-uuid"
    type: "LIGHT"
```

## Prerequisites

Before deploying, run these commands once on the Pi:

```sh
# Enable I2C
sudo raspi-config nonint do_i2c 0

# Create the pigrow system user
sudo useradd --system --group pigrow

# Grant hardware access
sudo usermod -a -G gpio,i2c pigrow

# Secure the config file
sudo chmod 640 /opt/pigrow-client/config.yaml
sudo chown root:pigrow /opt/pigrow-client/config.yaml
```

## Deploy

```sh
scp bin/pigrow-client-arm64 pigrow-client.service config.yaml pi@<pi-ip>:/opt/pigrow-client/
ssh pi@<pi-ip> sudo systemctl enable --now /opt/pigrow-client/pigrow-client.service
```

Requires I2C enabled (`raspi-config` → Interface Options → I2C).
