package config

import (
	"errors"
	"fmt"
	"net/url"
	"sort"

	"github.com/GoogleCloudPlatform/k8s-stackdriver/prometheus-to-sd/flags"
	"github.com/golang/glog"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	podNamespace = "kube-system"
	nameLabel    = "k8s-app"
)

// SourceConfigsFromDynamicSources takes pod specifications from the Kubernetes API and maps them to source configs.
func SourceConfigsFromDynamicSources(gceConfig *GceConfig, sources []flags.Uri) ([]SourceConfig, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	sourceMap, err := validateSources(sources)
	if err != nil {
		return nil, err
	}
	kubeApi, err := createKubernetesApiClient()
	if err != nil {
		return nil, err
	}
	podResponse, err := kubeApi.CoreV1().Pods(podNamespace).List(createOptionsForPodSelection(gceConfig.Instance, sourceMap))
	if err != nil {
		return nil, err
	}
	return getConfigsFromPods(podResponse.Items, sourceMap), nil
}

func validateSources(sources flags.Uris) (map[string]url.URL, error) {
	sourceMap := make(map[string]url.URL)
	for _, source := range sources {
		if source.Val.Hostname() != "" {
			return nil, errors.New("hostname should be empty for all dynamic sources")
		}
		if source.Key == "" {
			return nil, errors.New("component name should NOT be empty for any dynamic source")
		}
		if source.Val.Port() == "" {
			return nil, errors.New("port should NOT be empty for any dynamic source")
		}
		sourceMap[source.Key] = source.Val
	}
	if len(sourceMap) != len(sources) {
		return nil, errors.New("source should have unique component names")
	}
	return sourceMap, nil
}

func createKubernetesApiClient() (clientset.Interface, error) {
	conf, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return clientset.NewForConfig(conf)
}

func createOptionsForPodSelection(nodeName string, sources map[string]url.URL) v1.ListOptions {
	var nameList string
	var keys []string
	for key := range sources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		nameList += key + ","
	}
	labelSelector := fmt.Sprintf("%s in (%s)", nameLabel, nameList)
	return v1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
		LabelSelector: labelSelector,
	}
}

func getConfigsFromPods(pods []core.Pod, sources map[string]url.URL) []SourceConfig {
	var sourceConfigs []SourceConfig
	for _, pod := range pods {
		componentName := pod.Labels[nameLabel]
		source, _ := sources[componentName]
		sourceConfig, err := mapToSourceConfig(componentName, source, pod.Status.PodIP, pod.Name, pod.Namespace)
		if err != nil {
			glog.Warningf("could not create source config for pod %s: %v", pod.Name, err)
		}
		sourceConfigs = append(sourceConfigs, *sourceConfig)
	}
	return sourceConfigs
}

func mapToSourceConfig(componentName string, url url.URL, ip, podId, namespaceId string) (*SourceConfig, error) {
	port := url.Port()
	whitelisted := url.Query().Get("whitelisted")
	podIdLabel := url.Query().Get("podIdLabel")
	namespaceIdLabel := url.Query().Get("namespaceIdLabel")
	containerNamelabel := url.Query().Get("containerNamelabel")
	podConfig := NewPodConfig(podId, namespaceId, podIdLabel, namespaceIdLabel, containerNamelabel)
	return newSourceConfig(componentName, ip, port, url.Path, whitelisted, podConfig)
}
