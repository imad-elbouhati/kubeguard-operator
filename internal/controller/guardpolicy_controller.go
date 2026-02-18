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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	platformv1 "github.com/imad-elbouhati/kubeguard-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GuardPolicyReconciler reconciles a GuardPolicy object
type GuardPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// DriftReport captures detected violations
type DriftReport struct {
	Issues            []string
	ReplicasViolation bool
	TargetReplicas    int32
	CPUViolations     map[string]resource.Quantity // containerName -> target CPU
	MemoryViolations  map[string]resource.Quantity // containerName -> target Memory
	MissingLabels     map[string]string
}

func (d *DriftReport) HasViolations() bool {
	return len(d.Issues) > 0
}

// +kubebuilder:rbac:groups=platform.demo,resources=guardpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.demo,resources=guardpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.demo,resources=guardpolicies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GuardPolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *GuardPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the GuardPolicy
	policy := &platformv1.GuardPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if errors.IsNotFound(err) {
			// Object deleted, nothing to do
			logger.Info("GuardPolicy not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch GuardPolicy")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling GuardPolicy", "policy", policy.Name)

	// 2. Get applicable namespaces
	namespaces, err := r.getApplicableNamespaces(ctx, policy)
	if err != nil {
		logger.Error(err, "failed to get applicable namespaces")
		return ctrl.Result{}, err
	}

	logger.Info("Policy applies to namespaces", "count", len(namespaces), "namespaces", namespaces)

	// 3. Find all Deployments in those namespaces
	violations := []platformv1.ViolationRecord{}
	corrected := false

	for _, ns := range namespaces {
		deployments := &appsv1.DeploymentList{}
		if err := r.List(ctx, deployments, client.InNamespace(ns)); err != nil {
			logger.Error(err, "failed to list deployments", "namespace", ns)
			continue
		}

		logger.Info("Found deployments in namespace", "namespace", ns, "count", len(deployments.Items))

		// 4. Check each Deployment for violations
		for i := range deployments.Items {
			deployment := &deployments.Items[i]
			drift := r.detectDrift(deployment, policy)

			if drift.HasViolations() {
				logger.Info("Drift detected",
					"deployment", deployment.Name,
					"namespace", deployment.Namespace,
					"violations", drift.Issues)

				// 5. Correct the Deployment
				if err := r.correctDeployment(ctx, deployment, policy, drift); err != nil {
					logger.Error(err, "failed to correct deployment",
						"deployment", deployment.Name,
						"namespace", deployment.Namespace)
					continue
				}

				corrected = true
				violations = append(violations, platformv1.ViolationRecord{
					DeploymentName: deployment.Name,
					Namespace:      deployment.Namespace,
					Issues:         drift.Issues,
					CorrectedAt:    metav1.Now(),
				})

				// 6. Emit event
				r.recordEvent(policy, deployment, drift)
			}
		}
	}

	// 7. Update GuardPolicy status
	if err := r.updateStatus(ctx, policy, violations, corrected); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciliation complete",
		"policy", policy.Name,
		"violations", len(violations),
		"corrected", corrected)

	// Requeue after 5 minutes for continuous enforcement
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// detectDrift checks if a Deployment violates the policy
func (r *GuardPolicyReconciler) detectDrift(
	deployment *appsv1.Deployment,
	policy *platformv1.GuardPolicy,
) *DriftReport {
	report := &DriftReport{
		Issues:           []string{},
		CPUViolations:    make(map[string]resource.Quantity),
		MemoryViolations: make(map[string]resource.Quantity),
		MissingLabels:    make(map[string]string),
	}

	// Check replica count
	if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas > policy.Spec.MaxReplicas {
		report.ReplicasViolation = true
		report.TargetReplicas = policy.Spec.MaxReplicas
		report.Issues = append(report.Issues,
			fmt.Sprintf("replicas %d exceeds max %d",
				*deployment.Spec.Replicas, policy.Spec.MaxReplicas))
	}

	// Check container resources
	for _, container := range deployment.Spec.Template.Spec.Containers {
		// Check CPU
		if container.Resources.Limits != nil {
			cpu := container.Resources.Limits.Cpu()
			if cpu != nil && cpu.Cmp(policy.Spec.MaxCPU) > 0 {
				report.CPUViolations[container.Name] = policy.Spec.MaxCPU
				report.Issues = append(report.Issues,
					fmt.Sprintf("container %s CPU %s exceeds max %s",
						container.Name, cpu.String(), policy.Spec.MaxCPU.String()))
			}
		}

		// Check Memory
		if container.Resources.Limits != nil {
			memory := container.Resources.Limits.Memory()
			if memory != nil && memory.Cmp(policy.Spec.MaxMemory) > 0 {
				report.MemoryViolations[container.Name] = policy.Spec.MaxMemory
				report.Issues = append(report.Issues,
					fmt.Sprintf("container %s memory %s exceeds max %s",
						container.Name, memory.String(), policy.Spec.MaxMemory.String()))
			}
		}
	}

	// Check required labels
	if policy.Spec.EnforceLabels != nil {
		for key, value := range policy.Spec.EnforceLabels {
			if deployment.Labels == nil || deployment.Labels[key] != value {
				report.MissingLabels[key] = value
				report.Issues = append(report.Issues,
					fmt.Sprintf("missing required label %s=%s", key, value))
			}
		}
	}

	return report
}

