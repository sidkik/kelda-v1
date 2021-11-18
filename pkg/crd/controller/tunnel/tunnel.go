package tunnel

//go:generate mockery -testonly -inpkg -name tunnelManager

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/workqueue"

	kelda "github.com/sidkik/kelda-v1/pkg/crd/apis/kelda/v1alpha1"
	clientset "github.com/sidkik/kelda-v1/pkg/crd/client/clientset/versioned"
	informerfactory "github.com/sidkik/kelda-v1/pkg/crd/client/informers/externalversions"
	informers "github.com/sidkik/kelda-v1/pkg/crd/client/informers/externalversions/kelda/v1alpha1"
	listers "github.com/sidkik/kelda-v1/pkg/crd/client/listers/kelda/v1alpha1"
	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/kube"
)

type controller struct {
	kubeClient     kubernetes.Interface
	keldaClient    clientset.Interface
	restConfig     *rest.Config
	msInformer     cache.SharedIndexInformer
	msLister       listers.MicroserviceLister
	tunnelInformer cache.SharedIndexInformer
	tunnelLister   listers.TunnelLister
	workqueue      workqueue.RateLimitingInterface
	tunnels        map[tunnelKey]tunnel
}

type tunnel struct {
	pod string
	crd *kelda.Tunnel
	tunnelManager
}

// tunnelKey uniquely identifies a tunnel in the cluster.
type tunnelKey struct {
	namespace string
	name      string
}

// Run starts the Tunnel controller. It creates and destroys Kubernetes port
// forwarding connections from the developer's machine to the cluster.
func Run(kubeClient kubernetes.Interface, keldaClient clientset.Interface,
	restConfig *rest.Config, namespace string) {

	keldaInformerFactory := informerfactory.NewSharedInformerFactoryWithOptions(
		keldaClient, 30*time.Second, informerfactory.WithNamespace(namespace))
	msInformer := keldaInformerFactory.Kelda().V1alpha1().Microservices()
	tunnelInformer := keldaInformerFactory.Kelda().V1alpha1().Tunnels()

	c := newController(keldaClient, msInformer, tunnelInformer, kubeClient, restConfig)

	stopCh := make(chan struct{})
	defer close(stopCh)

	c.run(stopCh)
}

func newController(keldaClient clientset.Interface, msInformer informers.MicroserviceInformer,
	tunnelInformer informers.TunnelInformer, kubeClient kubernetes.Interface,
	restConfig *rest.Config) controller {

	c := controller{
		kubeClient:     kubeClient,
		restConfig:     restConfig,
		keldaClient:    keldaClient,
		msInformer:     msInformer.Informer(),
		msLister:       msInformer.Lister(),
		tunnelInformer: tunnelInformer.Informer(),
		tunnelLister:   tunnelInformer.Lister(),
		workqueue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		tunnels:        map[tunnelKey]tunnel{},
	}

	tunnelInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(tunnel interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(tunnel)
			if err == nil {
				c.workqueue.Add(key)
			}
		},
		DeleteFunc: func(tunnel interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(tunnel)
			if err == nil {
				c.workqueue.Add(key)
			}
		},
		UpdateFunc: func(oldIntf, newIntf interface{}) {
			oldTunnel := oldIntf.(*kelda.Tunnel)
			newTunnel := newIntf.(*kelda.Tunnel)
			// Ignore updates triggered by the resync.
			if oldTunnel.ResourceVersion == newTunnel.ResourceVersion {
				return
			}

			key, err := cache.MetaNamespaceKeyFunc(newTunnel)
			if err == nil {
				c.workqueue.Add(key)
			}
		},
	})

	msInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueMicroservice(obj.(*kelda.Microservice))
		},
		DeleteFunc: func(obj interface{}) {
			var ms *kelda.Microservice
			switch obj := obj.(type) {
			case *kelda.Microservice:
				ms = obj
			case cache.DeletedFinalStateUnknown:
				ms = obj.Obj.(*kelda.Microservice)
			default:
				log.WithField("obj", obj).Warn("Unexpected object passed to delete handler. Ignoring.")
				return
			}
			c.enqueueMicroservice(ms)
		},
		UpdateFunc: func(oldIntf, newIntf interface{}) {
			oldMs := oldIntf.(*kelda.Microservice)
			newMs := newIntf.(*kelda.Microservice)
			// Ignore updates triggered by the resync.
			if oldMs.ResourceVersion == newMs.ResourceVersion {
				return
			}

			c.enqueueMicroservice(newMs)
		},
	})

	return c
}

