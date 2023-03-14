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

	"github.com/go-logr/logr"
	opv1 "github.com/operator-framework/api/pkg/operators/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/templates"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	operatorGroupName = "managed-fusion-og"
	catalogSourceName = "managed-fusion-catsrc"
	subscriptionName  = "managed-fusion-sub"
)

// ManagedFusionOfferingReconciler reconciles a ManagedFusionOffering object
type ManagedFusionOfferingReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                   context.Context
	namespace             string
	managedFusionOffering *v1alpha1.ManagedFusionOffering
	ocsOperatorGroup      *opv1.OperatorGroup
	ocsCatalogSource      *opv1a1.CatalogSource
	ocsSubscription       *opv1a1.Subscription
}

//+kubebuilder:rbac:groups=misf.ibm.com,resources={managedfusionofferings,managedfusionofferings/finalizers},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=misf.ibm.com,resources=managedfusionofferings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="operators.coreos.com",resources={subscriptions,operatorgroups,catalogsources},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="operators.coreos.com",resources=installplans,verbs=get;list;watch;update;patch

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

	r.ocsOperatorGroup = &opv1.OperatorGroup{}
	r.ocsOperatorGroup.Name = operatorGroupName
	r.ocsOperatorGroup.Namespace = req.Namespace

	r.ocsCatalogSource = &opv1a1.CatalogSource{}
	r.ocsCatalogSource.Name = catalogSourceName
	r.ocsCatalogSource.Namespace = req.Namespace

	r.ocsSubscription = &opv1a1.Subscription{}
	r.ocsSubscription.Name = subscriptionName
	r.ocsSubscription.Namespace = req.Namespace
}

func (r *ManagedFusionOfferingReconciler) reconcilePhases() (reconcile.Result, error) {
	if err := r.reconcileOperatorGroup(); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileCatalogSource(); err != nil {
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
	r.Log.Info("Reconciling DF Offering Operator Group")
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.ocsOperatorGroup, func() error {
		if err := r.own(r.ocsOperatorGroup); err != nil {
			return err
		}
		r.ocsOperatorGroup.Spec.TargetNamespaces = []string{r.namespace}
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to create/update operatorGroup: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileCatalogSource() error {
	r.Log.Info("Reconciling DF Offering Source")
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.ocsCatalogSource, func() error {
		if err := r.own(r.ocsCatalogSource); err != nil {
			return err
		}
		desired := templates.OCSCatalogSource.DeepCopy()
		// A hook will be required to get catalog for a specific offering
		desired.Spec.Image = "registry.redhat.io/redhat/redhat-operator-index:v4.12"
		r.ocsCatalogSource.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to create/update catalogSource: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileSubscription() error {
	r.Log.Info("Reconciling DF Offering Subscription")
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.ocsSubscription, func() error {
		if err := r.own(r.ocsSubscription); err != nil {
			return err
		}
		desired := templates.OCSSubscription.DeepCopy()
		desired.Spec.CatalogSource = r.ocsCatalogSource.Name
		desired.Spec.CatalogSourceNamespace = r.ocsCatalogSource.Namespace
		// A hook will be required to get channel and startingCSV for a specific offering
		desired.Spec.Channel = "stable-4.12"
		desired.Spec.StartingCSV = "ocs-operator.v4.12.0"
		r.ocsSubscription.Spec = desired.Spec
		return nil

	})
	if err != nil {
		return fmt.Errorf("Failed to create/update subscription: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileInstallPlan() error {
	r.Log.Info("Reconciling DF Operator InstallPlan")
	ocsOperatorCSV := &opv1a1.ClusterServiceVersion{}
	ocsOperatorCSV.Name = r.ocsSubscription.Spec.StartingCSV
	ocsOperatorCSV.Namespace = r.namespace
	if err := r.get(ocsOperatorCSV); err != nil {
		if errors.IsNotFound(err) {
			var foundInstallPlan bool
			installPlans := &opv1a1.InstallPlanList{}
			if err := r.list(installPlans); err != nil {
				return err
			}
			for i, installPlan := range installPlans.Items {
				if findInSlice(installPlan.Spec.ClusterServiceVersionNames, ocsOperatorCSV.Name) {
					foundInstallPlan = true
					if installPlan.Spec.Approval == opv1a1.ApprovalManual &&
						!installPlan.Spec.Approved {
						installPlans.Items[i].Spec.Approved = true
						if err := r.update(&installPlans.Items[i]); err != nil {
							return err
						}
					}
				}
			}
			if !foundInstallPlan {
				r.Log.V(-1).Info("installPlan not found for %s CSV", ocsOperatorCSV.Name)
				return nil
			}
		}
		return fmt.Errorf("failed to get OCS Operator CSV: %v", err)
	}
	return nil
}

// This function is a placeholder for offering plugin integration
func pluginSetupWatches(controllerBuilder *builder.Builder) {
	controllerBuilder.
		Owns(&opv1.OperatorGroup{}).
		Owns(&opv1a1.CatalogSource{}).
		Owns(&opv1a1.Subscription{})
}

func findInSlice(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func (r *ManagedFusionOfferingReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *ManagedFusionOfferingReconciler) own(resource metav1.Object) error {
	// Ensure ManangedFusionOffering CR ownership on a resource
	if err := ctrl.SetControllerReference(r.managedFusionOffering, resource, r.Scheme); err != nil {
		return err
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) list(obj client.ObjectList) error {
	listOptions := client.InNamespace(r.namespace)
	return r.Client.List(r.ctx, obj, listOptions)
}

func (r *ManagedFusionOfferingReconciler) update(obj client.Object) error {
	return r.Client.Update(r.ctx, obj)
}
