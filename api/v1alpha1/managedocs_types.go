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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SMTPSpec defines the desired state of SMTP configuration
type SMTPSpec struct {
	Endpoint           string   `json:"endpoint,omitempty"`
	Username           string   `json:"username,omitempty"`
	FromAddress        string   `json:"fromAddress,omitempty"`
	NotificationEmails []string `json:"notificationEmails,omitempty"`
}

// PagerSpec defines the desired state of Pager configuration
type PagerSpec struct {
	SOPEndpoint string `json:"sopEndpoint,omitempty"`
}

// ManagedFusionDeploymentSpec defines the desired state of ManagedFusionDeployment
type ManagedFusionDeploymentSpec struct {
	SMTP  SMTPSpec  `json:"smtp,omitempty"`
	Pager PagerSpec `json:"pager,omitempty"`
}

type ComponentState string

const (
	ComponentReady    ComponentState = "Ready"
	ComponentPending  ComponentState = "Pending"
	ComponentNotFound ComponentState = "NotFound"
	ComponentUnknown  ComponentState = "Unknown"
)

type ComponentStatus struct {
	State ComponentState `json:"state"`
}

type ComponentStatusMap struct {
	Prometheus   ComponentStatus `json:"prometheus"`
	Alertmanager ComponentStatus `json:"alertmanager"`
}

// ManagedFusionDeploymentStatus defines the observed state of ManagedFusionDeployment
type ManagedFusionDeploymentStatus struct {
	Components ComponentStatusMap `json:"components"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mfd

// ManagedFusionDeployment is the Schema for the ManagedFusionDeployment API
type ManagedFusionDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedFusionDeploymentSpec   `json:"spec,omitempty"`
	Status ManagedFusionDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedFusionDeploymentList contains a list of ManagedFusionDeployment
type ManagedFusionDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedFusionDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedFusionDeployment{}, &ManagedFusionDeploymentList{})
}