// correctDeployment applies corrections to a Deployment
func (r *GuardPolicyReconciler) correctDeployment(
	ctx context.Context,
	deployment *appsv1.Deployment,
	policy *platformv1.GuardPolicy,
	drift *DriftReport,
) error {
	logger := log.FromContext(ctx)

	// Create a copy for patching
	corrected := deployment.DeepCopy()

	// Apply corrections
	if drift.ReplicasViolation {
		corrected.Spec.Replicas = &drift.TargetReplicas
		logger.Info("correcting replicas",
			"deployment", deployment.Name,
			"namespace", deployment.Namespace,
			"from", *deployment.Spec.Replicas,
			"to", drift.TargetReplicas)
	}

	// Correct container resources
	for i := range corrected.Spec.Template.Spec.Containers {
		container := &corrected.Spec.Template.Spec.Containers[i]

		// Apply CPU corrections
		if targetCPU, found := drift.CPUViolations[container.Name]; found {
			if container.Resources.Limits == nil {
				container.Resources.Limits = corev1.ResourceList{}
			}
			container.Resources.Limits[corev1.ResourceCPU] = targetCPU
			logger.Info("correcting CPU",
				"deployment", deployment.Name,
				"namespace", deployment.Namespace,
				"container", container.Name,
				"to", targetCPU.String())
		}

		// Apply Memory corrections
		if targetMem, found := drift.MemoryViolations[container.Name]; found {
			if container.Resources.Limits == nil {
				container.Resources.Limits = corev1.ResourceList{}
			}
			container.Resources.Limits[corev1.ResourceMemory] = targetMem
			logger.Info("correcting memory",
				"deployment", deployment.Name,
				"namespace", deployment.Namespace,
				"container", container.Name,
				"to", targetMem.String())
		}
	}

	// Apply missing labels
	if len(drift.MissingLabels) > 0 {
		if corrected.Labels == nil {
			corrected.Labels = make(map[string]string)
		}
		for key, value := range drift.MissingLabels {
			corrected.Labels[key] = value
			logger.Info("adding required label",
				"deployment", deployment.Name,
				"namespace", deployment.Namespace,
				"label", fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Add annotation to track correction
	if corrected.Annotations == nil {
		corrected.Annotations = make(map[string]string)
	}
	corrected.Annotations["platform.demo/last-corrected"] = time.Now().Format(time.RFC3339)
	corrected.Annotations["platform.demo/corrected-by"] = policy.Name

	// Patch the Deployment
	if err := r.Patch(ctx, corrected, client.MergeFrom(deployment)); err != nil {
		return fmt.Errorf("failed to patch deployment: %w", err)
	}

	logger.Info("successfully corrected deployment",
		"deployment", deployment.Name,
		"namespace", deployment.Namespace)

	return nil
}

// getApplicableNamespaces returns namespaces that match the policy selector
func (r *GuardPolicyReconciler) getApplicableNamespaces(
	ctx context.Context,
	policy *platformv1.GuardPolicy,
) ([]string, error) {
	// If no selector, apply to all namespaces
	if policy.Spec.NamespaceSelector == nil {
		nsList := &corev1.NamespaceList{}
		if err := r.List(ctx, nsList); err != nil {
			return nil, err
		}

		namespaces := []string{}
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
		return namespaces, nil
	}

	// Apply label selector
	selector, err := metav1.LabelSelectorAsSelector(policy.Spec.NamespaceSelector)
	if err != nil {
		return nil, err
	}

	nsList := &corev1.NamespaceList{}
	if err := r.List(ctx, nsList, &client.ListOptions{
		LabelSelector: selector,
	}); err != nil {
		return nil, err
	}

	namespaces := []string{}
	for _, ns := range nsList.Items {
		namespaces = append(namespaces, ns.Name)
	}

	return namespaces, nil
}

// policyAppliesToNamespace checks if a policy applies to a specific namespace
func (r *GuardPolicyReconciler) policyAppliesToNamespace(
	ctx context.Context,
	policy *platformv1.GuardPolicy,
	namespace string,
) bool {
	// No selector = applies everywhere
	if policy.Spec.NamespaceSelector == nil {
		return true
	}

	// Fetch the namespace and check labels
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		return false
	}

	selector, err := metav1.LabelSelectorAsSelector(policy.Spec.NamespaceSelector)
	if err != nil {
		return false
	}

	return selector.Matches(labels.Set(ns.Labels))
}

// updateStatus updates the GuardPolicy status
func (r *GuardPolicyReconciler) updateStatus(
	ctx context.Context,
	policy *platformv1.GuardPolicy,
	violations []platformv1.ViolationRecord,
	corrected bool,
) error {
	policy.Status.Violations = violations

	if corrected {
		now := metav1.Now()
		policy.Status.LastCorrectedTime = &now
	}

	// Update conditions
	condition := metav1.Condition{
		Type:               "Reconciled",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ReconciliationSucceeded",
		Message:            fmt.Sprintf("Found and corrected %d violations", len(violations)),
	}

	if len(violations) == 0 {
		condition.Message = "No violations detected"
	}

	// Set or update the condition
	setStatusCondition(&policy.Status.Conditions, condition)

	return r.Status().Update(ctx, policy)
}

// setStatusCondition sets the given condition with the given status,
// reason and message on a resource.
func setStatusCondition(conditions *[]metav1.Condition, newCondition metav1.Condition) {
	if conditions == nil {
		conditions = &[]metav1.Condition{}
	}
	existingCondition := findStatusCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = metav1.Now()
		*conditions = append(*conditions, newCondition)
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		existingCondition.LastTransitionTime = metav1.Now()
	}

	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
}

// findStatusCondition finds the conditionType in conditions.
func findStatusCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// recordEvent emits a Kubernetes event
func (r *GuardPolicyReconciler) recordEvent(
	policy *platformv1.GuardPolicy,
	deployment *appsv1.Deployment,
	drift *DriftReport,
) {
	message := fmt.Sprintf("Corrected %d violations in %s/%s: %s",
		len(drift.Issues),
		deployment.Namespace,
		deployment.Name,
		strings.Join(drift.Issues, "; "))

	r.Recorder.Event(policy, corev1.EventTypeNormal, "ViolationCorrected", message)
}

// deploymentToGuardPolicies maps a Deployment event to all GuardPolicies that might apply
func (r *GuardPolicyReconciler) deploymentToGuardPolicies(ctx context.Context, obj client.Object) []reconcile.Request {
	deployment := obj.(*appsv1.Deployment)

	// List all GuardPolicies
	policyList := &platformv1.GuardPolicyList{}
	if err := r.List(ctx, policyList); err != nil {
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}
	for _, policy := range policyList.Items {
		// Check if this policy applies to the deployment's namespace
		if r.policyAppliesToNamespace(ctx, &policy, deployment.Namespace) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: policy.Name,
				},
			})
		}
	}

	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *GuardPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1.GuardPolicy{}).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.deploymentToGuardPolicies),
		).
		Complete(r)
}
