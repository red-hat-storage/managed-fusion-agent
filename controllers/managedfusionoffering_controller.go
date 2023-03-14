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
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
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
	if err := r.reconcileRookCephOperatorConfig(); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// This function is a placeholder for offering plugin integration
func pluginSetupWatches(controllerBuilder *builder.Builder) {
	controllerBuilder.Owns(&corev1.ConfigMap{})
}

func (r *ManagedFusionOfferingReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *ManagedFusionOfferingReconciler) reconcileRookCephOperatorConfig() error {
	rookConfigMap := &corev1.ConfigMap{}
	rookConfigMap.Name = "rook-ceph-operator-config"
	rookConfigMap.Namespace = r.managedFusionOffering.Namespace

	if err := r.get(rookConfigMap); err != nil {
		// Because resource limits will not be set, failure to get the Rook ConfigMap results in failure to reconcile.
		return fmt.Errorf("Failed to get Rook ConfigMap: %v", err)
	}

	cloneRookConfigMap := rookConfigMap.DeepCopy()
	if cloneRookConfigMap.Data == nil {
		cloneRookConfigMap.Data = map[string]string{}
	}

	cloneRookConfigMap.Data["ROOK_CSI_ENABLE_CEPHFS"] = "false"
	cloneRookConfigMap.Data["ROOK_CSI_ENABLE_RBD"] = "false"
	if !equality.Semantic.DeepEqual(rookConfigMap, cloneRookConfigMap) {
		if err := r.update(cloneRookConfigMap); err != nil {
			return fmt.Errorf("Failed to update Rook ConfigMap: %v", err)
		}
	}

	return nil
}
