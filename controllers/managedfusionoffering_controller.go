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
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	operatorGroupName = "managed-fusion-offering-og"
	catalogSourceName = "managed-fusion-offering-catalog"
	subscriptionName  = "managed-fusion-offering"
)

// ManagedFusionOfferingReconciler reconciles a ManagedFusionOffering object
type ManagedFusionOfferingReconciler struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                   context.Context
	namespace             string
	managedFusionOffering *v1alpha1.ManagedFusionOffering
	operatorGroup         *opv1.OperatorGroup
	catalogSource         *opv1a1.CatalogSource
	subscription          *opv1a1.Subscription
}

//+kubebuilder:rbac:groups=misf.ibm.com,resources={managedfusionofferings,managedfusionofferings/finalizers},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=misf.ibm.com,resources=managedfusionofferings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="operators.coreos.com",resources={subscriptions,operatorgroups,catalogsources},verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedFusionOfferingReconciler) SetupWithManager(mgr ctrl.Manager, ctrlOptions *controller.Options) error {
	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ManagedFusionOffering{}).
		Owns(&opv1.OperatorGroup{}).
		Owns(&opv1a1.CatalogSource{}).
		Owns(&opv1a1.Subscription{})

	pluginSetupWatches(controllerBuilder)

	return controllerBuilder.Complete(r)
}

func (r *ManagedFusionOfferingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("req.Namespace", req.Namespace, "req.Name", req.Name)
	log.Info("Starting reconcile for ManangedFusionOffering")

	r.initReconciler(ctx, req)

	if err := r.get(r.managedFusionOffering); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to get managedFusionOffering: %v", err)
	}

	if result, err := r.reconcilePhases(); err != nil {
		return result, fmt.Errorf("an error was encountered during reconcilePhases: %v", err)
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
	r.operatorGroup.Name = operatorGroupName
	r.operatorGroup.Namespace = req.Namespace

	r.catalogSource = &opv1a1.CatalogSource{}
	r.catalogSource.Name = catalogSourceName
	r.catalogSource.Namespace = req.Namespace

	r.subscription = &opv1a1.Subscription{}
	r.subscription.Name = subscriptionName
	r.subscription.Namespace = req.Namespace
}

