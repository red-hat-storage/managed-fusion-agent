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

package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	managedFusionAnnotation = "misf.ibm.com/managedfusionoffering"
	ocsOperatorName         = "ocs-operator"
	mcgOperatorName         = "mcg-operator"
)

// ManagedFusionOfferingReconciler reconciles a ManagedFusionOffering object
type ManagedFusionOfferingReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                   context.Context
	managedFusionOffering *v1alpha1.ManagedFusionOffering
}

//+kubebuilder:rbac:groups=misf.ibm.com,resources=managedfusionofferings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=misf.ibm.com,resources=managedfusionofferings/status,verbs=get;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedFusionOfferingReconciler) SetupWithManager(mgr ctrl.Manager, ctrlOptions *controller.Options) error {
	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ManagedFusionOffering{})

	pluginSetupWatches(controllerBuilder)

	return controllerBuilder.Complete(r)
}

func (r *ManagedFusionOfferingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("req.Namespace", req.Namespace, "req.Name", req.Name)
	log.Info("Starting reconcile for ManangedFusionOffering")

	r.initReconciler(ctx, req)

	if err := r.get(r.managedFusionOffering); err != nil {
		return ctrl.Result{}, fmt.Errorf("Unable to get managedFusionOffering: %v", err)
	}

	if result, err := pluginReconcile(r); err != nil {
		return ctrl.Result{}, fmt.Errorf("An error was encountered during reconcile: %v", err)
	} else {
		return result, nil
	}
}

func (r *ManagedFusionOfferingReconciler) initReconciler(ctx context.Context, req ctrl.Request) {
	r.ctx = ctx

	r.managedFusionOffering = &v1alpha1.ManagedFusionOffering{}
	r.managedFusionOffering.Name = req.Name
	r.managedFusionOffering.Namespace = req.Namespace
}

// This function is a placeholder for offering plugin integration
func pluginReconcile(r *ManagedFusionOfferingReconciler) (ctrl.Result, error) {
	if _, found := r.managedFusionOffering.Spec.Config["usableCapacityInTiB"]; !found {
		return ctrl.Result{}, fmt.Errorf("%s CR does not contain usableCapacityInTiB entry", r.managedFusionOffering.Name)
	}
	if _, found := r.managedFusionOffering.Spec.Config["onboardingValidationKey"]; !found {
		return ctrl.Result{}, fmt.Errorf("%s CR does not contain onboardingValidationKey entry", r.managedFusionOffering.Name)
	}
	if err := reconcileCSV(r); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func reconcileCSV(r *ManagedFusionOfferingReconciler) error {
	r.Log.Info("Reconciling CSVs")
	csvList := opv1a1.ClusterServiceVersionList{}
	if err := r.list(&csvList); err != nil {
		return fmt.Errorf("unable to list csv resources: %v", err)
	}

	for index := range csvList.Items {
		csv := &csvList.Items[index]
		if strings.HasPrefix(csv.Name, ocsOperatorName) {
			if err := updateOCSCSV(r, csv); err != nil {
				return fmt.Errorf("Failed to update OCS CSV: %v", err)
			}
		} else if strings.HasPrefix(csv.Name, mcgOperatorName) {
			if err := updateMCGCSV(r, csv); err != nil {
				return fmt.Errorf("Failed to update MCG CSV: %v", err)
			}
		}
	}
	return nil
}

func updateOCSCSV(r *ManagedFusionOfferingReconciler, csv *opv1a1.ClusterServiceVersion) error {
	isChanged := false
	if _, found := csv.Annotations[managedFusionAnnotation]; !found {
		r.addManagedFusionOfferingAnnotation(csv, managedFusionAnnotation)
		isChanged = true
	}
	deployments := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs
	for i := range deployments {
		containers := deployments[i].Spec.Template.Spec.Containers
		for j := range containers {
			switch container := &containers[j]; container.Name {
			case "ocs-operator":
				resources := utils.GetResourceRequirements("ocs-operator")
				if !equality.Semantic.DeepEqual(container.Resources, resources) {
					container.Resources = resources
					isChanged = true
				}
			case "rook-ceph-operator":
				resources := utils.GetResourceRequirements("rook-ceph-operator")
				if !equality.Semantic.DeepEqual(container.Resources, resources) {
					container.Resources = resources
					isChanged = true
				}
			case "ocs-metrics-exporter":
				resources := utils.GetResourceRequirements("ocs-metrics-exporter")
				if !equality.Semantic.DeepEqual(container.Resources, resources) {
					container.Resources = resources
					isChanged = true
				}
			default:
				r.Log.V(-1).Info("Could not find resource requirement", "Resource", container.Name)
			}
		}
	}
	if isChanged {
		if err := r.update(csv); err != nil {
			return fmt.Errorf("Failed to update OCS CSV: %v", err)
		}
	}
	return nil
}

func updateMCGCSV(r *ManagedFusionOfferingReconciler, csv *opv1a1.ClusterServiceVersion) error {
	isChanged := false
	if _, found := csv.Annotations[managedFusionAnnotation]; !found {
		r.addManagedFusionOfferingAnnotation(csv, managedFusionAnnotation)
		isChanged = true
	}
	mcgDeployments := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs
	for i := range mcgDeployments {
		deployment := &mcgDeployments[i]
		// Disable noobaa operator by scaling down the replica of noobaa deploymnet
		// in MCG Operator CSV.
		if deployment.Name == "noobaa-operator" &&
			(deployment.Spec.Replicas == nil || *deployment.Spec.Replicas > 0) {
			zero := int32(0)
			deployment.Spec.Replicas = &zero
			isChanged = true
		}
	}
	if isChanged {
		if err := r.update(csv); err != nil {
			return fmt.Errorf("Failed to update MCG CSV: %v", err)
		}
	}
	return nil
}

// This function is a placeholder for offering plugin integration
func pluginSetupWatches(controllerBuilder *builder.Builder) {
	csvPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(client client.Object) bool {
				return strings.HasPrefix(client.GetName(), ocsOperatorName) ||
					strings.HasPrefix(client.GetName(), mcgOperatorName)
			},
		),
	)
	enqueueManagedFusionOfferingRequest := handler.EnqueueRequestsFromMapFunc(
		func(object client.Object) []reconcile.Request {
			annotations := object.GetAnnotations()
			if annotation, found := annotations[managedFusionAnnotation]; found {
				namespacedName := strings.Split(annotation, "/")
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: namespacedName[0],
						Name:      namespacedName[1],
					},
				}}
			}
			return []reconcile.Request{}
		},
	)
	controllerBuilder.
		Watches(
			&source.Kind{Type: &opv1a1.ClusterServiceVersion{}},
			enqueueManagedFusionOfferingRequest,
			csvPredicates,
		)
}

func (r *ManagedFusionOfferingReconciler) addManagedFusionOfferingAnnotation(obj metav1.Object, key string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}
	value := fmt.Sprintf("%s/%s", r.managedFusionOffering.Namespace, r.managedFusionOffering.Name)
	annotations[key] = value
}

func (r *ManagedFusionOfferingReconciler) list(obj client.ObjectList) error {
	listOptions := client.InNamespace(r.managedFusionOffering.Namespace)
	return r.Client.List(r.ctx, obj, listOptions)
}

func (r *ManagedFusionOfferingReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *ManagedFusionOfferingReconciler) update(obj client.Object) error {
	return r.Client.Update(r.ctx, obj)
}
