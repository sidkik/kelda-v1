package config

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/kelda-inc/kelda/pkg/errors"
)

const workspacePath = "/dir/workspace.yaml"

func TestParseWorkspaceVersion(t *testing.T) {
	webDeployment := deploymentYAML("web")
	webService := serviceYAML("web")
	serviceFiles := []file{
		{"service.yaml", webService},
		{"deployment.yml", webDeployment},
	}

	tests := []parseWorkspaceTest{
		{
			name:      "EmptyVersion",
			workspace: mustMarshal(Workspace{}),
			mockFiles: serviceFiles,
			expConfig: Workspace{
				Version: InitialWorkspaceConfigVersion,
				Services: []Service{
					{
						Name:      "web",
						Manifests: []string{webDeployment, webService},
					},
				},
			},
			expError: nil,
		},
		{
			name: "CorrectVersion",
			workspace: mustMarshal(Workspace{
				Version: SupportedWorkspaceConfigVersion,
			}),
			mockFiles: serviceFiles,
			expConfig: Workspace{
				Version: SupportedWorkspaceConfigVersion,
				Services: []Service{
					{
						Name:      "web",
						Manifests: []string{webDeployment, webService},
					},
				},
			},
			expError: nil,
		},
		{
			name: "IncorrectVersion",
			workspace: mustMarshal(Workspace{
				Version: "incorrect_version",
			}),
			expConfig: Workspace{},
			expError: errors.WithContext(incompatibleVersionError{
				path:   workspacePath,
				exp:    SupportedWorkspaceConfigVersion,
				actual: "incorrect_version",
			}, "parse"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func TestParseWorkspaceServices(t *testing.T) {
	gatewayDeployment := deploymentYAML("gateway")
	gatewayConfigMap := configMapYAML("gateway")
	webDeployment := deploymentYAML("web")
	webService := serviceYAML("web")
	webJob := jobYAML("web")
	setupDBJob := jobYAML("setup-db")

	tests := []parseWorkspaceTest{
		{
			name: "RootDir",
			expConfig: Workspace{
				Services: []Service{
					{
						Name:      "gateway",
						Manifests: []string{gatewayDeployment, gatewayConfigMap, webService},
					},
					{
						Name:      "setup-db",
						Manifests: []string{setupDBJob},
					},
					{
						Name:      "web",
						Manifests: []string{webDeployment},
					},
				},
			},
			mockFiles: []file{
				{"web-deployment.yaml", webDeployment},
				{"web-service.yaml", webService},
				{"gateway-deployment.yaml", gatewayDeployment},
				{"gateway-config.yaml", gatewayConfigMap},
				{"setup-db.yaml", setupDBJob},
			},
		},
		{
			name: "SubDir",
			expConfig: Workspace{
				Services: []Service{
					{
						Name:      "gateway-name-override",
						Manifests: []string{gatewayDeployment, gatewayConfigMap},
					},
					{
						Name:      "web",
						Manifests: []string{webDeployment, webService},
					},
				},
			},
			mockFiles: []file{
				{"web-deployment.yaml", webDeployment},
				{"web-service.yaml", webService},
				{"gateway-name-override/gateway-deployment.yaml", gatewayDeployment},
				{"gateway-name-override/gateway-config.yaml", gatewayConfigMap},
			},
		},
		{
			name: "SubDirMultipleControllers",
			workspace: mustMarshal(Workspace{
				Services: []Service{
					{Name: "service"},
				},
			}),
			expConfig: Workspace{
				Services: []Service{
					{
						Name:      "service-gateway",
						Manifests: []string{gatewayDeployment, gatewayConfigMap, webService},
					},
					{
						Name:      "service-web",
						Manifests: []string{webDeployment},
					},
				},
			},
			mockFiles: []file{
				{"service/web-deployment.yaml", webDeployment},
				{"service/web-service.yaml", webService},
				{"service/gateway-deployment.yaml", gatewayDeployment},
				{"service/gateway-config.yaml", gatewayConfigMap},
			},
		},
		{
			name: "NestedSubDir",
			expConfig: Workspace{
				Services: []Service{
					{
						Name:      "service-override",
						Manifests: []string{gatewayDeployment, gatewayConfigMap},
					},
					{
						Name:      "service-web",
						Manifests: []string{webDeployment, webService},
					},
				},
			},
			mockFiles: []file{
				{"service/web-deployment.yaml", webDeployment},
				{"service/web-service.yaml", webService},
				{"service/override/gateway-deployment.yaml", gatewayDeployment},
				{"service/override/gateway-config.yaml", gatewayConfigMap},
			},
		},
		{
			name: "DuplicateNames",
			expConfig: Workspace{
				Services: []Service{
					{
						Name:      "web-deployment",
						Manifests: []string{webDeployment},
					},
					{
						Name:      "web-job",
						Manifests: []string{webJob},
					},
				},
			},
			mockFiles: []file{
				{"web-deployment.yaml", webDeployment},
				{"web-job.yaml", webJob},
			},
		},
		{
			name: "MultipleManifestsPerFile",
			expConfig: Workspace{
				Services: []Service{
					{
						Name:      "gateway",
						Manifests: []string{gatewayDeployment, gatewayConfigMap},
					},
					{
						Name:      "web",
						Manifests: []string{webDeployment},
					},
				},
			},
			mockFiles: []file{
				{"combined.yaml", strings.Join(
					[]string{webDeployment, gatewayConfigMap, gatewayDeployment},
					"---\n")},
			},
		},
		{
			name: "IgnoreNonYAML",
			expConfig: Workspace{
				Services: []Service{
					{
						Name:      "web",
						Manifests: []string{webDeployment},
					},
				},
			},
			mockFiles: []file{
				{"web-deployment.yaml", webDeployment},
				{"ignoreme", "ignoreme"},
			},
		},
		{
			name: "InvalidYAML",
			expError: errors.WithContext(
				errors.NewFriendlyError(fmt.Sprintf(errParseFile,
					"/dir/broken.yaml",
					"parse Kubernetes object: Object 'Kind' is missing in 'invalid yaml:\n'")),
				"get services"),
			mockFiles: []file{
				{"broken.yaml", "invalid yaml:"},
			},
		},
		{
			name: "Explicit",
			workspace: mustMarshal(Workspace{
				Services: []Service{
					{Name: "gateway"},
					{Name: "web"},
				},
			}),
			expConfig: Workspace{
				Services: []Service{
					{
						Name:      "gateway",
						Manifests: []string{gatewayDeployment, gatewayConfigMap},
					},
					{
						Name:      "web",
						Manifests: []string{webDeployment, webService},
					},
				},
			},
			mockFiles: []file{
				{"ignoreme/setup-db.yaml", setupDBJob},
				{"web/web-deployment.yaml", webDeployment},
				{"web/web-service.yaml", webService},
				{"gateway/gateway-deployment.yaml", gatewayDeployment},
				{"gateway/gateway-config.yaml", gatewayConfigMap},
			},
		},
		{
			name: "ExplicitMissingYAML",
			workspace: mustMarshal(Workspace{
				Services: []Service{
					{Name: "missing-yaml"},
				},
			}),
			expError: errors.NewFriendlyError(
				"Kubernetes YAML directory for service \"missing-yaml\" does not exist.\n" +
					"Each service in the Workspace configuration must have a " +
					"corresponding directory containing the Kubernetes YAML for " +
					"deploying the service.\n\nSee http://docs.kelda.io/reference/" +
					"configuration/#workspace-configuration for more information."),
		},
		{
			name: "Needs non-empty script for no name",
			workspace: mustMarshal(Workspace{
				Services: []Service{
					{
						Name:   "",
						Script: []string{},
					},
				},
			}),
			expError: errors.NewFriendlyError(
				"Please fix your workspace.yml file.\n" +
					"The name field is required for services that don't have a script field."),
		},
	}
	for _, test := range tests {
		test.expConfig.Version = SupportedWorkspaceConfigVersion
		t.Run(test.name, test.run)
	}
}

func TestParseWorkspaceLint(t *testing.T) {
	webDeployment := deploymentYAML("web")

	tests := []parseWorkspaceTest{
		{
			name: "ExtraFields",
			workspace: []byte(fmt.Sprintf(
				"version: %s\nextra: fields", SupportedWorkspaceConfigVersion)),
			expError: errors.WithContext(
				errors.NewFriendlyError(parseConfigErrTemplate, workspacePath,
					errors.New("error unmarshaling JSON: while decoding JSON: "+
						`json: unknown field "extra"`)),
				"parse"),
		},
		{
			name: "FieldsCheckAfterVersion",
			workspace: []byte(`
version: incorrect_version
extra: fields
`),
			expError: errors.WithContext(incompatibleVersionError{
				path:   workspacePath,
				exp:    SupportedWorkspaceConfigVersion,
				actual: "incorrect_version",
			}, "parse"),
		},
		{
			name: "NoServicesDefined",
			workspace: mustMarshal(Workspace{
				Version: SupportedWorkspaceConfigVersion,
			}),
			expError: errors.NewFriendlyError(
				"No Kubernetes YAML found in the Workspace directory for /dir/workspace.yaml.\n" +
					"See kelda.io/docs/reference/configuration/#workspace-configuration for more information."),
		},
		{
			name: "TunnelMissingServiceName",
			workspace: mustMarshal(Workspace{
				Version: SupportedWorkspaceConfigVersion,
				Tunnels: []Tunnel{
					{LocalPort: 8080, RemotePort: 8080},
				},
			}),
			mockFiles: []file{{"deployment.yml", webDeployment}},
			expError: errors.NewFriendlyError(
				"A tunnel in /dir/workspace.yaml is missing a required field.\n" +
					"The following fields are required:\n" +
					"* serviceName: a string denoting the name of the service to connect to.\n" +
					"* remotePort: the port number on the remote pod.\n" +
					"* localPort: the port number on your local machine.\n\n" +
					"See kelda.io/docs/reference/configuration/#tunnels for more information."),
		},
		{
			name: "TunnelMissingPorts",
			workspace: mustMarshal(Workspace{
				Version: SupportedWorkspaceConfigVersion,
				Tunnels: []Tunnel{{ServiceName: "web"}},
			}),
			mockFiles: []file{{"deployment.yml", webDeployment}},
			expError: errors.NewFriendlyError(
				"A tunnel in /dir/workspace.yaml is missing a required field.\n" +
					"The following fields are required:\n" +
					"* serviceName: a string denoting the name of the service to connect to.\n" +
					"* remotePort: the port number on the remote pod.\n" +
					"* localPort: the port number on your local machine.\n\n" +
					"See kelda.io/docs/reference/configuration/#tunnels for more information."),
		},
		{
			name:      "TunnelOnNonexistentServicename",
			mockFiles: []file{{"deployment.yml", webDeployment}},
			workspace: mustMarshal(Workspace{
				Version: SupportedWorkspaceConfigVersion,
				Tunnels: []Tunnel{
					{
						ServiceName: "nonexistentService",
						LocalPort:   8080,
						RemotePort:  8080,
					},
				},
			}),
			expError: errors.NewFriendlyError(
				"A tunnel in /dir/workspace.yaml refers to service \"nonexistentService\", " +
					"which does not exist.\n\nThe valid service names are: [web]"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

type parseWorkspaceTest struct {
	name      string
	workspace []byte
	mockFiles []file
	expConfig Workspace
	expError  error
}

func (test parseWorkspaceTest) run(t *testing.T) {
	fs = afero.NewMemMapFs()
	for _, f := range test.mockFiles {
		path := filepath.Join(filepath.Dir(workspacePath), f.name)
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, afero.WriteFile(fs, path, []byte(f.contents), 0644))
	}
	require.NoError(t, fs.MkdirAll(filepath.Dir(workspacePath), 0755))
	require.NoError(t, afero.WriteFile(fs, workspacePath, test.workspace, 0644))

	config, err := ParseWorkspace(nil, workspacePath, "")
	require.Equal(t, test.expError, err)

	if test.expError == nil {
		// Unset the path field since it's just used for error messages.
		config.path = ""
		test.expConfig.path = ""
		assert.Equal(t, test.expConfig, config)
	}
}

type file struct {
	name, contents string
}

func TestGetDevCommand(t *testing.T) {
	service := "service"

	nginxWithCommand := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "nginx",
							Command: []string{"sh", "nginx.sh"},
						},
					},
				},
			},
		},
	}

	nginxWithArgs := nginxWithCommand.DeepCopy()
	nginxWithArgs.Spec.Template.Spec.Containers[0].Command = nil
	nginxWithArgs.Spec.Template.Spec.Containers[0].Args = []string{"/etc/nginx.conf"}

	nginxWithoutCommand := nginxWithCommand.DeepCopy()
	nginxWithoutCommand.Spec.Template.Spec.Containers[0].Command = nil

	alpineWithCommand := nginxWithCommand.DeepCopy()
	alpineWithCommand.Spec.Template.Spec.Containers[0] = corev1.Container{
		Name:    "alpine",
		Command: []string{"sh", "nginx.sh"},
	}

	tests := []struct {
		name        string
		svc         Service
		expCommand  []string
		shouldError bool
	}{
		{
			name: "Success",
			svc: Service{
				Name:      service,
				Manifests: marshalObjects(nginxWithCommand),
			},
			expCommand: nginxWithCommand.Spec.Template.Spec.Containers[0].Command,
		},
		{
			name: "MultiplePods",
			svc: Service{
				Name:      service,
				Manifests: marshalObjects(nginxWithCommand, alpineWithCommand),
			},
			shouldError: true,
		},
		{
			name: "MalformedManifest",
			svc: Service{
				Name:      service,
				Manifests: []string{"error"},
			},
			shouldError: true,
		},
		{
			name: "Args",
			svc: Service{
				Name:      service,
				Manifests: marshalObjects(nginxWithArgs),
			},
			shouldError: true,
		},
		{
			name: "NoCommand",
			svc: Service{
				Name:      service,
				Manifests: marshalObjects(nginxWithoutCommand),
			},
			shouldError: true,
		},
		{
			name: "NoPod",
			svc: Service{
				Name:      service,
				Manifests: marshalObjects(&corev1.ConfigMap{}),
			},
			shouldError: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			actual, err := test.svc.GetDevCommand()
			if test.shouldError {
				assert.NotNil(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expCommand, actual)
			}
		})
	}
}

