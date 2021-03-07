package kube

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured/unstructuredscheme"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

// Parse parses the Kubernetes object in `doc` into an Unstructured object.
// It assumes that `doc` contains exactly one object. It is the caller's
// responsibility to split files with multiple documents into individual
// documents.
func Parse(doc []byte) (*unstructured.Unstructured, error) {
	decoder := json.NewYAMLSerializer(json.DefaultMetaFactory,
		unstructuredscheme.NewUnstructuredCreator(),
		unstructuredscheme.NewUnstructuredObjectTyper())
	obj, _, err := decoder.Decode(doc, nil, nil)
	if err != nil {
		return nil, err
	}

	return obj.(*unstructured.Unstructured), nil
}
