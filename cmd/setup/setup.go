package setup

import (
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/pkg/namesgenerator"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsClientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/pkg/analytics"
	"github.com/kelda-inc/kelda/pkg/config"
	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	"github.com/kelda-inc/kelda/pkg/errors"
	minionClient "github.com/kelda-inc/kelda/pkg/minion/client"
	"github.com/kelda-inc/kelda/pkg/version"
)

const (
	confDir              = "/etc/kelda"
	licenseConfigMapName = "license"
	licenseKeyName       = "license"
)

// LicensePathInMinion is the path to the Kelda license within the minion container.
var LicensePathInMinion = filepath.Join(confDir, licenseKeyName)

var (
	fs            = afero.NewOsFs()
	getRandomName = namesgenerator.GetRandomName
)

// WaitForMinionErrorTemplate is an error message template shown to the user
// when there is an unrecoverable error deploying Kelda minion server.
const WaitForMinionErrorTemplate = "%s Please check `kubectl -n kelda " +
	"describe pod kelda` for more information."

// New creates a new `setup-minion` command.
func New() *cobra.Command {
	var force bool
	var licensePath string
	cmd := &cobra.Command{
		Use:   "setup-minion",
		Short: "Install the Kelda cluster components",
		Run: func(_ *cobra.Command, _ []string) {
			if err := main(licensePath, !force); err != nil {
				util.HandleFatalError(err)
			}
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Don't prompt before installing")
	cmd.Flags().StringVar(&licensePath, "license", "", "Path to the Kelda license. "+
		"Optional if Kelda has been deployed before.")
	return cmd
}

func getLicense(kubeClient kubernetes.Interface, path string) (string, error) {
	if path == "" {
		currLicense, err := kubeClient.CoreV1().ConfigMaps(minionClient.KeldaNamespace).Get(
			licenseConfigMapName, metav1.GetOptions{})
		if err == nil {
			return currLicense.Data[licenseKeyName], nil
		}

		if kerrors.IsNotFound(err) {
			log.Info("No license found. Deploying in trial mode, which allows one developer per cluster.")
			license := config.License{
				Terms: config.Terms{
					Customer: getRandomName(0),
					Type:     config.Trial,
				},
			}

			licenseStr, err := license.Marshal(nil)
			if err != nil {
				return "", errors.WithContext(err, "marshal generated license")
			}
			return licenseStr, nil
		}
		return "", errors.WithContext(err, "get current license")
	}

	license, err := afero.ReadFile(fs, path)
	switch {
	case err == nil:
		return string(license), nil
	case os.IsNotExist(err):
		return "", errors.NewFriendlyError("License file does not exist. Aborting.")
	default:
		return "", errors.NewFriendlyError("Failed to read license at `%s`:\n%s", path, err)
	}
}

func main(licensePath string, shouldPrompt bool) error {
	context, kubeClient, crdClient, err := getKubeClient()
	if err != nil {
		return errors.WithContext(err, "connect to Kubernetes")
	}

	rawLicense, err := getLicense(kubeClient, licensePath)
	if err != nil {
		return errors.WithContext(err, "read license")
	}

	license, err := config.ParseLicense(rawLicense)
	if err != nil {
		return errors.WithContext(err, "parse license")
	}
	analytics.SetCustomer(license.Terms.Customer)

	// If it's a trial, we need to show the EULA.
	if license.Terms.Type == config.Trial {
		eulaAccepted, err := ShowEULA()
		if err != nil {
			return errors.WithContext(err, "show EULA")
		}

		if !eulaAccepted {
			fmt.Println("You must accept the EULA to use Kelda.")
			return nil
		}
	}

	if shouldPrompt {
		prompt := fmt.Sprintf("Deploy to kubeconfig context `%s`?", context)
		shouldDeploy, err := util.PromptYesOrNo(prompt)
		if err != nil {
			return errors.WithContext(err, "prompt")
		}

		if !shouldDeploy {
			fmt.Println("Aborting.")
			return nil
		}
	}

	msg := fmt.Sprintf("Deploying Kelda components to the `%s` context..", context)
	pp := util.NewProgressPrinter(os.Stdout, msg)
	go pp.Run()

	if err := createObjects(kubeClient, crdClient, rawLicense); err != nil {
		pp.Stop()
		return errors.WithContext(err, "deploy")
	}
	pp.Stop()

	pp = util.NewProgressPrinter(os.Stdout, "Waiting for minion to boot..")
	go pp.Run()

	if err := waitForMinion(kubeClient); err != nil {
		pp.Stop()
		return errors.WithContext(err, "wait for minion")
	}
	pp.Stop()
	fmt.Println("Done!")
	return nil
}

func waitForMinion(kubeClient kubernetes.Interface) error {
	deploymentWatcher, err := kubeClient.AppsV1().
		Deployments(minionClient.KeldaNamespace).
		Watch(metav1.ListOptions{})
	if err != nil {
		return errors.WithContext(err, "watch")
	}
	defer deploymentWatcher.Stop()

	podWatcher, err := kubeClient.CoreV1().
		Pods(minionClient.KeldaNamespace).
		Watch(metav1.ListOptions{})
	if err != nil {
		return errors.WithContext(err, "watch")
	}
	defer podWatcher.Stop()

	deploymentTrigger := deploymentWatcher.ResultChan()
	podTrigger := podWatcher.ResultChan()
	timeout := time.After(5 * time.Minute)
	for {
		select {
		case <-timeout:
			return errors.New("timeout")
		case <-deploymentTrigger:
		case <-podTrigger:
		}

		ready, err := isMinionReady(kubeClient)
		if err != nil {
			return err
		}

		if ready {
			return nil
		}
	}
}

func isMinionReady(kubeClient kubernetes.Interface) (ready bool, err error) {
	deployment, err := kubeClient.AppsV1().
		Deployments(minionClient.KeldaNamespace).
		Get("kelda", metav1.GetOptions{})
	if err != nil {
		return false, errors.WithContext(err, "get")
	}

	// If the deployment hasn't created the ReplicaSet for the Deployment
	// spec yet, the status of any existing pods aren't relevant.
	// We check both the total replica count and the updated replica count
	// so that we don't erroneously process replicas from the previous
	// for the old version.
	// For example, when UpdatedReplicas is 1, and Replicas is 2, the new
	// Replica has been created, but the previous Replica hasn't been
	// destroyed yet.
	// It's also possible for UpdatedReplicas to be 0, and Replicas to be
	// 1, which indicates that the new Replica hasn't been created at all
	// yet.
	if deployment.Status.Replicas != 1 || deployment.Status.UpdatedReplicas != 1 {
		return false, nil
	}

	if deployment.Status.ReadyReplicas == 1 {
		return true, nil
	}

	// Abort early if the pod is in an unrecoverable state.
	podsList, err := kubeClient.CoreV1().
		Pods(minionClient.KeldaNamespace).
		List(metav1.ListOptions{})
	if err != nil {
		return false, errors.WithContext(err, "list")
	}

	pods := podsList.Items
	if len(pods) != 1 {
		return false, nil
	}

	switch pods[0].Status.Phase {
	case corev1.PodFailed:
		return false, fmt.Errorf("pod failed: %s", pods[0].Status.Message)
	case corev1.PodUnknown:
		return false, fmt.Errorf("pod unknown: %s", pods[0].Status.Message)
	}

	for _, cond := range pods[0].Status.Conditions {
		if cond.Type == corev1.PodScheduled &&
			cond.Status != corev1.ConditionTrue &&
			strings.Contains(cond.Message, "no nodes available to "+
				"schedule pods") {
			return false, errors.NewFriendlyError(WaitForMinionErrorTemplate,
				"The Kelda minion server is unschedulable because no "+
					"nodes are available in the cluster.")
		}
	}

	if statuses := pods[0].Status.ContainerStatuses; len(statuses) == 1 {
		if waitInfo := statuses[0].State.Waiting; waitInfo != nil {
			switch waitInfo.Reason {
			case "ImagePullBackOff", "ErrImagePull":
				return false, errors.NewFriendlyError(WaitForMinionErrorTemplate,
					"The cluster failed to pull the Kelda minion image.")
			}
		}
	}
	return false, nil
}

func createObjects(kubeClient kubernetes.Interface,
	crdClient apiextensionsClientset.Interface, license string) error {

	if err := createNamespace(kubeClient); err != nil {
		return errors.WithContext(err, "create namespace")
	}

	if err := createServiceAccount(kubeClient); err != nil {
		return errors.WithContext(err, "create service account")
	}

	if err := createCRDs(crdClient); err != nil {
		return errors.WithContext(err, "create CRDs")
	}

	if err := createDeployment(kubeClient, license); err != nil {
		return errors.WithContext(err, "create deployment")
	}

	if err := createService(kubeClient); err != nil {
		return errors.WithContext(err, "create service")
	}

	return nil
}

func createNamespace(kubeClient kubernetes.Interface) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kelda",
		},
	}

	c := kubeClient.CoreV1().Namespaces()
	curr, err := c.Get(ns.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if exists := err == nil; exists {
		ns.ResourceVersion = curr.ResourceVersion
		_, err = c.Update(ns)
		return err
	}
	_, err = c.Create(ns)
	return err
}