func (c *controller) enqueueMicroservice(ms *kelda.Microservice) {
	tunnels, err := c.tunnelLister.Tunnels(ms.Namespace).List(labels.Everything())
	if err != nil {
		log.WithError(err).Error("Failed to list current tunnels. " +
			"Skipping Microservice update.")
		return
	}

	// Reprocess any tunnels that might be affected by the change to the
	// Microservice.
	for _, tunnel := range tunnels {
		if tunnel.Spec.Service == ms.Name {
			key, err := cache.MetaNamespaceKeyFunc(tunnel)
			if err == nil {
				c.workqueue.Add(key)
			}
		}
	}
}

func (c *controller) run(stopCh <-chan struct{}) {
	defer c.workqueue.ShutDown()
	go c.tunnelInformer.Run(stopCh)
	go c.msInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.tunnelInformer.HasSynced) {
		return
	}

	if !cache.WaitForCacheSync(stopCh, c.msInformer.HasSynced) {
		return
	}

	wait.Until(c.runWorker, time.Second, stopCh)
}

func (c *controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *controller) processNextItem() bool {
	key, quit := c.workqueue.Get()
	if quit {
		return false
	}
	defer c.workqueue.Done(key)

	obj, exists, err := c.tunnelInformer.GetIndexer().GetByKey(key.(string))
	if err != nil {
		log.WithError(err).WithField("key", key).Error("Failed to get tunnel")
		c.workqueue.AddRateLimited(key)
		return true
	}

	if exists {
		c.syncTunnel(obj.(*kelda.Tunnel))
	} else {
		namespace, name, err := cache.SplitMetaNamespaceKey(key.(string))
		if err != nil {
			log.WithError(err).WithField("key", key).Error("Unexpected tunnel key format. Ignoring.")
		} else {
			c.ensureTunnelDoesNotExist(namespace, name)
		}
	}

	c.workqueue.Forget(key)
	return true
}

func (c *controller) ensureTunnelDoesNotExist(namespace, name string) {
	key := tunnelKey{namespace, name}
	tunnel, ok := c.tunnels[key]
	if ok {
		tunnel.Stop()
		delete(c.tunnels, key)
	}
}

// syncTunnel ensures that the given tunnel is properly setup, and removes any
// previous instances of the tunnel.
func (c *controller) syncTunnel(target *kelda.Tunnel) {
	key := tunnelKey{target.Namespace, target.Name}
	expPod, hasPod := c.getPodForService(target.Namespace, target.Spec.Service)
	curr, ok := c.tunnels[key]
	if ok {
		if hasPod && curr.pod == expPod && curr.crd.Spec == target.Spec {
			return
		}

		curr.Stop()
		delete(c.tunnels, key)
	}

	// There's no pod to create a tunnel to. Don't do anything for now -- once
	// the Microservice gets updated with the pod status, the tunnel will be
	// reprocessed.
	if !hasPod {
		return
	}

	c.tunnels[key] = tunnel{
		pod:           expPod,
		crd:           target,
		tunnelManager: newTunnelManager(c.kubeClient, c.keldaClient, c.restConfig, target, expPod),
	}
	go c.tunnels[key].Start()
}

func (c *controller) getPodForService(namespace, name string) (string, bool) {
	ms, err := c.msLister.Microservices(namespace).Get(name)
	if err != nil {
		return "", false
	}

	pods := kube.SelectPods(ms.Status.Actual, true)
	if len(pods) != 1 {
		return "", false
	}

	return pods[0].Name, true
}

type tunnelManager interface {
	Start()
	Stop()
}

// tunnelManagerImpl controls stopping tunnels.
type tunnelManagerImpl struct {
	kubeClient  kubernetes.Interface
	keldaClient clientset.Interface
	restConfig  *rest.Config

	pod  string
	spec *kelda.Tunnel

	stop    chan struct{}
	stopped chan struct{}

	// Assigned to a variable for unit testing.
	runTunnelOnce func(chan struct{}, chan struct{}) error
}

var newTunnelManager = newTunnelManagerImpl

