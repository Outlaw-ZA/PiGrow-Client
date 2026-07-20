package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Outlaw-ZA/PiGrow-Client/internal/config"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/device"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/mqtt"
	"github.com/Outlaw-ZA/PiGrow-Client/internal/sensor"
)

// CommandPayload is the JSON body received on a device commands topic.
type CommandPayload struct {
	Action    string `json:"action"`
	Pin       int    `json:"pin"`
	Timestamp int64  `json:"timestamp"`
}

// StatePayload is the JSON body published to a device state topic.
type StatePayload struct {
	Action    string `json:"action"`
	Timestamp int64  `json:"timestamp"`
}

// cmdJob carries a validated device command ready for execution.
type cmdJob struct {
	deviceID  string
	action    string
	pin       int
	timestamp int64
}

type Controller struct {
	cfg        *config.Config
	mqttClient mqtt.Client
	sensors    []sensor.Sensor
	deviceMap  map[string]device.Device
	mu         sync.Mutex
	cmdCh      chan cmdJob
}

// New creates a Controller.
func New(cfg *config.Config, mc mqtt.Client, sensors []sensor.Sensor) *Controller {
	return &Controller{
		cfg:        cfg,
		mqttClient: mc,
		sensors:    sensors,
		deviceMap:  make(map[string]device.Device),
		cmdCh:      make(chan cmdJob, 64),
	}
}

// Start begins sensor telemetry goroutines and subscribes to device commands.
// Each spawned goroutine is tracked via wg. It returns when ctx is cancelled.
func (c *Controller) Start(ctx context.Context, wg *sync.WaitGroup) {
	for _, s := range c.sensors {
		wg.Add(1)
		go c.sensorLoop(ctx, wg, s)
	}

	if err := c.mqttClient.Subscribe("devices/+/commands", c.deviceCommandHandler); err != nil {
		slog.Error("Failed to subscribe to device commands", "error", err)
	}
	wg.Add(1)
	go c.commandWorker(ctx, wg)

	<-ctx.Done()
}

func (c *Controller) sensorLoop(ctx context.Context, wg *sync.WaitGroup, s sensor.Sensor) {
	defer wg.Done()

	ticker := time.NewTicker(s.Interval())
	defer ticker.Stop()

	slog.Info("Sensor loop started", "id", s.ID(), "interval", s.Interval())

	for {
		c.publishTelemetry(s)

		select {
		case <-ctx.Done():
			slog.Info("Sensor loop stopped", "id", s.ID())
			return
		case <-ticker.C:
		}
	}
}

func (c *Controller) publishTelemetry(s sensor.Sensor) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in telemetry publish", "id", s.ID(), "panic", r)
		}
	}()

	readings, err := s.Read()
	if err != nil {
		slog.Error("Sensor read failed", "id", s.ID(), "error", err)
		return
	}

	payload := struct {
		Readings []sensor.Reading `json:"readings"`
	}{Readings: readings}

	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Marshal telemetry failed", "id", s.ID(), "error", err)
		return
	}

	topic := fmt.Sprintf("sensors/%s/telemetry", s.ID())
	slog.Debug("Publishing telemetry", "topic", topic)
	if err := c.mqttClient.PublishQoS0(topic, data); err != nil {
		slog.Error("Publish telemetry failed", "id", s.ID(), "error", err)
	}
}

func (c *Controller) deviceCommandHandler(topic string, payload []byte) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in device command handler", "panic", r, "topic", topic)
		}
	}()

	slog.Debug("Device command received", "topic", topic)

	// Extract deviceId from topic: "devices/<deviceId>/commands"
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		slog.Warn("Malformed command topic", "topic", topic)
		return
	}
	deviceID := parts[1]

	c.mu.Lock()
	_, exists := c.deviceMap[deviceID]
	if !exists {
		c.deviceMap[deviceID] = device.NewRPIDevice(deviceID)
		slog.Info("Lazy-created device from command", "deviceId", deviceID)
	}
	c.mu.Unlock()

	var cmd CommandPayload
	if err := json.Unmarshal(payload, &cmd); err != nil {
		slog.Error("Parse command payload failed", "deviceId", deviceID, "error", err)
		return
	}

	slog.Info("Dispatching command", "deviceId", deviceID, "action", cmd.Action, "pin", cmd.Pin)

	select {
	case c.cmdCh <- cmdJob{deviceID, cmd.Action, cmd.Pin, cmd.Timestamp}:
	default:
		slog.Warn("Command channel full, dropping", "deviceId", deviceID)
	}
}

func (c *Controller) commandWorker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-c.cmdCh:
			c.executeCommand(job)
		}
	}
}

func (c *Controller) executeCommand(job cmdJob) {
	// Lookup must be locked: deviceCommandHandler writes to the same
	// map from the MQTT callback goroutine while this worker reads it.
	c.mu.Lock()
	dev, ok := c.deviceMap[job.deviceID]
	c.mu.Unlock()
	if !ok {
		slog.Warn("Device gone before command execution", "deviceId", job.deviceID)
		return
	}

	var execErr error
	switch strings.ToUpper(job.action) {
	case "ON":
		execErr = dev.On(job.pin)
	case "OFF":
		execErr = dev.Off(job.pin)
	default:
		slog.Warn("Unknown command action", "action", job.action)
		return
	}

	if execErr != nil {
		slog.Error("Command execution failed", "deviceId", job.deviceID, "error", execErr)
		return
	}

	c.publishState(job.deviceID, job.action, job.timestamp)
}

func (c *Controller) publishState(deviceID, action string, timestamp int64) {
	state := StatePayload{
		Action:    action,
		Timestamp: timestamp,
	}
	data, err := json.Marshal(state)
	if err != nil {
		slog.Error("Marshal state payload failed", "deviceId", deviceID, "error", err)
		return
	}

	topic := fmt.Sprintf("devices/%s/state", deviceID)
	slog.Debug("Publishing device state", "deviceId", deviceID, "action", action)
	if err := c.mqttClient.Publish(topic, data); err != nil {
		slog.Error("Publish state failed", "deviceId", deviceID, "error", err)
	}
}
