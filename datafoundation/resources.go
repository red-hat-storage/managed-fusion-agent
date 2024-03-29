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

package datafoundation

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var resourceRequirements = map[string]corev1.ResourceRequirements{
	"ocs-operator": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("200m"),
			"memory": resource.MustParse("200Mi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("200m"),
			"memory": resource.MustParse("200Mi"),
		},
	},
	"rook-ceph-operator": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("300m"),
			"memory": resource.MustParse("250Mi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("300m"),
			"memory": resource.MustParse("250Mi"),
		},
	},
	"ocs-metrics-exporter": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("60m"),
			"memory": resource.MustParse("75Mi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("60m"),
			"memory": resource.MustParse("75Mi"),
		},
	},
	"mds": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("1500m"),
			"memory": resource.MustParse("8Gi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("1500m"),
			"memory": resource.MustParse("8Gi"),
		},
	},
	"mgr": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("1000m"),
			"memory": resource.MustParse("3Gi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("1000m"),
			"memory": resource.MustParse("3Gi"),
		},
	},
	"mon": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("1000m"),
			"memory": resource.MustParse("2Gi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("1000m"),
			"memory": resource.MustParse("2Gi"),
		},
	},
	"sds": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("1650m"),
			"memory": resource.MustParse("6Gi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("1650m"),
			"memory": resource.MustParse("6Gi"),
		},
	},
	"crashcollector": {
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse("50m"),
			"memory": resource.MustParse("80Mi"),
		},
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse("50m"),
			"memory": resource.MustParse("80Mi"),
		},
	},
}

func GetResourceRequirements(name string) corev1.ResourceRequirements {
	if req, ok := resourceRequirements[name]; ok {
		return req
	}
	panic(fmt.Sprintf("Resource requirement not found: %v", name))
}
