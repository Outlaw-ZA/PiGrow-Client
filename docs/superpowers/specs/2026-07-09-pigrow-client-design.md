# PiGrow-Client Design

Go client for Raspberry Pi that reads SHT40 sensors and controls GPIO relays via MQTT, matching the PiGrow-Server protocol.

## Architecture

```
cmd/pigrow-client/main.go         entry point, config load, signal handling
internal/
  config/config.go                 YAML parsing (gopkg.in/yaml.v3)
  mqtt/client.go                   MQTT connect/reconnect/publish/subscribe
  sensor/sensor.go                 Sensor interface
  sensor/sht40.go                  SHT40 I2C driver
  device/device.go                 GPIO interface
  device/rpi.go                    Raspberry Pi GPIO via sysfs
  controller/controller.go         spawns sensor+device goroutines, orchestrates
config.yaml                        example config
pigrow-client.service              systemd unit
Makefile                           build + cross-compile targets
```

## MQTT Protocol (matching PiGrow-Server)

| Direction | Topic | Payload |
|-----------|-------|---------|
| Client→Server | `sensors/<sensorId>/telemetry` | `{ "readings": [{ "sensorType": "TEMPERATURE", "value": 25.5 }] }` |
| Client→Server | `devices/<deviceId>/state` | `{ "action": "ON", "timestamp": 1712345678000 }` |
| Server→Client | `devices/<deviceId>/commands` | `{ "action": "ON", "pin": 17, "timestamp": 1712345678000 }` |

SensorType values: `TEMPERATURE`, `HUMIDITY`, `TEMP_HUMIDITY`.

## Configuration

Single YAML file with broker settings, sensor list (UUID, I2C bus/address, interval), and device list (UUID). GPIO pin comes from server command.
