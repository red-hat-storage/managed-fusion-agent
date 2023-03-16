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
	opv1 "github.com/operator-framework/api/pkg/operators/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
	"k8s.io/apimachinery/pkg/api/errors"
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
	managedFusionAnnotationKey = "misf.ibm.com/managedfusionoffering"
	operatorGroupName          = "managed-fusion-og"
	subscriptionName           = "managed-fusion-sub"
)

// ManagedFusionOfferingReconciler reconciles a ManagedFusionOffering object
type ManagedFusionOfferingReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                   context.Context
	namespace             string
	managedFusionOffering *v1alpha1.ManagedFusionOffering
	operatorGroup         *opv1.OperatorGroup
	subscription          *opv1a1.Subscription
}

//+kubebuilder:rbac:groups=misf.ibm.com,resources={managedfusionofferings,managedfusionofferings/finalizers},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=misf.ibm.com,resources=managedfusionofferings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="operators.coreos.com",resources={subscriptions,operatorgroups},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="operators.coreos.com",resources=installplans,verbs=get;list;watch;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedFusionOfferingReconciler) SetupWithManager(mgr ctrl.Manager, ctrlOptions *controller.Options) error {
	installPlanPredicate := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(object client.Object) bool {
				owners := object.GetOwnerReferences()
				for _, owner := range owners {
					if strings.HasPrefix(owner.Name, subscriptionName) {
						return true
					}
				}
				return false
			},
		),
	)
	enqueueManagedFusionOfferingRequest := handler.EnqueueRequestsFromMapFunc(
		func(object client.Object) []reconcile.Request {
			annotations := object.GetAnnotations()
			if annotation, found := annotations[managedFusionAnnotationKey]; found {
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
	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ManagedFusionOffering{}).
		Owns(&opv1.OperatorGroup{}).
		Owns(&opv1a1.Subscription{}).
		Watches(
			&source.Kind{Type: &opv1a1.InstallPlan{}},
			enqueueManagedFusionOfferingRequest,
			installPlanPredicate,
		)

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

	if result, err := r.reconcilePhases(); err != nil {
		return result, fmt.Errorf("An error was encountered during reconcilePhases: %v", err)
	}

	if result, err := pluginReconcile(r); err != nil {
		return ctrl.Result{}, fmt.Errorf("An error was encountered during reconcile: %v", err)
	} else {
		return result, nil
	}
}

func (r *ManagedFusionOfferingReconciler) initReconciler(ctx context.Context, req ctrl.Request) {
	r.ctx = ctx
	r.namespace = req.Namespace

	r.managedFusionOffering = &v1alpha1.ManagedFusionOffering{}
	r.managedFusionOffering.Name = req.Name
	r.managedFusionOffering.Namespace = req.Namespace

	r.operatorGroup = &opv1.OperatorGroup{}
	r.operatorGroup.Namespace = req.Namespace

	r.subscription = &opv1a1.Subscription{}
	r.subscription.Namespace = req.Namespace
}

func (r *ManagedFusionOfferingReconciler) reconcilePhases() (reconcile.Result, error) {

	if err := r.reconcileOperatorGroup(); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileSubscription(); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileInstallPlan(); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// This function is a placeholder for offering plugin integration
func pluginReconcile(r *ManagedFusionOfferingReconciler) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *ManagedFusionOfferingReconciler) reconcileOperatorGroup() error {
	r.Log.Info(fmt.Sprintf("Reconciling operator group for %s offering deployment", r.managedFusionOffering.Spec.Kind))

	r.operatorGroup.Name = fmt.Sprintf("%s-%s-offering", operatorGroupName, strings.ToLower(string(r.managedFusionOffering.Spec.Kind)))
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.operatorGroup, func() error {
		if err := r.own(r.operatorGroup); err != nil {
			return err
		}
		desiredOperatorGroup := &opv1.OperatorGroupSpec{}
		pluginGetDesiredOperatorGroup(r, desiredOperatorGroup)
		r.operatorGroup.Spec = *desiredOperatorGroup
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to create/update OLM operator group: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileSubscription() error {
	r.Log.Info(fmt.Sprintf("Reconciling subscription for %s offering deployment", r.managedFusionOffering.Spec.Kind))

	r.subscription.Name = fmt.Sprintf("%s-%s-offering", subscriptionName, strings.ToLower(string(r.managedFusionOffering.Spec.Kind)))
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.subscription, func() error {
		if err := r.own(r.subscription); err != nil {
			return err
		}
		desiredSubscriptionSpec := &opv1a1.SubscriptionSpec{}
		pluginGetDesiredSubscriptionSpec(r, desiredSubscriptionSpec)
		desiredSubscriptionSpec.InstallPlanApproval = opv1a1.ApprovalManual
		r.subscription.Spec = desiredSubscriptionSpec
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to create/update OLM subscription: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileInstallPlan() error {
	r.Log.Info(fmt.Sprintf("Reconciling install plan for %s offering deployment", r.managedFusionOffering.Spec.Kind))

	operatorCSV := &opv1a1.ClusterServiceVersion{}
	operatorCSV.Name = r.subscription.Spec.StartingCSV
	operatorCSV.Namespace = r.namespace
	var desiredInstallPlan *opv1a1.InstallPlan
	if err := r.get(operatorCSV); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get CSV for %s offering deployment: %v", r.managedFusionOffering.Spec.Kind, err)
		}
		installPlans := &opv1a1.InstallPlanList{}
		if err := r.list(installPlans); err != nil {
			return err
		}
		for _, installPlan := range installPlans.Items {
			if utils.Contains(installPlan.Spec.ClusterServiceVersionNames, operatorCSV.Name) {
				desiredInstallPlan = &installPlan
				break
			}
		}
	}
	if desiredInstallPlan == nil {
		r.Log.V(-1).Info(fmt.Sprintf("install plan not found for %s CSV", operatorCSV.Name))
		return nil
	}
	if desiredInstallPlan.Spec.Approval == opv1a1.ApprovalManual &&
		!desiredInstallPlan.Spec.Approved {
		desiredInstallPlan.Spec.Approved = true
		r.addManagedFusionOfferingAnnotation(desiredInstallPlan)
		if err := r.update(desiredInstallPlan); err != nil {
			return err
		}
	}
	return nil
}

// This function is a placeholder for offering plugin integration
func pluginSetupWatches(controllerBuilder *builder.Builder) {}

// This function is a placeholder for offering plugin integration
func pluginGetDesiredOperatorGroup(r *ManagedFusionOfferingReconciler, operatorGroupSpec *opv1.OperatorGroupSpec) {
	switch r.managedFusionOffering.Spec.Release {
	case "4.12":
		operatorGroupSpec.TargetNamespaces = []string{r.namespace}
	}
}

// This function is a placeholder for offering plugin integration
func pluginGetDesiredSubscriptionSpec(r *ManagedFusionOfferingReconciler, subscriptionSpec *opv1a1.SubscriptionSpec) {
	switch r.managedFusionOffering.Spec.Release {
	case "4.12":
		subscriptionSpec.CatalogSource = "redhat-operators"
		subscriptionSpec.CatalogSourceNamespace = "openshift-marketplace"
		subscriptionSpec.Channel = "stable-4.12"
		subscriptionSpec.Package = "ocs-operator"
		subscriptionSpec.StartingCSV = "ocs-operator.v4.12.0"
	}
}

func (r *ManagedFusionOfferingReconciler) addManagedFusionOfferingAnnotation(obj metav1.Object) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}
	value := fmt.Sprintf("%s/%s", r.managedFusionOffering.Namespace, r.managedFusionOffering.Name)
	annotations[managedFusionAnnotationKey] = value
}

func (r *ManagedFusionOfferingReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *ManagedFusionOfferingReconciler) own(resource metav1.Object) error {
	// Ensure ManangedFusionOffering CR ownership on a resource
	return ctrl.SetControllerReference(r.managedFusionOffering, resource, r.Scheme)
}

func (r *ManagedFusionOfferingReconciler) list(obj client.ObjectList) error {
	listOptions := client.InNamespace(r.namespace)
	return r.Client.List(r.ctx, obj, listOptions)
}

func (r *ManagedFusionOfferingReconciler) update(obj client.Object) error {
	return r.Client.Update(r.ctx, obj)
}