func createServiceAccount(kubeClient kubernetes.Interface) error {
	rbacClient := kubeClient.RbacV1()
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-access",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
			{
				NonResourceURLs: []string{"*"},
				Verbs:           []string{"*"},
			},
		},
	}
	currClusterRole, err := rbacClient.ClusterRoles().Get(clusterRole.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if exists := err == nil; exists {
		clusterRole.ResourceVersion = currClusterRole.ResourceVersion
		_, err = rbacClient.ClusterRoles().Update(clusterRole)
	} else {
		_, err = rbacClient.ClusterRoles().Create(clusterRole)
	}
	if err != nil {
		if kerrors.IsForbidden(err) {
			errTemplate := "Failed to create the cluster role used by Kelda " +
				"to interact with the Kubernetes API.\n\n" +
				"If you're running on a version of GKE older than v1.11.x or older, " +
				"this is most likely because your account doesn't have cluster-admin access " +
				"(https://cloud.google.com/kubernetes-engine/docs/how-to/" +
				"role-based-access-control#iam-rolebinding-bootstrap).\n" +
				"Re-run `kelda setup-minion` after granting yourself cluster-admin access.\n\n" +
				"For debugging, the full error is shown below:\n%s"
			return errors.NewFriendlyError(errTemplate, err)
		}
		return errors.WithContext(err, "cluster role")
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-access",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "kelda",
				Namespace: minionClient.KeldaNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "api-access",
		},
	}
	currClusterRoleBinding, err := rbacClient.ClusterRoleBindings().Get(clusterRoleBinding.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if exists := err == nil; exists {
		clusterRoleBinding.ResourceVersion = currClusterRoleBinding.ResourceVersion
		_, err = rbacClient.ClusterRoleBindings().Update(clusterRoleBinding)
	} else {
		_, err = rbacClient.ClusterRoleBindings().Create(clusterRoleBinding)
	}
	if err != nil {
		return errors.WithContext(err, "cluster role binding")
	}

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kelda",
			Namespace: minionClient.KeldaNamespace,
		},
	}
	serviceAccountClient := kubeClient.CoreV1().ServiceAccounts(serviceAccount.Namespace)
	currServiceAccount, err := serviceAccountClient.Get(serviceAccount.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if exists := err == nil; exists {
		serviceAccount.ResourceVersion = currServiceAccount.ResourceVersion
		// Copy over secrets, otherwise Kubernetes will create a duplicate token.
		serviceAccount.Secrets = currServiceAccount.Secrets
		_, err = serviceAccountClient.Update(serviceAccount)
	} else {
		_, err = serviceAccountClient.Create(serviceAccount)
	}
	if err != nil {
		return errors.WithContext(err, "service account")
	}

	return nil
}

