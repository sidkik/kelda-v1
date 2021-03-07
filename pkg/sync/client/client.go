package client

//go:generate mockery -name Client

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/proto/dev"
	"github.com/kelda-inc/kelda/pkg/sync"
)

// Client is the interface for syncing code from the local machine into a
// development pod in Kubernetes.
type Client interface {
	SetTargetVersion(dev.SyncConfig, string) error
	GetMirrorSnapshot() (sync.MirrorSnapshot, error)
	Mirror(sync.SourceFile) error
	Remove(string) error
	SyncComplete() error
	Close() error
}

type client struct {
	namespace string
	pod       string

	kubeClient kubernetes.Interface
	restConfig *rest.Config

	pbClient dev.DevClient
	grpcConn *grpc.ClientConn
}

// New returns a new sync Client.
func New(kubeClient kubernetes.Interface, restConfig *rest.Config, namespace,
	pod string) (Client, error) {

	addr, err := getSyncTunnelAddress(kubeClient, restConfig, namespace, pod)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)))
	if err != nil {
		return nil, errors.WithContext(err, "dial")
	}

	return &client{
		namespace:  namespace,
		pod:        pod,
		kubeClient: kubeClient,
		restConfig: restConfig,
		pbClient:   dev.NewDevClient(conn),
		grpcConn:   conn,
	}, nil
}

func (c *client) SetTargetVersion(syncConfig dev.SyncConfig, version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &dev.SetTargetVersionRequest{
		Version: &dev.Version{
			SyncConfig: &syncConfig,
			Version:    version,
		},
	}
	resp, err := c.pbClient.SetTargetVersion(ctx, req)
	return errors.Unmarshal(err, resp.GetError())
}

func (c *client) GetMirrorSnapshot() (sync.MirrorSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.pbClient.GetMirrorSnapshot(ctx, &dev.GetMirrorSnapshotRequest{})
	err = errors.Unmarshal(err, resp.GetError())
	if err != nil {
		return nil, err
	}

	return sync.UnmarshalMirrorSnapshot(*resp.GetSnapshot())
}

func (c *client) SyncComplete() error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	resp, err := c.pbClient.SyncComplete(ctx, &dev.SyncCompleteRequest{})
	return errors.Unmarshal(err, resp.GetError())
}

func (c *client) Close() error {
	return c.grpcConn.Close()
}

const chunkSize = 1024

func (c *client) Mirror(toMirror sync.SourceFile) error {
	f, err := os.Open(toMirror.ContentsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Ignore the error that the file doesn't exist. These kinds of
			// files are typically temporary files like vim's .swp files.
			return nil
		}
		return errors.WithContext(err, "open")
	}
	defer f.Close()

	modTime, err := ptypes.TimestampProto(toMirror.ModTime)
	if err != nil {
		return errors.WithContext(err, "marshal modtime timestamp")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	stream, err := c.pbClient.Mirror(ctx)
	if err != nil {
		return errors.WithContext(err, "start stream")
	}

	// Inform the server what file we're staging in the first request.
	header := &dev.MirrorFile{
		SyncSourcePath: toMirror.SyncSourcePath,
		FileAttributes: &dev.FileAttributes{
			ContentsHash: toMirror.ContentsHash,
			Mode:         uint32(toMirror.Mode),
			ModTime:      modTime,
		},
	}
	err = stream.Send(&dev.MirrorFileRequest{Header: header})
	if err != nil {
		return errors.WithContext(err, "send header")
	}

	// Send the contents of the file to the server in `chunkSize` increments.
	buf := make([]byte, chunkSize)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return errors.WithContext(err, "read file")
		}

		err = stream.Send(&dev.MirrorFileRequest{
			Chunk: buf[:n],
		})
		if err != nil {
			// The server has closed the stream, likely because of an error
			// while processing the stream. Read the last message from the
			// server to discover the error.
			if err == io.EOF {
				resp := &dev.MirrorFileResponse{}
				recvErr := stream.RecvMsg(resp)
				err = errors.Unmarshal(recvErr, resp.GetError())
			}

			return errors.WithContext(err, "send file chunk")
		}
	}

	resp, err := stream.CloseAndRecv()
	return errors.Unmarshal(err, resp.GetError())
}

// Remove informs the server that `path` is no longer tracked locally.
// This can happen if the file is deleted, or if the sync config is changed so
// that the file isn't covered.
func (c *client) Remove(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	_, err := c.pbClient.Remove(ctx, &dev.RemoveFileRequest{Path: path})
	return err
}
