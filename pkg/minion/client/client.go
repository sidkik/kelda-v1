package client

//go:generate mockery -name Client

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/kube"
	"github.com/kelda-inc/kelda/pkg/proto/messages"
	"github.com/kelda-inc/kelda/pkg/proto/minion"
)

const (
	// KeldaNamespace is the namespace that Kelda is running in. It also
	// contains any default configuration for deploying development workspaces.
	KeldaNamespace = "kelda"

	// MinionLabelSelectorKey is the label selector key for the Kelda minion pod.
	MinionLabelSelectorKey = "service"

	// MinionLabelSelectorValue is the label selector value for the Kelda minion pod.
	MinionLabelSelectorValue = "kelda"

	// RegistrySecretName is the name of the secret containing the registry
	// credentials to pull images in the development workspace. If it's defined
	// in the KeldaNamespace, then a copy is made in the development workspace.
	RegistrySecretName = "regcred"

	// MinionContainerWaitingErrTemplate is a generic error template
	// for when the Minion Pod is in a "Waiting" state for reasons other than
	// starting or due to being in a crash loop. The reason is propagated back
	// to the user.
	MinionContainerWaitingErrTemplate = "The minion server is waiting due to reason: %s"

	// DefaultPort is the default port used by the minion server.
	DefaultPort = 9000
)

// Client is used for communicating with the Kelda minion.
type Client interface {
	CreateWorkspace(minion.Workspace) ([]*messages.Message, error)
	GetUpdates(string) ([]*minion.ServiceUpdate, error)
	PerformUpdates(string, []*minion.ServiceUpdate) error
	GetLicense() (config.License, error)
	GetVersion() (string, error)
	Close() error
}

type grpcClient struct {
	pbClient minion.KeldaClient
	grpcConn *grpc.ClientConn
}

type tunneledClient struct {
	minionTunnel *kube.Tunnel
	grpcClient
}

var (
	// ErrMinionPodNotFound occurs if the client successfully connects to the
	// cluster, but cannot find a minion pod. This happens if the minion has not
	// been deployed.
	ErrMinionPodNotFound = errors.NewFriendlyError("Could not find minion pod.\n" +
		"Check that you have correctly deployed the Kelda minion to your cluster " +
		"using `kelda setup-minion`")

	// ErrMultipleMinionPods occurs if the client finds more than one running
	// minion pod. This can occur if the minion is updating through a rolling
	// deployment, or if the number of minion replicas was deliberately increased.
	ErrMultipleMinionPods = errors.NewFriendlyError("Found multiple minion pods\n" +
		"The minion server may be updating, and should finish momentarily.")

	// ErrMinionContainerCreating occurs when the Pod is in "Waiting" Phase with
	// Reason: "ContainerCreating" due to the minion container booting up.
	ErrMinionContainerCreating = errors.NewFriendlyError("Minion server is starting.\n" +
		"Please wait until the minion server has fully started.")

	// ErrMinionContainerCrashLoop occurs when the container is in a crash loop,
	// which causes the container state to oscillate between "Waiting" (Reason:
	// CrashLoopBackOff), and "Terminated"
	ErrMinionContainerCrashLoop = errors.NewFriendlyError("Minion server is in a crash loop.\n" +
		"Try redeploying the Kelda minion via `kelda setup-minion`. If this issue persists, " +
		"contact your Kelda administrator.")

	// ErrMinionContainerTerminated occurs when the minion container has been
	// terminated e.g. due to the user deleting the minion deploynent from the
	// cluster or due to a crash.
	ErrMinionContainerTerminated = errors.NewFriendlyError("Minion server has been terminated.\n" +
		"If the minion deployment has not been deleted, the server should restart momentarily. " +
		"Contact your Kelda administrator if the issue persists.")
)

// Mocked for unit testing
var (
	getTunnel     = getTunnelImpl
	getGrpcClient = getGrpcClientImpl
)

// New creates a new client connected to the Kelda minion.
func New(kubeClient kubernetes.Interface, restConfig *rest.Config) (Client, error) {
	selector := fmt.Sprintf("%s=%s",
		MinionLabelSelectorKey,
		MinionLabelSelectorValue)
	listOpts := meta.ListOptions{LabelSelector: selector}
	pods, err := kubeClient.CoreV1().Pods(KeldaNamespace).List(listOpts)
	if err != nil {
		return nil, errors.WithContext(err, "failed to get pods")
	}

	switch len(pods.Items) {
	case 0:
		return nil, ErrMinionPodNotFound
	case 1:
	default:
		return nil, ErrMultipleMinionPods
	}

	// In certain situations pods can have no container statuses.
	if len(pods.Items[0].Status.ContainerStatuses) == 0 {
		return nil, errors.New("no container status found")
	}

	// Only one of these three fields can be specified at once.
	// If none of the three exist, the container is in "Waiting" status by default
	// There should also only be one container in a minion pod.
	switch cs := pods.Items[0].Status.ContainerStatuses[0].State; {
	case cs.Waiting != nil:
		// Discern if starting, crashing, or other
		switch cs.Waiting.Reason {
		case "CrashLoopBackOff":
			return nil, ErrMinionContainerCrashLoop
		case "ContainerCreating":
			return nil, ErrMinionContainerCreating
		default:
			return nil, errors.NewFriendlyError(
				MinionContainerWaitingErrTemplate, cs.Waiting.Reason)
		}
	case cs.Terminated != nil:
		return nil, ErrMinionContainerTerminated
	case cs.Running != nil:
	default:
		return nil, errors.NewFriendlyError(
			MinionContainerWaitingErrTemplate, "unknown")
	}

	podName := pods.Items[0].Name

	tunnel, err := getTunnel(kubeClient, restConfig, podName)
	if err != nil {
		return nil, errors.WithContext(err, "get tunnel")
	}

	grpcClient, err := getGrpcClient(tunnel)
	if err != nil {
		return nil, errors.WithContext(err, "get grpcclient")
	}

	return tunneledClient{
		minionTunnel: tunnel,
		grpcClient:   grpcClient,
	}, nil
}

