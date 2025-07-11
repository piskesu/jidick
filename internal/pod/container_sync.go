// Copyright 2025 The HuaTuo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pod

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"huatuo-bamai/internal/conf"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/utils/procfsutil"

	corev1 "k8s.io/api/core/v1"
)

const (
	kubeletReqTimeout = 5 * time.Second
)

func kubeletSyncContainers() error {
	podList, err := kubeletGetPodList()
	if err != nil {
		// ignore all errors and remain old containers.
		log.Infof("failed to get pod list, err: %v", err)
		return nil
	}

	type containerInfo struct {
		container       *corev1.Container
		containerStatus *corev1.ContainerStatus
		pod             *corev1.Pod
	}

	// map: ContainerID -> *containerInfo
	newContainers := make(map[string]*containerInfo)
	for i := range podList.Items {
		pod := &podList.Items[i]

		if !isRuningPod(pod) {
			continue
		}

		// map: name -> [*corev1.Container, *corev1.ContainerStatus]
		m := make(map[string][2]any)
		for i := range pod.Spec.Containers {
			container := &pod.Spec.Containers[i]
			m[container.Name] = [2]any{container, nil}
		}
		for i := range pod.Status.ContainerStatuses {
			containerStatus := &pod.Status.ContainerStatuses[i]
			if c, ok := m[containerStatus.Name]; ok {
				m[containerStatus.Name] = [2]any{c[0], containerStatus}
			}
		}

		for _, c := range m {
			containerStatus := c[1].(*corev1.ContainerStatus)
			containerID, err := parseContainerIDInPodStatus(containerStatus.ContainerID)
			if err != nil {
				return fmt.Errorf("failed to parse container id %s in pod %s status: %w", containerStatus.ContainerID, pod.Name, err)
			}

			newContainers[containerID] = &containerInfo{
				container:       c[0].(*corev1.Container),
				containerStatus: containerStatus,
				pod:             pod,
			}
		}
	}

	for k := range containers {
		// clear old containers which do not exist in newContainers.
		if _, ok := newContainers[k]; !ok {
			delete(containers, k)
			continue
		}

		// skip the existing containers
		delete(newContainers, k)
	}

	// update containers.
	for newContainerID, newContainerInfo := range newContainers {
		container := newContainerInfo.container
		containerStatus := newContainerInfo.containerStatus
		pod := newContainerInfo.pod

		if err := kubeletUpdateContainer(newContainerID, container, containerStatus, pod); err != nil {
			log.Infof("failed to update container %s in pod %s: %v", newContainerID, pod.Name, err)
			continue
		}
	}

	return nil
}

func kubeletGetPodList() (corev1.PodList, error) {
	kubeletPodListURL := conf.Get().Pod.KubeletPodListURL
	client := &http.Client{
		Timeout: kubeletReqTimeout,
	}
	if podList, err := kubeletDoRequest(client, kubeletPodListURL); err == nil {
		return podList, nil
	}

	// get the Cert of CA and Client kubelet-client-current.pem
	kubeletPodCACertPath := conf.Get().Pod.KubeletPodCACertPath
	kubeletPodClientCertDir := conf.Get().Pod.KubeletPodClientCertDir
	kubeletPodClientCertPath := filepath.Join(kubeletPodClientCertDir, "kubelet-client-current.pem")
	kubeletPodClientKeyPath := filepath.Join(kubeletPodClientCertDir, "kubelet-client-current.pem")

	// Load Client Cert and Key
	cert, err := tls.LoadX509KeyPair(kubeletPodClientCertPath, kubeletPodClientKeyPath)
	if err != nil {
		return corev1.PodList{}, fmt.Errorf("loading client key pair: %w", err)
	}

	caCert, err := os.ReadFile(kubeletPodCACertPath)
	if err != nil {
		return corev1.PodList{}, fmt.Errorf("reading CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            caCertPool,
			InsecureSkipVerify: true, // #nosec G402
		},
	}

	kubeletPodListURL = conf.Get().Pod.KubeletPodListHTTPSURL
	return kubeletDoRequest(client, kubeletPodListURL)
}

