package kube

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kelda-inc/kelda/pkg/crd/controller/microservice/annotations"
)

func TestMakeTree(t *testing.T) {
	versionOneAnnotation := map[string]string{annotations.MicroserviceVersion: "1"}
	versionTwoAnnotation := map[string]string{annotations.MicroserviceVersion: "2"}

	blueMS := types.UID("blueMS")
	blueDeployment, blueDeploymentUID := newObject("Deployment", "blueDeployment",
		versionOneAnnotation, metav1.OwnerReference{Kind: "Microservice", UID: blueMS})
	blueReplicaSet, blueReplicaSetUID := newObject("ReplicaSet", "blueReplicaSet",
		nil, metav1.OwnerReference{Kind: "Deployment", UID: blueDeploymentUID})
	bluePodOne, _ := newObject("Pod", "bluePodOne",
		nil, metav1.OwnerReference{Kind: "ReplicaSet", UID: blueReplicaSetUID})
	bluePodTwo, _ := newObject("Pod", "bluePodTwo",
		nil, metav1.OwnerReference{Kind: "ReplicaSet", UID: blueReplicaSetUID})

	redMS := types.UID("redMS")
	redDeployment, redDeploymentUID := newObject("Deployment", "redDeployment",
		versionTwoAnnotation, metav1.OwnerReference{Kind: "Microservice", UID: redMS})
	redReplicaSet, redReplicaSetUID := newObject("ReplicaSet", "redReplicaSet",
		nil, metav1.OwnerReference{Kind: "Deployment", UID: redDeploymentUID})
	redPod, _ := newObject("Pod", "redPod",
		nil, metav1.OwnerReference{Kind: "ReplicaSet", UID: redReplicaSetUID})

	job, jobUID := newObject("Job", "job", versionTwoAnnotation,
		metav1.OwnerReference{Kind: "Microservice", UID: redMS})
	jobPod, _ := newObject("Pod", "jobPod", nil,
		metav1.OwnerReference{Kind: "Job", UID: jobUID})

	tests := []struct {
		in  []runtime.Object
		exp map[microserviceKey][]*Object
	}{
		{
			in: []runtime.Object{
				blueDeployment, blueReplicaSet, bluePodOne, bluePodTwo,
				redDeployment, redReplicaSet, redPod,
				job, jobPod,
			},
			exp: map[microserviceKey][]*Object{
				{blueMS, 1}: {{
					Object: blueDeployment,
					Children: []*Object{{
						Object: blueReplicaSet,
						Children: []*Object{
							{Object: bluePodOne},
							{Object: bluePodTwo},
						},
					}},
				}},
				{redMS, 2}: {
					{
						Object: redDeployment,
						Children: []*Object{{
							Object: redReplicaSet,
							Children: []*Object{
								{Object: redPod},
							},
						}},
					},
					{
						Object: job,
						Children: []*Object{{
							Object: jobPod,
						}},
					},
				},
			},
		},
	}

	for _, test := range tests {
		tree := makeTree(test.in)
		assert.Equal(t, test.exp, tree, "Trees should be the same.")
	}
}

func newObject(kind, name string, annotations map[string]string, owners ...metav1.OwnerReference) (
	runtime.Object, types.UID) {

	uid := types.UID(name)
	objectMeta := metav1.ObjectMeta{
		Annotations:     annotations,
		Name:            name,
		OwnerReferences: owners,
		UID:             uid,
	}

	typeMeta := metav1.TypeMeta{
		Kind: kind,
	}

	switch kind {
	case "Pod":
		return &corev1.Pod{ObjectMeta: objectMeta, TypeMeta: typeMeta}, uid
	case "Deployment":
		return &appsv1.Deployment{ObjectMeta: objectMeta, TypeMeta: typeMeta}, uid
	case "ReplicaSet":
		return &appsv1.ReplicaSet{ObjectMeta: objectMeta, TypeMeta: typeMeta}, uid
	case "Job":
		return &batchv1.Job{ObjectMeta: objectMeta, TypeMeta: typeMeta}, uid
	default:
		panic(fmt.Sprintf("unrecognized kind: %s", kind))
	}
}
