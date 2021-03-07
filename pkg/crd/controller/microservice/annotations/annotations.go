package annotations

const (
	// MicroserviceName is the annotation used to identify the name of
	// the Microservice that created an object.
	MicroserviceName = "kelda.io.minion.microservice"

	// MicroserviceVersion is the annotation used to identify the
	// version of the Microservice that created an object.
	MicroserviceVersion = "kelda.io.minion.microserviceSpecVersion"

	// DeprecatedMicroserviceName is the annotation that we used to use for
	// tracking the microservice name. It's used as a fallback for interacting
	// with CRDs deployed by previous versions of Kelda.
	DeprecatedMicroserviceName = "kelda.io.microservice"

	// DeprecatedMicroserviceVersion is the annotation that we used to use for
	// tracking the microservice version. It's used as a fallback for interacting
	// with CRDs deployed by previous versions of Kelda.
	DeprecatedMicroserviceVersion = "kelda.io.microserviceSpecVersion"

	// KeldaVersion tracks the version of Kelda that created the object.
	KeldaVersion = "kelda.io.minion.keldaVersion"

	// PriorityClass is the key for the annotation that describes
	// which PriorityClass a pod should use.
	PriorityClass = "kelda.io.minion.microservicePriorityClass"
)
