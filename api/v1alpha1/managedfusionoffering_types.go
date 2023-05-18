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

// +kubebuilder:validation:Enum=DF;DFC
type OfferingKind string

const (
	KindDataFoundation       OfferingKind = "DF"
	KindDataFoundationClient OfferingKind = "DFC"
)

// ManagedFusionOfferingSpec defines the desired state of ManagedFusionOffering
type ManagedFusionOfferingSpec struct {
	Kind    OfferingKind `json:"kind,omitempty"`
	Release string       `json:"release,omitempty"`
	Config  string       `json:"config,omitempty"`
}

// ManagedFusionOfferingStatus defines the observed state of ManagedFusionOffering
type ManagedFusionOfferingStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ManagedFusionOffering is the Schema for the managedfusionofferings API
type ManagedFusionOffering struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedFusionOfferingSpec   `json:"spec,omitempty"`
	Status ManagedFusionOfferingStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ManagedFusionOfferingList contains a list of ManagedFusionOffering
type ManagedFusionOfferingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedFusionOffering `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedFusionOffering{}, &ManagedFusionOfferingList{})
}
