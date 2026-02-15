/*
Copyright 2026.

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

package v1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GuardPolicySpec defines the desired state of GuardPolicy.
type GuardPolicySpec struct {
	// NamespaceSelector selects which namespaces this policy applies to.
	// Empty selector means all namespaces.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// MaxReplicas is the maximum allowed replica count
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// MaxCPU is the maximum CPU limit per container (e.g., "500m" or "2")
	MaxCPU resource.Quantity `json:"maxCPU"`

	// MaxMemory is the maximum memory limit per container (e.g., "512Mi" or "2Gi")
	MaxMemory resource.Quantity `json:"maxMemory"`

	// EnforceLabels are required labels that must exist on Deployments
	// +optional
	EnforceLabels map[string]string `json:"enforceLabels,omitempty"`
}

// ViolationRecord tracks a specific violation
type ViolationRecord struct {
	// DeploymentName is the name of the violating Deployment
	DeploymentName string `json:"deploymentName"`

	// Namespace is the namespace of the Deployment
	Namespace string `json:"namespace"`

	// Issues is a list of violation descriptions
	Issues []string `json:"issues"`

	// CorrectedAt is when this violation was corrected
	CorrectedAt metav1.Time `json:"correctedAt"`
}

// GuardPolicyStatus defines the observed state of GuardPolicy.
type GuardPolicyStatus struct {
	// LastCorrectedTime is when the last correction was made
	// +optional
	LastCorrectedTime *metav1.Time `json:"lastCorrectedTime,omitempty"`

	// Violations is a list of current active violations
	// +optional
	Violations []ViolationRecord `json:"violations,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=gp
// +kubebuilder:printcolumn:name="Max Replicas",type=integer,JSONPath=`.spec.maxReplicas`
// +kubebuilder:printcolumn:name="Max CPU",type=string,JSONPath=`.spec.maxCPU`
// +kubebuilder:printcolumn:name="Max Memory",type=string,JSONPath=`.spec.maxMemory`
// +kubebuilder:printcolumn:name="Violations",type=integer,JSONPath=`.status.violations`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GuardPolicy is the Schema for the guardpolicies API
type GuardPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuardPolicySpec   `json:"spec,omitempty"`
	Status GuardPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GuardPolicyList contains a list of GuardPolicy.
type GuardPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GuardPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GuardPolicy{}, &GuardPolicyList{})
}
