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

package templates

import (
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var params = map[string][]string{
	"match[]": {},
}

var K8sMetricsServiceMonitorTemplate = promv1.ServiceMonitor{
	Spec: promv1.ServiceMonitorSpec{
		Endpoints: []promv1.Endpoint{
			{
				Port:          "web",
				Path:          "/federate",
				Scheme:        "https",
				ScrapeTimeout: "1m",
				Interval:      "2m",
				HonorLabels:   true,
				MetricRelabelConfigs: []*promv1.RelabelConfig{
					{
						Action: "labeldrop",
						Regex:  "prometheus_replica",
					},
				},
				RelabelConfigs: []*promv1.RelabelConfig{
					{
						Action:      "replace",
						Regex:       "prometheus-k8s-.*",
						Replacement: "",
						SourceLabels: []string{
							"pod",
						},
						TargetLabel: "pod",
					},
				},
				TLSConfig: &promv1.TLSConfig{
					SafeTLSConfig: promv1.SafeTLSConfig{
						InsecureSkipVerify: true,
					},
				},
				Params:          params,
				BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
			},
		},
		NamespaceSelector: promv1.NamespaceSelector{
			MatchNames: []string{"openshift-monitoring"},
		},
		Selector: metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/component": "prometheus",
			},
		},
	},
}
