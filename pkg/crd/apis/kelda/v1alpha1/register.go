package v1alpha1

import (
	"github.com/sidkik/kelda-v1/pkg/crd/apis/kelda"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var tb bool = true

// SchemeGroupVersion is group version used to register these objects.
var SchemeGroupVersion = schema.GroupVersion{Group: kelda.GroupName, Version: "v1alpha1"}

// MicroserviceSchemaVersion is the v1 definition for version for microservice
var MicroserviceSchemaVersion apiextensionsv1.CustomResourceDefinitionVersion = apiextensionsv1.CustomResourceDefinitionVersion{
	Name:    "v1alpha1",
	Served:  true,
	Storage: true,
	Schema: &apiextensionsv1.CustomResourceValidation{
		OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"devStatus": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"Pod": {
							Type: "string",
						},
						"TargetVersion": {
							Type: "string",
						},
						"RunningVersion": {
							Type: "string",
						},
					},
				},
				"status": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"metaStatus": {
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"Phase": {
									Type: "string",
								},
								"Message": {
									Type: "string",
								},
							},
						},
						"serviceStatus": {
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"Phase": {
									Type: "string",
								},
								"Message": {
									Type: "string",
								},
							},
						},
						"jobStatus": {
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"Phase": {
									Type: "string",
								},
								"Message": {
									Type: "string",
								},
							},
						},
						"actual": {
							Type: "array",
							Items: &apiextensionsv1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1.JSONSchemaProps{
									XPreserveUnknownFields: &tb,
									Type:                   "object",
								},
							},
						},
					},
				},
				"specVersion": {
					Type: "integer",
				},
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"devMode": {
							Type: "boolean",
						},
						"devImage": {
							Type: "string",
						},
						"manifests": {
							Type: "array",
							Items: &apiextensionsv1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"},
							},
						},
						"hasService": {
							Type: "boolean",
						},
						"hasJob": {
							Type: "boolean",
						},
						"imageDigests": {
							Type: "array",
							Items: &apiextensionsv1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"ControllerName": {
											Type: "string",
										},
										"ContainerName": {
											Type: "string",
										},
										"Digest": {
											Type: "string",
										},
										"ImageURL": {
											Type: "string",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	},
}

// TunnelSchemaVersion is the v1 definition for version for tunnel
var TunnelSchemaVersion apiextensionsv1.CustomResourceDefinitionVersion = apiextensionsv1.CustomResourceDefinitionVersion{
	Name:    "v1alpha1",
	Served:  true,
	Storage: true,
	Schema: &apiextensionsv1.CustomResourceValidation{
		OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"service": {
							Type: "string",
						},
						"localPort": {
							Type: "integer",
						},
						"remotePort": {
							Type: "integer",
						},
					},
				},
				"status": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"Phase": {
							Type: "string",
						},
						"Message": {
							Type: "string",
						},
					},
				},
			},
		},
	},
}

// Kind takes an unqualified kind and returns back a Group qualified GroupKind.
func Kind(kind string) schema.GroupKind {
	return SchemeGroupVersion.WithKind(kind).GroupKind()
}

// Resource takes an unqualified resource and returns a Group qualified GroupResource.
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

var (
	// SchemeBuilder collects functions to add to the scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	// AddToScheme applies all the stored functions to the scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// Adds the list of known types to Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&Microservice{},
		&MicroserviceList{},
		&Tunnel{},
		&TunnelList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
