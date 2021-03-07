package update

import (
	_ "crypto/sha256" // For reference to correctly parse digest
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/distribution/reference"
	"github.com/kelda-inc/docker-registry-client/registry"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/kube"
)

// ContainerInfo contains a unique identifier for a container within a
// microservice and all information needed to pull its digest.
type ContainerInfo struct {
	ControllerName string
	ContainerName  string
	ImageURL       string
	PullSecrets    []string
	ServiceAccount string
}

func getImageDigest(imageURL string, regcred []byte) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageURL)
	if err != nil {
		return "", errors.WithContext(err, "parse normalized named")
	}

	digested, ok := named.(reference.Digested)
	if ok {
		// The URL contains a digest.
		return digested.Digest().String(), nil
	}

	// If imageURL already contains a tag, then this function does nothing.
	// Otherwise, it adds a "latest" tag to named and turn it into a
	// NamedTagged.
	named = reference.TagNameOnly(named)
	namedTagged, ok := named.(reference.NamedTagged)
	if !ok {
		// Unlikely. Only happens when there is a digest in the URL, which has
		// already been handled.
		return "", errors.New("image URL not tagged")
	}
	regAddr := reference.Domain(namedTagged)
	repo := reference.Path(namedTagged)
	tag := namedTagged.Tag()

	// docker.io is not a registry, but is returned by reference.Domain as the
	// default registry.
	if regAddr == "docker.io" {
		regAddr = "index.docker.io"
	}

	username, password := "", ""
	if len(regcred) != 0 {
		username, password, err = getCredentialsFromDockerConfig(regcred, regAddr)
		if err != nil {
			return "", errors.WithContext(err, "get credentials")
		}
	}

	reg, err := registry.New(fmt.Sprintf("https://%s/", regAddr), username, password)
	if err != nil {
		return "", errors.WithContext(err, "connect to registry")
	}

	digest, err := reg.ManifestDigest(repo, tag)
	if err != nil {
		return "", errors.WithContext(err, "get digest")
	}
	return digest.String(), nil
}

func getCredentialsFromDockerConfig(dockerConfig []byte, registryName string) (string, string, error) {
	type Auth struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	type DockerConfig struct {
		Auths map[string]Auth `json:"auths"`
	}

	var dockerCfg DockerConfig
	err := json.Unmarshal(dockerConfig, &dockerCfg)
	if err != nil {
		return "", "", errors.WithContext(err, "json unmarshal")
	}

	for r, auth := range dockerCfg.Auths {
		if strings.Contains(r, registryName) {
			return auth.Username, auth.Password, nil
		}
	}
	return "", "", errors.New("no matching registry found")
}

func stripTagFromImageURL(imageURL string) string {
	if atIndex := strings.Index(imageURL, "@"); atIndex != -1 {
		imageURL = imageURL[:atIndex]
	}
	if colonIndex := strings.Index(imageURL, ":"); colonIndex != -1 {
		imageURL = imageURL[:colonIndex]
	}
	return imageURL
}

// InjectDigestIntoImageURL strips the original tag or digest (if any) in an
// image URL and appends a new digest to it.
func InjectDigestIntoImageURL(imageURL, newDigest string) string {
	return fmt.Sprintf("%s@%s", stripTagFromImageURL(imageURL), newDigest)
}

// GetContainerInfosFromManifests parses the given manifests and returns
// ContainerInfos for all containers found.
func GetContainerInfosFromManifests(manifests []string) ([]ContainerInfo, error) {
	var ret []ContainerInfo
	for _, manifest := range manifests {
		obj, err := kube.Parse([]byte(manifest))
		if err != nil {
			return nil, errors.WithContext(err, "parse manifest")
		}

		// Skip the manifest if it doesn't contain a PodSpec.
		switch obj.GetObjectKind().GroupVersionKind().Kind {
		case "Deployment", "DaemonSet", "StatefulSet", "Job":
		default:
			continue
		}

		unstructuredPodSpec, _, err := unstructured.NestedMap(obj.Object, "spec", "template", "spec")
		if err != nil {
			return nil, errors.WithContext(err, "get podSpec")
		}

		var podSpec corev1.PodSpec
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredPodSpec, &podSpec); err != nil {
			return nil, errors.WithContext(err, "parse podSpec")
		}

		pullSecrets := make([]string, len(podSpec.ImagePullSecrets))
		for i, s := range podSpec.ImagePullSecrets {
			pullSecrets[i] = s.Name
		}

		serviceAccount := podSpec.ServiceAccountName
		if serviceAccount == "" {
			serviceAccount = "default"
		}

		for _, container := range podSpec.Containers {
			ret = append(ret, ContainerInfo{
				ControllerName: obj.GetName(),
				ContainerName:  container.Name,
				ImageURL:       container.Image,
				PullSecrets:    pullSecrets,
				ServiceAccount: serviceAccount,
			})
		}
	}
	return ret, nil
}

// GetDigestFromContainerInfo retrieves the digest of the image specified in the
// ContainerInfo from its registry.
func GetDigestFromContainerInfo(info *ContainerInfo,
	secretClient typev1.SecretInterface,
	saClient typev1.ServiceAccountInterface) (string, error) {
	sa, err := saClient.Get(info.ServiceAccount, metav1.GetOptions{})
	if err != nil {
		return "", errors.WithContext(err, "get serviceaccount")
	}

	// Add the imagePullSecrets of the default ServiceAccount to the pull
	// secrets list.
	imagePullSecrets := info.PullSecrets
	for _, secret := range sa.ImagePullSecrets {
		imagePullSecrets = append(imagePullSecrets, secret.Name)
	}

	var errMsg string

	for _, pullSecret := range imagePullSecrets {
		secret, err := getSecret(secretClient, pullSecret)
		if err != nil {
			errMsg += fmt.Sprintf("get secret: %s; ", err.Error())
			// Try next secret
			continue
		}

		digest, err := getImageDigest(info.ImageURL, secret)
		if err == nil {
			return digest, nil
		}
		errMsg += fmt.Sprintf("get digest: %s; ", err.Error())
	}

	// All credentials failed or no credential provided. Try empty credential.
	digest, err := getImageDigest(info.ImageURL, nil)
	if err == nil {
		return digest, nil
	}
	errMsg += fmt.Sprintf("get digest: %s", err.Error())

	return "", errors.New(errMsg)
}

func getSecret(secretClient typev1.SecretInterface, name string) ([]byte, error) {
	secret, err := secretClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.WithContext(err, "get secret")
	}
	if secret.Type != corev1.SecretTypeDockerConfigJson {
		return nil, errors.New("wrong secret type")
	}
	ret, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return nil, fmt.Errorf("secret %q does not contain image pull secret", name)
	}
	return ret, nil
}

// FindDigest finds the specific kelda.ImageDigest from an array with the
// specified controller name and container name. If it doesn't find the element,
// it'll return nil.
func FindDigest(imageDigests []kelda.ImageDigest, controllerName, containerName,
	imageURL string) (*kelda.ImageDigest, bool) {

	for i, imageDigest := range imageDigests {
		if imageDigest.ControllerName == controllerName &&
			imageDigest.ContainerName == containerName &&
			imageDigest.ImageURL == imageURL {
			return &imageDigests[i], true
		}
	}
	return nil, false
}