func createCRDs(crdClient apiextensionsClientset.Interface) error {
	ms := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "microservices.kelda.io",
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   kelda.SchemeGroupVersion.Group,
			Version: kelda.SchemeGroupVersion.Version,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     "microservices",
				ShortNames: []string{"ms"},
				Kind:       "Microservice",
			},
			Scope: apiextensionsv1beta1.NamespaceScoped,
		},
	}

	tunnel := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tunnels.kelda.io",
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   kelda.SchemeGroupVersion.Group,
			Version: kelda.SchemeGroupVersion.Version,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: "tunnels",
				Kind:   "Tunnel",
			},
			Scope: apiextensionsv1beta1.NamespaceScoped,
		},
	}

	c := crdClient.ApiextensionsV1beta1().CustomResourceDefinitions()
	for _, crd := range []*apiextensionsv1beta1.CustomResourceDefinition{ms, tunnel} {
		curr, err := c.Get(crd.Name, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return errors.WithContext(err, "get")
		}

		if exists := err == nil; exists {
			crd.ResourceVersion = curr.ResourceVersion
			_, err = c.Update(crd)
		} else {
			_, err = c.Create(crd)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func createDeployment(kubeClient kubernetes.Interface, license string) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      licenseConfigMapName,
			Namespace: minionClient.KeldaNamespace,
		},
		Data: map[string]string{
			licenseKeyName: license,
		},
	}

	configMapClient := kubeClient.CoreV1().ConfigMaps(configMap.Namespace)
	currConfigMap, err := configMapClient.Get(configMap.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if exists := err == nil; exists {
		configMap.ResourceVersion = currConfigMap.ResourceVersion
		_, err = configMapClient.Update(configMap)
	} else {
		_, err = configMapClient.Create(configMap)
	}
	if err != nil {
		return errors.WithContext(err, "config map")
	}

	labels := map[string]string{
		minionClient.MinionLabelSelectorKey: minionClient.MinionLabelSelectorValue,
	}

	// Limits should always be set equal to requests in order to give the minion
	// pod a guaranteed QoS class, which gives it higher priority if the scheduler
	// starts to evict pods due to resource constraints.
	minionResourceReqs := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("500Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("500Mi"),
		},
	}

	licenseHash, err := hashString(license)
	if err != nil {
		return errors.WithContext(err, "hash license")
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kelda",
			Namespace: minionClient.KeldaNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						// Track the version of the license associated with the
						// minion pod.
						// This way, when the license changes, the annotation
						// will change, which will trigger the minion to
						// restart and read the new license.
						"kelda-license-hash": licenseHash,
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: licenseConfigMapName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: licenseConfigMapName,
									},
									Items: []corev1.KeyToPath{
										{
											Key:  licenseKeyName,
											Path: licenseKeyName,
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "kelda",
							Image:           version.KeldaImage,
							ImagePullPolicy: corev1.PullAlways,
							Command:         []string{"kelda", "minion"},
							Resources:       minionResourceReqs,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      licenseConfigMapName,
									MountPath: confDir,
								},
							},
							ReadinessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(minionClient.DefaultPort),
									},
								},

								// Give the minion some time to startup.
								InitialDelaySeconds: 5,
							},
						},
					},
					ServiceAccountName: "kelda",
				},
			},
		},
	}

	deploymentClient := kubeClient.AppsV1().Deployments(deployment.Namespace)
	currDeployment, err := deploymentClient.Get(deployment.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if exists := err == nil; exists {
		deployment.ResourceVersion = currDeployment.ResourceVersion
		_, err = deploymentClient.Update(deployment)
	} else {
		_, err = deploymentClient.Create(deployment)
	}
	return err
}