func (r *ManagedFusionOfferingReconciler) reconcilePhases() (reconcile.Result, error) {
	if !r.managedFusionOffering.DeletionTimestamp.IsZero() {
		if ready, err := pluginIsReadyToBeRemoved(r, r.managedFusionOffering); err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"failed to validate if %s (%s) can be removed %v",
				r.managedFusionOffering.Name,
				r.managedFusionOffering.Namespace,
				err,
			)

		} else if ready {
			if removed := utils.RemoveFinalizer(r.managedFusionOffering, managedFusionFinalizer); removed {
				r.Log.Info(fmt.Sprintf("removing finalizer from %s secret", managedFusionSecretName))
				if err := r.update(r.managedFusionOffering); err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from %s secret: %v", managedFusionSecretName, err)
				}
			}
		}
	} else if r.managedFusionOffering.UID != "" {
		if added := utils.AddFinalizer(r.managedFusionOffering, managedFusionFinalizer); added {
			r.Log.V(-1).Info(fmt.Sprintf("finalizer missing on the %s secret resource, adding...", managedFusionSecretName))
			if err := r.update(r.managedFusionOffering); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update %s secret with finalizer: %v", managedFusionSecretName, err)
			}
		}

		if err := r.reconcileOperatorGroup(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileCatalogSource(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileSubscription(); err != nil {
			return ctrl.Result{}, err
		}

		if result, err := pluginReconcile(r, r.managedFusionOffering); err != nil {
			return ctrl.Result{}, fmt.Errorf("an error was encountered during reconcile: %v", err)
		} else {
			return result, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *ManagedFusionOfferingReconciler) reconcileOperatorGroup() error {
	r.Log.Info(fmt.Sprintf("Reconciling operator group for %s offering deployment", r.managedFusionOffering.Spec.Kind))

	_, err := r.CreateOrUpdate(r.operatorGroup, func() error {
		if err := r.own(r.operatorGroup, true); err != nil {
			return err
		}
		r.operatorGroup.Spec = pluginGetDesiredOperatorGroupSpec(r)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create/update OLM operator group: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileCatalogSource() error {
	r.Log.Info(fmt.Sprintf("Reconciling catalog source for %s offering deployment", r.managedFusionOffering.Spec.Kind))

	_, err := r.CreateOrUpdate(r.catalogSource, func() error {
		if err := r.own(r.catalogSource, true); err != nil {
			return err
		}
		desiredCatalogSourceSpec := pluginGetDesiredCatalogSourceSpec(r)
		r.catalogSource.Spec = desiredCatalogSourceSpec
		r.catalogSource.Spec.SourceType = opv1a1.SourceTypeGrpc
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create/update OLM catalog source: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileSubscription() error {
	r.Log.Info(fmt.Sprintf("Reconciling subscription for %s offering deployment", r.managedFusionOffering.Spec.Kind))

	_, err := r.CreateOrUpdate(r.subscription, func() error {
		if err := r.own(r.subscription, true); err != nil {
			return err
		}
		desiredSubscriptionSpec := pluginGetDesiredSubscriptionSpec(r)
		r.subscription.Spec = desiredSubscriptionSpec
		r.subscription.Spec.CatalogSource = r.catalogSource.Name
		r.subscription.Spec.CatalogSourceNamespace = r.catalogSource.Namespace
		r.subscription.Spec.InstallPlanApproval = opv1a1.ApprovalAutomatic
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create/update OLM subscription: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) list(obj client.ObjectList, listOptions ...client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOptions...)
}

func (r *ManagedFusionOfferingReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *ManagedFusionOfferingReconciler) update(obj client.Object) error {
	return r.Client.Update(r.ctx, obj)
}

func (r *ManagedFusionOfferingReconciler) own(res client.Object, isController bool) error {
	return utils.AddOwnerReference(r.managedFusionOffering, res, r.Scheme, isController)
}

func (r *ManagedFusionOfferingReconciler) CreateOrUpdate(obj client.Object, f controllerutil.MutateFn) (controllerutil.OperationResult, error) {
	return ctrl.CreateOrUpdate(r.ctx, r.Client, obj, f)
}

func (r *ManagedFusionOfferingReconciler) GetAndUpdate(obj client.Object, f controllerutil.MutateFn) error {
	if f == nil {
		return fmt.Errorf("MutateFn cannot be nil")
	}
	if err := r.get(obj); err != nil {
		return err
	}
	key := client.ObjectKeyFromObject(obj)
	existing := obj.DeepCopyObject()
	if err := f(); err != nil {
		return err
	}
	// NamespacedName before and after mutateFunc should remain same
	if key != client.ObjectKeyFromObject(obj) {
		return fmt.Errorf("MutateFn cannot mutate object name and/or object namespace")
	}
	if equality.Semantic.DeepEqual(existing, obj) {
		return nil
	}
	if err := r.update(obj); err != nil {
		return err
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) getCSVByPrefix(name string) (*opv1a1.ClusterServiceVersion, error) {
	csvList := opv1a1.ClusterServiceVersionList{}
	if err := r.list(&csvList, client.InNamespace(r.namespace)); err != nil {
		return nil, fmt.Errorf("unable to list csv resources: %v", err)
	}

	for index := range csvList.Items {
		candidate := &csvList.Items[index]
		if strings.HasPrefix(candidate.Name, name) {
			return candidate, nil
		}
	}
	return nil, errors.NewNotFound(opv1a1.Resource("csv"), fmt.Sprintf("unable to find a csv prefixed with %s", name))
}

// All the below functions are placeholder for offering plugin integration

func pluginIsReadyToBeRemoved(reconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) (bool, error) {
	return dfIsReadyToBeRemoved(reconciler, offering)
}

// This function is a placeholder for offering plugin integration
func pluginReconcile(reconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) (ctrl.Result, error) {
	return dfReconcile(reconciler, offering)
}

// This function is a placeholder for offering plugin integration
func pluginSetupWatches(controllerBuilder *builder.Builder) {
	dfSetupWatches(controllerBuilder)
}

// This function is a placeholder for offering plugin integration
func pluginGetDesiredOperatorGroupSpec(r *ManagedFusionOfferingReconciler) opv1.OperatorGroupSpec {
	return opv1.OperatorGroupSpec{
		TargetNamespaces: []string{r.namespace},
	}
}

// This function is a placeholder for offering plugin integration
func pluginGetDesiredCatalogSourceSpec(r *ManagedFusionOfferingReconciler) opv1a1.CatalogSourceSpec {
	return opv1a1.CatalogSourceSpec{
		Image: "registry.redhat.io/redhat/redhat-operator-index:v4.12",
	}
}

// This function is a placeholder for offering plugin integration
func pluginGetDesiredSubscriptionSpec(r *ManagedFusionOfferingReconciler) *opv1a1.SubscriptionSpec {
	return &opv1a1.SubscriptionSpec{
		Channel: "stable-4.12",
		Package: "ocs-operator",
	}
}
