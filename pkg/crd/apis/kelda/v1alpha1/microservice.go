package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kelda-inc/kelda/pkg/kube"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Microservice represents a Microservice as declared by the user. It can
// contain multiple Kubernetes objects (e.g. a Deployment and a Service).
type Microservice struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec        MicroserviceSpec   `json:"spec"`
	SpecVersion int                `json:"specVersion"`
	Status      MicroserviceStatus `json:"status"`
	DevStatus   DevStatus          `json:"devStatus"`
}

// MicroserviceSpec defines the Kubernetes manifests required to boot the
// Microservice.
type MicroserviceSpec struct {
	DevMode      bool          `json:"devMode"`
	DevImage     string        `json:"devImage"`
	Manifests    []string      `json:"manifests"`
	HasService   bool          `json:"hasService"`
	HasJob       bool          `json:"hasJob"`
	ImageDigests []ImageDigest `json:"imageDigests"`
}

// MicroserviceStatus represents the status of the Kubernetes objects created
// by the Microservice controller.
type MicroserviceStatus struct {
	MetaStatus    MetaStatus     `json:"metaStatus"`
	ServiceStatus ServiceStatus  `json:"serviceStatus"`
	JobStatus     JobStatus      `json:"jobStatus"`
	Actual        []*kube.Object `json:"actual"`
}

// DevStatus represents the status that is updated by the sync service.
type DevStatus struct {
	Pod            string
	TargetVersion  string
	RunningVersion string
}

// MetaStatus represents the overall sync status of the service as a whole.
type MetaStatus struct {
	Phase   MetaPhase
	Message string
}

// ServiceStatus represents the status of long-running services in the microservice.
type ServiceStatus struct {
	Phase   ServicePhase
	Message string
}

// JobStatus represents the status of one-off jobs in the microservice.
type JobStatus struct {
	Phase   JobPhase
	Message string
}

// ImageDigest holds a unique identifier within a microservice of a container
// and the digest of the image it should run. The digest specified here will be
// injected to the image URL in the Kubernetes object by the controller.
type ImageDigest struct {
	// ControllerName is the name in the ObjectMeta of an object.
	ControllerName string

	// ContainerName is the name of the container.
	ContainerName string

	// Digest is the desired digest of the image to be run in the container.
	Digest string

	// ImageURL is the image that the digest is for.
	ImageURL string
}

// MetaPhase is a displayed status that applies to both jobs and services.
type MetaPhase string

// These are the valid MetaPhases.
const (
	// MetaSyncing means that the microservice is in development mode, and is in
	// the process of updating to the latest version of the developer's local
	// code.
	MetaSyncing MetaPhase = "Syncing"

	// MetaSynced means that the microservice is in development mode, and is
	// running the latest version of the developer's local code.
	MetaSynced MetaPhase = "Synced"

	// MetaStatusSyncFailed means that some error occurred while syncing the
	// status of the Microservice, children.
	MetaStatusSyncFailed MetaPhase = "Status Sync Failed"

	// MetaDeployFailed means that some error occurred while deploying the
	// manifests for the Microservice.
	MetaDeployFailed MetaPhase = "Deploy Failed"
)

// ServicePhase is the displayed status of the services in a Microservice.
type ServicePhase string

// These are the valid service statuses.
const (
	// ServiceStarting means that Kubernetes is still starting the containers for
	// a service.
	ServiceStarting ServicePhase = "Starting"

	// ServiceFailed means that the service is not running, and exited in failure.
	ServiceFailed ServicePhase = "Failed"

	// ServiceNotReady means that the service has been started, but readiness
	// checks are still failing.
	ServiceNotReady ServicePhase = "Not Ready"

	// ServiceReady means that the service has been started and readiness checks
	// are passing if they exist.
	ServiceReady ServicePhase = "Ready"

	// ServiceUnknown means that the status of the service is unknown.
	ServiceUnknown ServicePhase = "Unknown"
)

// JobPhase is the displayed status of the jobs in a Microservice.
type JobPhase string

// These are the valid job statuses.
const (
	// JobStarting means that Kubernetes is still starting the containers for
	// a job.
	JobStarting JobPhase = "Starting"

	// JobRunning means that the job has not failed, and has active pods.
	JobRunning JobPhase = "Running"

	// JobFailed means that the job is not running, and exited in failure.
	JobFailed JobPhase = "Failed"

	// JobUnknown means that the status of the job is unknown.
	JobUnknown JobPhase = "Unknown"

	// JobCompleted means that the job has successfully completed.
	JobCompleted JobPhase = "Completed"
)

// MicroserviceList is a list of microservices.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MicroserviceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Microservice `json:"items"`
}