func kubeletDoRequest(client *http.Client, kubeletPodListURL string) (corev1.PodList, error) {
	podList := corev1.PodList{}
	req, err := http.NewRequest(http.MethodGet, kubeletPodListURL, http.NoBody)
	if err != nil {
		return podList, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return podList, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return podList, fmt.Errorf("http: %s, status: %d, body: %s", kubeletPodListURL, resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, &podList); err != nil {
		return podList, fmt.Errorf("http: %s, Unmarshal: %w, body: %s", kubeletPodListURL, err, string(body))
	}

	return podList, nil
}

// func updateKubeletContainer(containerID string, container *corev1.Container, containerStatus *corev1.ContainerStatus, pod *corev1.Pod, css map[string]uint64) error {
func kubeletUpdateContainer(containerID string, container *corev1.Container, containerStatus *corev1.ContainerStatus, pod *corev1.Pod) error {
	// container type
	containerType, err := parseContainerType(container, pod)
	if err != nil {
		return fmt.Errorf("failed to parse type: %w", err)
	}

	// container qos
	containerQos, err := parseContainerQos(containerType, pod)
	if err != nil {
		return fmt.Errorf("failed to parse qos: %w", err)
	}

	hostname, err := parseContainerHostname(containerType, pod)
	if err != nil {
		return fmt.Errorf("failed to parse hostname: %w", err)
	}

	// fetch InitPid
	initPid, err := containerInitPid(containerID)
	if err != nil {
		return fmt.Errorf("failed to get InitPid: %w", err)
	}

	// net namespace
	nsInode, err := procfsutil.NetNSInodeByPid(initPid)
	if err != nil {
		return fmt.Errorf("failed to get net namespace inode by pid: %w", err)
	}

	labels, err := parseContainerLabels(containerType, pod)
	if err != nil {
		return fmt.Errorf("failed to parse container labels: %w", err)
	}

	startedAt, err := time.Parse(time.RFC3339, containerStatus.State.Running.StartedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to parse StartedAt %s: %w", containerStatus.State.Running.StartedAt, err)
	}

	css, err := parseContainerCSS(containerID)
	if err != nil {
		return fmt.Errorf("failed to parse container css: %w", err)
	}

	containers[containerID] = &Container{
		ID:                containerID,
		Name:              container.Name,
		Hostname:          hostname,
		Type:              containerType,
		Qos:               containerQos,
		IPAddress:         parseContainerIPAddress(pod),
		NetNamespaceInode: nsInode,
		InitPid:           initPid,
		CgroupSuffix:      containerCgroupSuffix(containerID, pod),
		CSS:               css,
		StartedAt:         startedAt,
		SyncedAt:          time.Now(),
		lifeResouces:      make(map[string]any),
		Labels:            labels,
	}

	// create container life resources
	createContainerLifeResources(containers[containerID])

	log.Infof("update container %#v", containers[containerID])
	return nil
}

func parseContainerIDInPodStatus(data string) (string, error) {
	// containerID example:
	//
	// "containerID": "docker://06ae8891e7e9b80f353e07116980f93a357fb3f239c09894de73b2e74121c94f",
	// "containerID": "containerd://0ac95a0f051b5094551a02b584414773dc24f5b2f1e4ea768460a787f762e279"
	parts := strings.Split(strings.Trim(data, "\""), "://")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid container id: %s", data)
	}

	// init the container provider
	initContainerProviderEnv(parts[0])

	return parts[1], nil
}

func parseContainerIPAddress(pod *corev1.Pod) string {
	// podIP example:
	//
	//	"status": {
	//		...
	//		"hostIP": "10.74.164.13",
	//		"podIP": "10.74.164.13",
	//		"podIPs": [
	//			{
	//				"ip": "10.74.164.13"
	//			}
	//		],
	//		...
	//	},
	return pod.Status.PodIP
}

func isRuningPod(pod *corev1.Pod) bool {
	// running pod example:
	//
	//  "status": {
	//		...
	//		"phase": "Running",
	//		...
	//	    "containerStatuses": [
	//			{
	//				"name": "taxi-invoice-center-zjy",
	//				"state": {
	//					"running": {
	//						"startedAt": "2024-05-28T03:10:30Z"
	//					},
	//				...
	//				},
	//				...
	//			},
	//			{
	//				"name": "agent-taxi-invoice-center-zjy",
	//				"state": {
	//					"running": {
	//						"startedAt": "2024-05-28T03:10:30Z"
	//					},
	//				...
	//				},
	//				...
	//			},
	//}

	// The Pod has been bound to a node, and all of the containers have been created.
	// At least one container is still running, or is in the process of starting or
	// restarting.
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	// all containers are running.
	for i := range pod.Status.ContainerStatuses {
		containerStatus := &pod.Status.ContainerStatuses[i]
		if containerStatus.State.Running == nil {
			return false
		}
	}

	return true
}
