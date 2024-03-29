/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var resourceRequirements = map[string]corev1.ResourceRequirements{
	"prometheus": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("400m"),
			"memory": resource.MustParse("250Mi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("400m"),
			"memory": resource.MustParse("250Mi"),
		},
	},
	"alertmanager": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("100m"),
			"memory": resource.MustParse("200Mi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("100m"),
			"memory": resource.MustParse("200Mi"),
		},
	},
	"csi-provisioner": {
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("85Mi"),
			"cpu":    resource.MustParse("15m"),
		},
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("85Mi"),
			"cpu":    resource.MustParse("15m"),
		},
	},

	"csi-resizer": {
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("55Mi"),
			"cpu":    resource.MustParse("10m"),
		},
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("55Mi"),
			"cpu":    resource.MustParse("10m"),
		},
	},

	"csi-attacher": {
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("45Mi"),
			"cpu":    resource.MustParse("10m"),
		},
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("45Mi"),
			"cpu":    resource.MustParse("10m"),
		},
	},

	"csi-snapshotter": {
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("35Mi"),
			"cpu":    resource.MustParse("10m"),
		},
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("35Mi"),
			"cpu":    resource.MustParse("10m"),
		},
	},

	"csi-rbdplugin": {
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("270Mi"),
			"cpu":    resource.MustParse("25m"),
		},
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("270Mi"),
			"cpu":    resource.MustParse("25m"),
		},
	},

	"liveness-prometheus": {
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("30Mi"),
			"cpu":    resource.MustParse("10m"),
		},
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("30Mi"),
			"cpu":    resource.MustParse("10m"),
		},
	},

	"driver-registrar": {
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("25Mi"),
			"cpu":    resource.MustParse("10m"),
		},
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("25Mi"),
			"cpu":    resource.MustParse("10m"),
		},
	},

	"csi-cephfsplugin": {
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("160Mi"),
			"cpu":    resource.MustParse("20m"),
		},
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("160Mi"),
			"cpu":    resource.MustParse("20m"),
		},
	},

	"kube-rbac-proxy": {
		Limits: corev1.ResourceList{
			"memory": resource.MustParse("30Mi"),
			"cpu":    resource.MustParse("50m"),
		},
		Requests: corev1.ResourceList{
			"memory": resource.MustParse("30Mi"),
			"cpu":    resource.MustParse("50m"),
		},
	},
}

func GetResourceRequirements(name string) corev1.ResourceRequirements {
	if req, ok := resourceRequirements[name]; ok {
		return req
	}
	panic(fmt.Sprintf("Resource requirement not found: %v", name))
}
