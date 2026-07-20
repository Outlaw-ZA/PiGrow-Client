package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Outlaw-ZA/PiGrow-Client/internal/config"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/controller"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/mqtt"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/provision"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/sensor"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration file")
	hardwarePath := flag.String("hardware", "hardware.yaml", "path to hardware.yaml (optional; missing = empty manifest)")
	serialPath := flag.String("serial", "serial.txt", "path to the persistent device serial file")
	statePath := flag.String("state", "state.json", "path to the active state.json (written after a successful claim)")
	fwVersion := flag.String("fw-version", "0.4.0", "PiGrow-Client firmware version surfaced in the beacon and UI")
	unclaimedFlag := flag.Bool("unclaimed", false, "force unclaimed boot (run provisioning even if state.json exists)")
	resetClaim := flag.Bool("reset-claim", false, "delete state.json and enter unclaimed boot (re-provision)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if *resetClaim {
		if err := os.Remove(*statePath); err != nil && !os.IsNotExist(err) {
			slog.Error("Reset failed", "path", *statePath, "error", err)
			os.Exit(1)
		}
		slog.Info("state.json removed", "path", *statePath)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}
	slog.Info("Config loaded", "path", *configPath)

	if hasHardwareSensors(cfg) {
		if err := sensor.InitHost(); err != nil {
			slog.Error("Failed to initialise periph host", "error", err)
			os.Exit(1)
		}
		slog.Info("Periph host initialised")
	}

	// Active state (if any) overrides legacy YAML fields per spec §5.
	st, err := provision.LoadActiveState(*statePath)
	if err != nil {
		slog.Error("Failed to load state", "error", err)
		os.Exit(1)
	}
	if st != nil {
		slog.Info("Active state loaded", "controllerId", st.ControllerID, "server", st.ServerHTTPURL)
	}
	cfg = provision.ResolveActiveConfig(cfg, st)

	enterUnclaimed := *unclaimedFlag || st == nil || !st.IsClaimed()

	if enterUnclaimed {
		runUnclaimedFlow(*configPath, *hardwarePath, *serialPath, *statePath, *fwVersion)
		return
	}

	runActiveFlow(cfg, st.MQTTBrokerURL, st.MQTTUsername, st.MQTTPassword)
}

func runUnclaimedFlow(configPath, hardwarePath, serialPath, statePath, fwVersion string) {
	slog.Info("Booting in unclaimed mode — beaconing for claim", "fwVersion", fwVersion)

	// The claim handshake needs a real broker URL (state.json's
	// broker is empty before the first claim). Read the legacy YAML
	// here purely to discover the broker address.
	yamlCfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("Cannot re-read config for broker URL", "error", err)
		os.Exit(1)
	}

	transport, err := provision.DialClaimTransport(yamlCfg.MQTT.Broker, yamlCfg.MQTT.ClientID)
	if err != nil {
		slog.Error("Failed to dial claim MQTT transport", "error", err)
		os.Exit(1)
	}
	defer transport.Client.Disconnect(250)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state, err := provision.RunUnclaimed(ctx, provision.RunOptions{
		FwVersion:    fwVersion,
		SerialPath:   serialPath,
		HardwarePath: hardwarePath,
		StatePath:    statePath,
		ClaimMQTT:    transport,
	})
	if err != nil {
		slog.Error("Provisioning did not complete", "error", err)
		os.Exit(1)
	}
	slog.Info("Provisioning complete — switching to active mode", "controllerId", state.ControllerID)

	// Re-read state.json + apply precedence so the active loop
	// starts with the claimed controllerId and broker credentials.
	// periph host was already initialised in main() at boot — no
	// need to retry here, and retrying it is wasteful even if safe.
	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("Config reload failed", "error", err)
		os.Exit(1)
	}
	st, err := provision.LoadActiveState(statePath)
	if err != nil {
		slog.Error("State reload failed", "error", err)
		os.Exit(1)
	}
	cfg = provision.ResolveActiveConfig(cfg, st)
	runActiveFlow(cfg, state.MQTTBrokerURL, state.MQTTUsername, state.MQTTPassword)
}

func runActiveFlow(cfg *config.Config, claimBrokerURL, claimUsername, claimPassword string) {
	// Active-mode MQTT connection: prefer claim-supplied broker +
	// credentials when present, fall back to the legacy YAML. v1
	// keeps the broker anonymous; fields are stored for the Phase-2
	// broker-config flip without code changes.
	statusTopic := fmt.Sprintf("pigrow-client/%s/status", cfg.MQTT.ClientID)
	broker := cfg.MQTT.Broker
	if claimBrokerURL != "" {
		broker = claimBrokerURL
	}
	username := cfg.MQTT.Username
	password := cfg.MQTT.Password
	if claimUsername != "" {
		username = claimUsername
	}
	if claimPassword != "" {
		password = claimPassword
	}

	mqttCfg := mqtt.Config{
		Broker:         broker,
		ClientID:       cfg.MQTT.ClientID,
		ConnectTimeout: time.Duration(cfg.MQTT.ConnectTimeout) * time.Second,
		KeepAlive:      time.Duration(cfg.MQTT.KeepAlive) * time.Second,
		Username:       username,
		Password:       password,
		StatusTopic:    statusTopic,
	}
	mc, err := mqtt.New(mqttCfg)
	if err != nil {
		slog.Error("Failed to connect to MQTT broker", "error", err)
		os.Exit(1)
	}
	defer mc.Disconnect()

	sensors, err := buildSensors(cfg)
	if err != nil {
		slog.Error("Failed to create sensors", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl := controller.New(cfg, mc, sensors)

	var wg sync.WaitGroup
	go ctrl.Start(ctx, &wg)

	if cfg.Server.HTTPURL != "" && cfg.Server.ControllerID != "" {
		wg.Add(1)
		go heartbeatLoop(ctx, &wg, cfg.Server.HTTPURL, cfg.Server.ControllerID, cfg.MQTT.ClientID)
	} else {
		slog.Info("Heartbeat disabled — controllerId or server URL unset")
	}

	slog.Info("PiGrow-Client started (active)")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Info("Shutting down", "signal", s.String())

	cancel()
	wg.Wait()
}

// heartbeatLoop periodically calls the server heartbeat endpoint so the
// server knows this Pi controller is still alive.
func heartbeatLoop(ctx context.Context, wg *sync.WaitGroup, baseURL, controllerID, _ string) {
	defer wg.Done()
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("%s/controllers/%s/heartbeat", baseURL, controllerID)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			body, _ := json.Marshal(map[string]string{"status": "ONLINE"})
			req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url,
				bytes.NewReader(body))
			if err != nil {
				slog.Warn("Heartbeat request creation failed", "error", err)
				continue
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				slog.Warn("Heartbeat failed", "error", err)
				continue
			}
			resp.Body.Close()
			slog.Debug("Heartbeat sent", "controllerId", controllerID)
		}
	}
}

func hasHardwareSensors(cfg *config.Config) bool {
	for _, s := range cfg.Sensors {
		if s.Type == "TEMP_HUMIDITY" {
			return true
		}
	}
	return false
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
