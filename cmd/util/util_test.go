package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/kelda-inc/kelda/pkg/kube"
)

func TestCountPodSpecs(t *testing.T) {
	tests := []struct {
		name string
		arg  []*kube.Object
		exp  int32
	}{
		{
			name: "Empty",
			exp:  0,
			arg:  nil,
		},
		{
			name: "ConfigMap",
			exp:  0,
			arg: []*kube.Object{
				{
					Object: &corev1.ConfigMap{},
				},
			},
		},
		{
			name: "OneReplica",
			exp:  1,
			arg: []*kube.Object{
				{
					Object: &appsv1.Deployment{
						Spec: appsv1.DeploymentSpec{
							Replicas: intPointer(1),
						},
					},
				},
			},
		},
		{
			name: "MultipleReplicas",
			exp:  2,
			arg: []*kube.Object{
				{
					Object: &appsv1.Deployment{
						Spec: appsv1.DeploymentSpec{
							Replicas: intPointer(2),
						},
					},
				},
			},
		},
		{
			name: "MixedControllers",
			exp:  4,
			arg: []*kube.Object{
				{
					Object: &appsv1.Deployment{
						Spec: appsv1.DeploymentSpec{
							Replicas: intPointer(1),
						},
					},
					Children: []*kube.Object{
						{
							Object: &appsv1.StatefulSet{
								Spec: appsv1.StatefulSetSpec{
									Replicas: intPointer(1),
								},
							},
						},
					},
				},
				{
					Object: &appsv1.DaemonSet{
						Status: appsv1.DaemonSetStatus{
							DesiredNumberScheduled: 2,
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.exp, countPodSpecs(test.arg))
		})
	}
}

func intPointer(x int32) *int32 {
	return &x
}