func newTunnelManagerImpl(kubeClient kubernetes.Interface, keldaClient clientset.Interface,
	restConfig *rest.Config, spec *kelda.Tunnel, pod string) tunnelManager {
	tm := &tunnelManagerImpl{
		kubeClient:  kubeClient,
		keldaClient: keldaClient,
		restConfig:  restConfig,
		pod:         pod,
		spec:        spec,
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
	}
	tm.runTunnelOnce = tm.runTunnelOnceImpl
	return tm
}

// Stop shuts down the tunnel. It blocks until the tunnel is completely shut down.
func (tm *tunnelManagerImpl) Stop() {
	close(tm.stop)
	<-tm.stopped
}

// Start creates a tunnel to the given pod according to the tunnel spec. If the
// tunnel crashes, it automatically tries to recreate it. It publishes updates
// to the tunnel's state directly to the tunnel CRD.
func (tm *tunnelManagerImpl) Start() {
	// How long to sleep after a tunnel crash before retrying. The duration is
	// updated for each failure to create an exponential backoff.
	const defaultSleepTime = 1 * time.Second
	const maxSleepTime = 30 * time.Second
	sleepTime := defaultSleepTime

	defer close(tm.stopped)
	for {
		select {
		case <-tm.stop:
			return
		default:
		}

		ready := make(chan struct{})
		go func() {
			select {
			case <-tm.stop:
				return
			case <-ready:
				sleepTime = defaultSleepTime
				upStatus := kelda.TunnelStatus{Phase: kelda.TunnelUp}
				if err := updateStatus(tm.keldaClient, tm.spec, upStatus); err != nil {
					log.WithError(err).Error("Failed to update tunnel status")
				}
			}
		}()

		crashedStatus := kelda.TunnelStatus{Phase: kelda.TunnelCrashed}
		err := tm.runTunnelOnce(tm.stop, ready)
		if err != nil {
			log.WithError(err).WithField("pod", tm.pod).
				Error("Tunnel crashed. Will recreate.")
			crashedStatus.Message = err.Error()
		}

		if err := updateStatus(tm.keldaClient, tm.spec, crashedStatus); err != nil {
			log.WithError(err).Error("Failed to update tunnel status")
		}

		// Don't retry creating the tunnel immediately to avoid a tight crash loop.
		time.Sleep(sleepTime)
		sleepTime *= 2
		if sleepTime > maxSleepTime {
			sleepTime = maxSleepTime
		}
	}
}

// runTunnelOnce creates a tunnel between the local machine and the Kubernetes
// cluster. It exits once the tunnel is shut down.
func (tm *tunnelManagerImpl) runTunnelOnceImpl(stop, ready chan struct{}) error {
	client := tm.kubeClient.CoreV1().RESTClient()
	u := client.Post().
		Resource("pods").
		Namespace(tm.spec.Namespace).
		Name(tm.pod).
		SubResource("portforward").URL()

	transport, upgrader, err := spdy.RoundTripperFor(tm.restConfig)
	if err != nil {
		return err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", u)

	ports := []string{fmt.Sprintf("%d:%d", tm.spec.Spec.LocalPort, tm.spec.Spec.RemotePort)}
	forwarder, err := portforward.New(dialer, ports, stop, ready,
		ioutil.Discard, ioutil.Discard)
	if err != nil {
		return err
	}

	return forwarder.ForwardPorts()
}

// updateStatus updates the status field of the given Tunnel.
func updateStatus(keldaClient clientset.Interface, tunnel *kelda.Tunnel,
	status kelda.TunnelStatus) error {

	curr, err := keldaClient.KeldaV1alpha1().Tunnels(tunnel.Namespace).Get(tunnel.Name, metav1.GetOptions{})
	if err != nil {
		// The tunnel was deleted by the minion, so the status update is obsolete.
		if kerrors.IsNotFound(err) {
			return nil
		}
		return errors.WithContext(err, "get")
	}

	// The tunnel has been changed by the minion, so the status update is obsolete.
	if curr.Spec != tunnel.Spec {
		return nil
	}

	curr.Status = status
	_, err = keldaClient.KeldaV1alpha1().Tunnels(tunnel.Namespace).Update(curr)

	// Even though we checked for these scenarios above, it's still possible
	// that an update occurred between the Get and Update calls.
	if kerrors.IsConflict(err) || kerrors.IsNotFound(err) {
		return nil
	}
	return err
}