func TestGetServicesForScript(t *testing.T) {
	serviceName := "service"
	tests := []struct {
		name        string
		serviceName string
		script      []string
		expServices []Service
		expError    bool
	}{
		{
			name:        "SinglePod",
			serviceName: serviceName,
			script:      []string{"printf", deploymentYAML("hello")},
			expServices: []Service{
				{
					Name:      serviceName,
					Manifests: []string{deploymentYAML("hello")},
				},
			},
		},
		{
			name:        "MultiPod",
			serviceName: serviceName,
			script:      []string{"printf", deploymentYAML("hello") + "---\n" + deploymentYAML("world")},
			expServices: []Service{
				{
					Name:      "service-hello",
					Manifests: []string{deploymentYAML("hello")},
				},
				{
					Name:      "service-world",
					Manifests: []string{deploymentYAML("world")},
				},
			},
		},
		{
			name:        "No Service Name",
			serviceName: "",
			script:      []string{"printf", deploymentYAML("hello")},
			expServices: []Service{
				{
					Name:      "hello",
					Manifests: []string{deploymentYAML("hello")},
				},
			},
		},
		{
			name:        "MultiPod, No Service Name",
			serviceName: "",
			script:      []string{"printf", deploymentYAML("one") + "---\n" + deploymentYAML("two")},
			expServices: []Service{
				{
					Name:      "one",
					Manifests: []string{deploymentYAML("one")},
				},
				{
					Name:      "two",
					Manifests: []string{deploymentYAML("two")},
				},
			},
		},
		{
			name:        "BadScript",
			serviceName: serviceName,
			script:      []string{"exit", "1"},
			expError:    true,
		},
		{
			name:        "BadYAML",
			serviceName: serviceName,
			script:      []string{"printf", "invalid yaml"},
			expError:    true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			svcs, err := getServicesForScript("", Service{
				Name:   test.serviceName,
				Script: test.script,
			}, "")
			assert.Equal(t, test.expServices, svcs)

			if test.expError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func marshalObjects(objs ...runtime.Object) (strs []string) {
	for _, obj := range objs {
		strs = append(strs, marshalObject(obj))
	}
	return strs
}

func marshalObject(obj runtime.Object) string {
	out := bytes.NewBuffer(nil)
	err := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme).Encode(obj, out)
	if err != nil {
		panic(fmt.Errorf("bad test: failed to serialize object: %s", err))
	}
	return out.String()
}

func jobYAML(name string) string {
	return fmt.Sprintf(`
apiVersion: batch/v1
kind: Job
metadata:
  name: %s
spec:
  template:
    spec:
      containers:
      - name: my-container
        image: my-image
  backoffLimit: 4
`, name)
}

func deploymentYAML(name string) string {
	return fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      service: svc
  template:
    metadata:
      labels:
        service: svc
    spec:
      containers:
      - name: my-container
        image: nginx
`, name)
}

func serviceYAML(name string) string {
	return fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
spec:
  ports:
  - name: http
    port: 80
    targetPort: 80
    type: ClusterIP
  selector:
    service: svc
`, name)
}

func configMapYAML(name string) string {
	return fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
data:
  key: val
`, name)
}