func getTunnelImpl(kubeClient kubernetes.Interface, restConfig *rest.Config,
	podName string) (*kube.Tunnel, error) {
	tunnel := &kube.Tunnel{Namespace: KeldaNamespace, Pod: podName, RemotePort: 9000}
	restClient := kubeClient.CoreV1().RESTClient()
	if err := tunnel.Run(restClient, restConfig); err != nil {
		return nil, errors.WithContext(err, "forward port")
	}
	return tunnel, nil
}

func getGrpcClientImpl(tunnel *kube.Tunnel) (grpcClient, error) {
	addr := fmt.Sprintf("localhost:%d", tunnel.LocalPort)
	client, err := createGrpcClient(addr)
	if err != nil {
		return grpcClient{}, err
	}
	return client, nil
}

// NewWithAddress creates a new client connected to the Kelda minion at the
// given address. It does not set up tunnelling.
func NewWithAddress(addr string) (Client, error) {
	client, err := createGrpcClient(addr)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func createGrpcClient(addr string) (grpcClient, error) {
	keepaliveOpt := grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second})
	conn, err := grpc.Dial(addr, grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)), keepaliveOpt)
	if err != nil {
		return grpcClient{}, err
	}

	return grpcClient{
		pbClient: minion.NewKeldaClient(conn),
		grpcConn: conn,
	}, nil
}

func (mc grpcClient) CreateWorkspace(ws minion.Workspace) ([]*messages.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	wsReq := &minion.CreateWorkspaceRequest{Workspace: &ws}
	resp, err := mc.pbClient.CreateWorkspace(ctx, wsReq)
	return resp.GetMessages(), errors.Unmarshal(err, resp.GetError())
}

func (mc grpcClient) GetLicense() (config.License, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	resp, err := mc.pbClient.GetLicense(ctx, &minion.GetLicenseRequest{})
	if err = errors.Unmarshal(err, resp.GetError()); err != nil {
		return config.License{}, err
	}

	license, err := unmarshalLicense(resp.GetLicense())
	if err != nil {
		return config.License{}, errors.WithContext(err, "unmarshal license")
	}
	return license, nil
}

func unmarshalLicense(pbLicense *minion.License) (config.License, error) {
	expiryTime, err := ptypes.Timestamp(pbLicense.GetTerms().GetExpiryTime())
	if err != nil {
		return config.License{}, errors.WithContext(err, "parse timestamp")
	}

	var licenseType config.LicenseType
	switch pbLicense.GetTerms().GetType() {
	case minion.LicenseTerms_CUSTOMER:
		licenseType = config.Customer
	case minion.LicenseTerms_TRIAL:
		licenseType = config.Trial
	default:
		return config.License{}, errors.New("unknown license type")
	}

	return config.License{
		Terms: config.Terms{
			Customer:   pbLicense.GetTerms().GetCustomer(),
			Type:       licenseType,
			Seats:      int(pbLicense.GetTerms().GetSeats()),
			ExpiryTime: expiryTime,
		},
	}, nil
}

func (mc grpcClient) GetVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	resp, err := mc.pbClient.GetVersion(ctx, &minion.GetVersionRequest{})
	return resp.GetKeldaVersion().GetVersion(), errors.Unmarshal(err, resp.GetError())
}

func (mc grpcClient) GetUpdates(namespace string) ([]*minion.ServiceUpdate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	resp, err := mc.pbClient.GetUpdates(ctx, &minion.GetUpdatesRequest{
		Namespace: namespace,
	})
	return resp.GetServiceUpdates(), errors.Unmarshal(err, resp.GetError())
}

func (mc grpcClient) PerformUpdates(namespace string,
	serviceUpdates []*minion.ServiceUpdate) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	resp, err := mc.pbClient.PerformUpdates(ctx, &minion.PerformUpdatesRequest{
		Namespace:      namespace,
		ServiceUpdates: serviceUpdates,
	})
	return errors.Unmarshal(err, resp.GetError())
}

// Close closes the grpc connection for communicating with the Kelda minion.
func (mc grpcClient) Close() error {
	return mc.grpcConn.Close()
}

// Close closes the tunnel necessary for communicating with the Kelda minion.
func (mc tunneledClient) Close() (err error) {
	err = mc.grpcClient.Close()
	mc.minionTunnel.Close()
	return
}
