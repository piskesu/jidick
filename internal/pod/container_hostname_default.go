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

//go:build !didi

package pod

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
)

func parseContainerHostname(typ ContainerType, pod *corev1.Pod) (string, error) {
	if typ == ContainerTypeDaemonSet {
		hostname, err := os.Hostname()
		if err != nil {
			return "", fmt.Errorf("os.Hostname: %w", err)
		}
		return hostname, nil
	}

	hostname := pod.Spec.Hostname
	if hostname == "" {
		hostname = pod.Name
	}

	return hostname, nil
}
