package provision

import "github.com/Outlaw-ZA/PiGrow-Client/internal/config"

// ResolveActiveConfig returns the active-mode configuration: state.json
// fields (controllerId, server URL, broker URL+credentials) override
// legacy config.yaml when a claim has succeeded; otherwise the legacy
// config.yaml is returned untouched.
//
// Sensors and devices are NOT overridden — spec §2.2 says the server
// auto-creates Sensor/Device rows from the hwManifest, but in v1 the
// Pi still reads its own sensors from the YAMLs it ships with. State
// does not replace the sensor list.
func ResolveActiveConfig(yamlCfg *config.Config, st *ActiveState) *config.Config {
	if yamlCfg == nil {
		return nil
	}
	if st == nil || !st.IsClaimed() {
		return yamlCfg
	}
	out := *yamlCfg
	out.Server.HTTPURL = st.ServerHTTPURL
	out.Server.ControllerID = st.ControllerID
	if st.MQTTBrokerURL != "" {
		out.MQTT.Broker = st.MQTTBrokerURL
	}
	if st.MQTTUsername != "" {
		out.MQTT.Username = st.MQTTUsername
	}
	if st.MQTTPassword != "" {
		out.MQTT.Password = st.MQTTPassword
	}
	return &out
}
