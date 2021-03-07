package update

import (
	"testing"

	"github.com/stretchr/testify/assert"

	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
)

func TestGetCredentialsFromDockerConfig(t *testing.T) {
	tests := []struct {
		name           string
		inputRegcred   []byte
		inputReg       string
		expUsername    string
		expPassword    string
		expErrorNotNil bool
	}{
		{
			name: "normal",
			inputRegcred: []byte(`{
	"auths": {
		"https://gcr.io": {
			"username": "username",
			"password": "password"
		}
	}
}
`),
			inputReg:       "gcr.io",
			expUsername:    "username",
			expPassword:    "password",
			expErrorNotNil: false,
		},
		{
			name:           "invalid json",
			inputRegcred:   []byte(`abcde`),
			inputReg:       "gcr.io",
			expUsername:    "",
			expPassword:    "",
			expErrorNotNil: true,
		},
		{
			name: "no matching registry",
			inputRegcred: []byte(`{
	"auths": {
		"https://index.docker.io": {
			"username": "username",
			"password": "password"
		}
	}
}
`),
			inputReg:       "gcr.io",
			expUsername:    "",
			expPassword:    "",
			expErrorNotNil: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			username, password, err := getCredentialsFromDockerConfig(test.inputRegcred,
				test.inputReg)
			assert.Equal(t, test.expUsername, username)
			assert.Equal(t, test.expPassword, password)
			if test.expErrorNotNil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStripTagFromImageURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expOutput string
	}{
		{
			name:      "no tag or digest",
			input:     "nginx",
			expOutput: "nginx",
		},
		{
			name:      "tag",
			input:     "nginx:tag",
			expOutput: "nginx",
		},
		{
			name:      "digest",
			input:     "nginx@digest",
			expOutput: "nginx",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expOutput, stripTagFromImageURL(test.input))
		})
	}
}

func TestInjectDigestIntoImageURL(t *testing.T) {
	tests := []struct {
		name          string
		inputImageURL string
		inputDigest   string
		expOutput     string
	}{
		{
			name:          "no tag or digest",
			inputImageURL: "nginx",
			inputDigest:   "newdigest",
			expOutput:     "nginx@newdigest",
		},
		{
			name:          "tag",
			inputImageURL: "nginx:tag",
			inputDigest:   "newdigest",
			expOutput:     "nginx@newdigest",
		},
		{
			name:          "digest",
			inputImageURL: "nginx@digest",
			inputDigest:   "newdigest",
			expOutput:     "nginx@newdigest",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expOutput, InjectDigestIntoImageURL(
				test.inputImageURL, test.inputDigest))
		})
	}
}

func TestGetContainerInfosFromManifests(t *testing.T) {
	tests := []struct {
		name      string
		input     []string
		expOutput []ContainerInfo
		expError  error
	}{
		{
			name: "valid manifests",
			input: []string{
				`apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: controller1
spec:
  replicas: 1
  template:
    metadata:
      labels:
        service: web
    spec:
      containers:
      - name: container1
        image: nginx
        imagePullPolicy: IfNotPresent
      - name: container2
        image: apache
        imagePullPolicy: IfNotPresent
      imagePullSecrets:
      - name: regcred
`,
				`apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: controller2
spec:
  replicas: 1
  template:
    metadata:
      labels:
        service: database
    spec:
      containers:
      - name: container1
        image: mariadb
        imagePullPolicy: IfNotPresent
      serviceAccountName: serviceAccount
`,
			},
			expOutput: []ContainerInfo{
				{
					ControllerName: "controller1",
					ContainerName:  "container1",
					ImageURL:       "nginx",
					PullSecrets: []string{
						"regcred",
					},
					ServiceAccount: "default",
				},
				{
					ControllerName: "controller1",
					ContainerName:  "container2",
					ImageURL:       "apache",
					PullSecrets: []string{
						"regcred",
					},
					ServiceAccount: "default",
				},
				{
					ControllerName: "controller2",
					ContainerName:  "container1",
					ImageURL:       "mariadb",
					PullSecrets:    []string{},
					ServiceAccount: "serviceAccount",
				},
			},
			expError: nil,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			containerInfos, err := GetContainerInfosFromManifests(test.input)
			assert.Equal(t, test.expOutput, containerInfos)
			assert.Equal(t, test.expError, err)
		})
	}
}

func TestFindDigest(t *testing.T) {
	imageDigests := []kelda.ImageDigest{
		{
			ControllerName: "controller1",
			ContainerName:  "container1",
			Digest:         "digest1",
			ImageURL:       "imageURL1",
		},
		{
			ControllerName: "controller2",
			ContainerName:  "container2",
			Digest:         "digest2",
			ImageURL:       "imageURL2",
		},
		{
			ControllerName: "controller3",
			ContainerName:  "container3",
			Digest:         "digest3",
			ImageURL:       "imageURL3",
		},
	}

	tests := []struct {
		name                string
		inputImageDigests   []kelda.ImageDigest
		inputControllerName string
		inputContainerName  string
		inputImageURL       string
		expOutput           *kelda.ImageDigest
		expOk               bool
	}{
		{
			name:                "ImageDigest exists",
			inputImageDigests:   imageDigests,
			inputControllerName: "controller2",
			inputContainerName:  "container2",
			inputImageURL:       "imageURL2",
			expOutput:           &imageDigests[1],
			expOk:               true,
		},
		{
			name:                "ImageDigest not exists",
			inputImageDigests:   imageDigests,
			inputControllerName: "controller4",
			inputContainerName:  "container4",
			expOutput:           nil,
			expOk:               false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			imageDigest, ok := FindDigest(test.inputImageDigests,
				test.inputControllerName, test.inputContainerName, test.inputImageURL)
			assert.Equal(t, test.expOutput, imageDigest)
			assert.Equal(t, test.expOk, ok)
		})
	}
}
