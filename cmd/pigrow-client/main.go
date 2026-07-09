package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Outlaw-ZA/PiGrow-Client/internal/config"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/controller"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/device"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/mqtt"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/sensor"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}
	slog.Info("Config loaded", "path", *configPath)

	// Initialise periph host for I2C and GPIO.
	if hasHardwareSensors(cfg) || hasHardwareDevices(cfg) {
		if err := sensor.InitHost(); err != nil {
			slog.Error("Failed to initialise periph host", "error", err)
			os.Exit(1)
		}
		slog.Info("Periph host initialised")
	}

	// MQTT connect.
	mqttCfg := mqtt.Config{
		Broker:         cfg.MQTT.Broker,
		ClientID:       cfg.MQTT.ClientID,
		ConnectTimeout: time.Duration(cfg.MQTT.ConnectTimeout) * time.Second,
		KeepAlive:      time.Duration(cfg.MQTT.KeepAlive) * time.Second,
		Username:       cfg.MQTT.Username,
		Password:       cfg.MQTT.Password,
	}
	mc, err := mqtt.New(mqttCfg)
	if err != nil {
		slog.Error("Failed to connect to MQTT broker", "error", err)
		os.Exit(1)
	}
	defer mc.Disconnect()

	// Build sensors.
	sensors, err := buildSensors(cfg)
	if err != nil {
		slog.Error("Failed to create sensors", "error", err)
		os.Exit(1)
	}

	// Build devices.
	devices := buildDevices(cfg)
	defer func() {
		for _, d := range devices {
			if err := d.Close(); err != nil {
				slog.Error("Device close failed", "id", d.ID(), "error", err)
			}
		}
	}()

	// Start controller.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl := controller.New(cfg, mc, sensors, devices)

	var wg sync.WaitGroup
	go ctrl.Start(ctx, &wg)

	slog.Info("PiGrow-Client started")

	// Wait for shutdown signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Info("Shutting down", "signal", s.String())

	cancel()
	wg.Wait()
}

func hasHardwareSensors(cfg *config.Config) bool {
	for _, s := range cfg.Sensors {
		if s.Type == "TEMP_HUMIDITY" {
			return true
		}
	}
	return false
}

func hasHardwareDevices(cfg *config.Config) bool {
	return len(cfg.Devices) > 0
}

func buildSensors(cfg *config.Config) ([]sensor.Sensor, error) {
	var sensors []sensor.Sensor
	for _, sc := range cfg.Sensors {
		switch sc.Type {
		case "TEMP_HUMIDITY":
			s, err := sensor.NewSHT40(sc.ID, sc.I2CBus, sc.I2CAddress, sc.ParseInterval())
			if err != nil {
				return nil, err
			}
			sensors = append(sensors, s)
		default:
			slog.Warn("Unknown sensor type, skipping", "type", sc.Type, "id", sc.ID)
		}
	}
	return sensors, nil
}

func buildDevices(cfg *config.Config) []device.Device {
	var devices []device.Device
	for _, dc := range cfg.Devices {
		d := device.NewRPIDevice(dc.ID)
		devices = append(devices, d)
	}
	return devices
}
