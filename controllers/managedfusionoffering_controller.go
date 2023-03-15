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
	"github.com/red-hat-storage/managed-fusion-agent/utils"
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

type installationConfig struct {
	displayName string
	publisher   string
	indexImage  string
	channel     string
	startingCSV string
	packageName string
}

// ManagedFusionOfferingReconciler reconciles a ManagedFusionOffering object
type ManagedFusionOfferingReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                   context.Context
	namespace             string
	managedFusionOffering *v1alpha1.ManagedFusionOffering
	operatorGroup         *opv1.OperatorGroup
	catalogSource         *opv1a1.CatalogSource
	subscription          *opv1a1.Subscription
	installConfig         *installationConfig
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

	r.operatorGroup = &opv1.OperatorGroup{}
	r.operatorGroup.Name = operatorGroupName
	r.operatorGroup.Namespace = req.Namespace

	r.catalogSource = &opv1a1.CatalogSource{}
	r.catalogSource.Name = catalogSourceName
	r.catalogSource.Namespace = req.Namespace

	r.subscription = &opv1a1.Subscription{}
	r.subscription.Name = subscriptionName
	r.subscription.Namespace = req.Namespace

	r.installConfig = &installationConfig{}
}

func (r *ManagedFusionOfferingReconciler) reconcilePhases() (reconcile.Result, error) {
	if err := pluginGetInstallalConfig(r); err != nil {
		return ctrl.Result{}, err
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
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.operatorGroup, func() error {
		if err := r.own(r.operatorGroup); err != nil {
			return err
		}
		r.operatorGroup.Spec.TargetNamespaces = []string{r.namespace}
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to create/update OLM operator group: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileCatalogSource() error {
	r.Log.Info(fmt.Sprintf("Reconciling catalog source for %s offering deployment", r.managedFusionOffering.Spec.Kind))
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.catalogSource, func() error {
		if err := r.own(r.catalogSource); err != nil {
			return err
		}
		desired := templates.OCSCatalogSource.DeepCopy()
		desired.Spec.DisplayName = r.installConfig.displayName
		desired.Spec.Publisher = r.installConfig.publisher
		desired.Spec.Image = r.installConfig.indexImage
		r.catalogSource.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to create/update OLM catalog source: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileSubscription() error {
	r.Log.Info(fmt.Sprintf("Reconciling subscription for %s offering deployment", r.managedFusionOffering.Spec.Kind))
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.subscription, func() error {
		if err := r.own(r.subscription); err != nil {
			return err
		}
		desired := templates.OCSSubscription.DeepCopy()
		desired.Spec.CatalogSource = r.catalogSource.Name
		desired.Spec.CatalogSourceNamespace = r.catalogSource.Namespace
		desired.Spec.Channel = r.installConfig.channel
		desired.Spec.StartingCSV = r.installConfig.startingCSV
		desired.Spec.Package = r.installConfig.packageName
		r.subscription.Spec = desired.Spec
		return nil

	})
	if err != nil {
		return fmt.Errorf("Failed to create/update OLM subscription: %v", err)
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) reconcileInstallPlan() error {
	r.Log.Info(fmt.Sprintf("Reconciling DF Operator install plan for %s offering deployment", r.managedFusionOffering.Spec.Kind))
	operatorCSV := &opv1a1.ClusterServiceVersion{}
	operatorCSV.Name = r.subscription.Spec.StartingCSV
	operatorCSV.Namespace = r.namespace
	var requiredInstallPlan *opv1a1.InstallPlan
	if err := r.get(operatorCSV); err != nil {
		if errors.IsNotFound(err) {
			installPlans := &opv1a1.InstallPlanList{}
			if err := r.list(installPlans); err != nil {
				return err
			}
			for _, installPlan := range installPlans.Items {
				if utils.FindInSlice(installPlan.Spec.ClusterServiceVersionNames, operatorCSV.Name) {
					requiredInstallPlan = &installPlan
					break
				}
			}
		} else {
			return fmt.Errorf("failed to get CSV for %s offering deployment: %v", r.managedFusionOffering.Spec.Kind, err)
		}
	}
	if requiredInstallPlan == nil {
		r.Log.V(-1).Info(fmt.Sprintf("install plan not found for %s CSV", operatorCSV.Name))
		return nil
	}
	if requiredInstallPlan.Spec.Approval == opv1a1.ApprovalManual &&
		!requiredInstallPlan.Spec.Approved {
		requiredInstallPlan.Spec.Approved = true
		if err := r.update(requiredInstallPlan); err != nil {
			return err
		}
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

// This function is a placeholder for offering plugin integration
func pluginGetInstallalConfig(r *ManagedFusionOfferingReconciler) error {
	switch r.managedFusionOffering.Spec.Release {
	case "4.12":
		r.installConfig.displayName = "managed-fusion-ocs"
		r.installConfig.publisher = "IBM"
		r.installConfig.indexImage = "registry.redhat.io/redhat/redhat-operator-index:v4.12"
		r.installConfig.startingCSV = "ocs-operator.v4.12.0"
		r.installConfig.channel = "stable-4.12"
		r.installConfig.packageName = "ocs-operator"
		return nil
	default:
		return fmt.Errorf("Invalid release for %s offering", r.managedFusionOffering.Spec.Kind)
	}
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
