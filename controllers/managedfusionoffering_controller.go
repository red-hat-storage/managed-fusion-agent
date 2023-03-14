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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	operatorGroupName = "managed-fusion-offering-og"
	catalogSourceName = "managed-fusion-offering-catalog"
	subscriptionName  = "managed-fusion-offering"
)

// ManagedFusionOfferingReconciler reconciles a ManagedFusionOffering object
type ManagedFusionOfferingReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	Ctx                   context.Context
	Namespace             string
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
		return ctrl.Result{}, fmt.Errorf("Unable to get managedFusionOffering: %v", err)
	}

	if result, err := r.reconcilePhases(); err != nil {
		return result, fmt.Errorf("an error was encountered during reconcilePhases: %v", err)
	}

	if result, err := pluginReconcile(r, r.managedFusionOffering); err != nil {
		return ctrl.Result{}, fmt.Errorf("An error was encountered during reconcile: %v", err)
	} else {
		return result, nil
	}
}

func (r *ManagedFusionOfferingReconciler) initReconciler(ctx context.Context, req ctrl.Request) {
	r.Ctx = ctx
	r.Namespace = req.Namespace

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

	if err := r.reconcileOperatorGroup(); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileCatalogSource(); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileSubscription(); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ManagedFusionOfferingReconciler) reconcileOperatorGroup() error {
	r.Log.Info(fmt.Sprintf("Reconciling operator group for %s offering deployment", r.managedFusionOffering.Spec.Kind))

	_, err := ctrl.CreateOrUpdate(r.Ctx, r.Client, r.operatorGroup, func() error {
		if err := r.own(r.operatorGroup); err != nil {
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

	_, err := ctrl.CreateOrUpdate(r.Ctx, r.Client, r.catalogSource, func() error {
		if err := r.own(r.catalogSource); err != nil {
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

	_, err := ctrl.CreateOrUpdate(r.Ctx, r.Client, r.subscription, func() error {
		if err := r.own(r.subscription); err != nil {
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

func (r *ManagedFusionOfferingReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.Ctx, key, obj)
}

func (r *ManagedFusionOfferingReconciler) own(resource metav1.Object) error {
	// Ensure ManangedFusionOffering CR ownership on a resource
	return ctrl.SetControllerReference(r.managedFusionOffering, resource, r.Scheme)
}

// All the below functions are placeholder for offering plugin integration

// This function is a placeholder for offering plugin integration
func pluginReconcile(reconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) (ctrl.Result, error) {
	if err := dfReconcile(reconciler, offering); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// This function is a placeholder for offering plugin integration
func pluginSetupWatches(controllerBuilder *builder.Builder) {
	dfSetupWatches(controllerBuilder)
}

// This function is a placeholder for offering plugin integration
func pluginGetDesiredOperatorGroupSpec(r *ManagedFusionOfferingReconciler) opv1.OperatorGroupSpec {
	return opv1.OperatorGroupSpec{
		TargetNamespaces: []string{r.Namespace},
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
