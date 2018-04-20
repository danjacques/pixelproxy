package web

import (
	"time"
)

// Status is the structure returned by the status API endpoint.
type Status struct {
	Status  ControllerStatus `json:"status"`
	Devices []*DeviceInfo    `json:"devices,omitempty"`
}

// ControllerStatus provides the current state of the Controller.
type ControllerStatus struct {
	// If in Recording or Playing state, the file that is being operated on.
	Description string `json:"description,omitempty"`

	// StartTime is when the server started.
	StartTime time.Time `json:"start_time"`

	// Uptime is the amount of time this Controller has been running.
	Uptime time.Duration `json:"uptime"`

	// ProxyForwarding is true if the proxy manager is forwarding, false if it is
	// not (generally when we are recording).
	ProxyForwarding bool `json:"proxy_forwarding"`

	// DisabingeProxyForwarding is true if the Controller is currently blocking
	// proxy forwarding.
	DisablingProxyForwarding bool `json:"disabling_proxy_forwarding"`

	// PlaybackStatus, if not nil, is the status of the ongoing playback.
	PlaybackStatus *PlaybackStatus `json:"playback_status,omitempty"`

	// RecordStatus, if not nil, is the status of the ongoing recording.
	RecordStatus *RecordStatus `json:"record_status,omitempty"`
}

// DeviceInfo contains information for a proxy device.
type DeviceInfo struct {
	// Type is a type identification string for this device.
	Type string `json:"type"`
	// ID is the ID of this device.
	ID string `json:"id"`

	// ProxiedID is the ID of the device being proxied. If empty, this device
	// is not proxying for another device.
	ProxiedID string `json:"proxiedId,omitempty"`

	// Strips is the number of strips.
	Strips int `json:"strips,omitempty"`
	// Pixels is the number of LEDs per strip.
	Pixels int `json:"pixels,omitempty"`

	// Group is this device's group ID.
	Group int `json:"group,omitempty"`
	// Controller is this device's controller ID.
	Controller int `json:"controller,omitempty"`

	// Network is the network that this device is listening on.
	Network string `json:"network,omitempty"`
	// Address is the network address that this device is on.
	Address string `json:"address,omitempty"`

	// BytesSent is the number of bytes that have been sent.
	BytesSent int64 `json:"bytesSent,omitempty"`
	// PacketsSent is the number of packets that this device has sent.
	PacketsSent int64 `json:"packetsSent,omitempty"`

	// BytesReceived is the number of bytes that have been received.
	BytesReceived int64 `json:"bytesReceived,omitempty"`
	// PacketsReceived is the number of packets that this device has received.
	PacketsReceived int64 `json:"packetsReceived,omitempty"`

	// CreateTime is the time when this device was first observed/created.
	Created time.Time `json:"createTime,omitempty"`
	// LastObserved is the time when this device was last observed. If this is
	// not a discovered device, this may equal Created.
	LastObserved time.Time `json:"lastObserved,omitempty"`

	// HasSnapshot is true if this device has a snapshot available.
	HasSnapshot bool `json:"has_snapshot,omitempty"`
}

// ProxyInfo contains information for a proxy device.
type ProxyInfo struct {
	// ProxyID is the ID of the proxy device.
	ProxyID string `json:"proxyId"`
	// BaseID is the ID of the device being proxied.
	BaseID string `json:"baseId"`
}

// PlaybackStatus is a description of an ongoing playback operation.
type PlaybackStatus struct {
	Name          string        `json:"name"`
	Rounds        int64         `json:"rounds"`
	Position      time.Duration `json:"position"`
	Duration      time.Duration `json:"duration"`
	TotalPlaytime time.Duration `json:"total_playtime"`
	Progress      int           `json:"progress"`
	Paused        bool          `json:"paused"`

	NoRouteDevices []string `json:"no_route_devices,omitempty"`
}

// RecordStatus is a description of an ongoing record operation.
type RecordStatus struct {
	Name     string        `json:"name"`
	Error    string        `json:"error,omitempty"`
	Events   int64         `json:"events"`
	Bytes    int64         `json:"bytes"`
	Duration time.Duration `json:"duration"`
}

// SystemState is the state of the system controls.
type SystemState struct {
	Status string `json:"status"`
}
