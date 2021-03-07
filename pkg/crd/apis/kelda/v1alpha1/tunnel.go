package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Tunnel represents a tunnel that should be opened on the developer's local
// machine.
type Tunnel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TunnelSpec   `json:"spec"`
	Status TunnelStatus `json:"status"`
}

// TunnelSpec defines a desired tunnel between a local port and a service in
// the development environment.
type TunnelSpec struct {
	Service    string `json:"service"`
	LocalPort  uint32 `json:"localPort"`
	RemotePort uint32 `json:"remotePort"`
}

// TunnelStatus provides information about the status of a tunnel.
type TunnelStatus struct {
	Phase TunnelPhase

	// A message giving more information on the Phase. Only defined for Crashed
	// tunnels.
	Message string
}

// TunnelPhase is indicates what phase the tunnel is in.
type TunnelPhase string

const (
	// TunnelStarting indicates a tunnel is starting.
	TunnelStarting TunnelPhase = "Starting"
	// TunnelUp indicates the tunnel is up.
	TunnelUp TunnelPhase = "Up"
	// TunnelCrashed indicates the tunnel has crashed.
	TunnelCrashed TunnelPhase = "Crashed"
)

// TunnelList is a list of tunnels required by the application.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TunnelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Tunnel `json:"items"`
}