func createService(kubeClient kubernetes.Interface) error {
	selector := map[string]string{
		minionClient.MinionLabelSelectorKey: minionClient.MinionLabelSelectorValue,
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minion",
			Namespace: minionClient.KeldaNamespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{
				{Port: minionClient.DefaultPort},
			},
		},
	}

	c := kubeClient.CoreV1().Services(svc.Namespace)
	curr, err := c.Get(svc.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if exists := err == nil; exists {
		// Update by modifying `curr` so that we don't lose fields populated by
		// Kubernetes at runtime (e.g. `spec.ClusterIP`).
		curr.Spec.Selector = svc.Spec.Selector
		curr.Spec.Ports = svc.Spec.Ports
		_, err = c.Update(curr)
		return err
	}
	_, err = c.Create(svc)
	return err
}

func getKubeClient() (string, kubernetes.Interface, apiextensionsClientset.Interface, error) {
	kubeConfig := util.GetKubeConfig("")
	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return "", nil, nil, errors.WithContext(err, "get raw kubeconfig")
	}

	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return "", nil, nil, errors.WithContext(err, "get rest config")
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return "", nil, nil, errors.WithContext(err, "new kube client")
	}

	crdClient, err := apiextensionsClientset.NewForConfig(restConfig)
	if err != nil {
		return "", nil, nil, errors.WithContext(err, "new apiextensions client")
	}

	return rawConfig.CurrentContext, kubeClient, crdClient, nil
}

func hashString(str string) (string, error) {
	hasher := sha512.New()
	if _, err := hasher.Write([]byte(str)); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil)), nil
}
