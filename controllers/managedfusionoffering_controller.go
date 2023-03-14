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
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	if _, found := r.managedFusionOffering.Spec.Config["usableCapacityInTiB"]; !found {
		return ctrl.Result{}, fmt.Errorf("%s CR does not contain usableCapacityInTiB entry", r.managedFusionOffering.Name)
	}
	if _, found := r.managedFusionOffering.Spec.Config["onboardingValidationKey"]; !found {
		return ctrl.Result{}, fmt.Errorf("%s CR does not contain onboardingValidationKey entry", r.managedFusionOffering.Name)
	}
	if err := reconcileOnboardingValidationSecret(r); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func reconcileOnboardingValidationSecret(r *ManagedFusionOfferingReconciler) error {
	r.Log.Info("Reconciling onboardingValidationKeySecret")
	onboardingValidationKeySecret := &corev1.Secret{}
	onboardingValidationKeySecret.Name = "onboarding-ticket-key"
	onboardingValidationKeySecret.Namespace = r.managedFusionOffering.Namespace
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, onboardingValidationKeySecret, func() error {
		if err := r.own(onboardingValidationKeySecret); err != nil {
			return err
		}
		onboardingValidationData := fmt.Sprintf(
			"-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----",
			strings.TrimSpace(r.managedFusionOffering.Spec.Config["onboardingValidationKey"]),
		)
		onboardingValidationKeySecret.Data = map[string][]byte{
			"key": []byte(onboardingValidationData),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to update onboardingValidationKeySecret: %v", err)
	}
	return nil
}

// This function is a placeholder for offering plugin integration
func pluginSetupWatches(controllerBuilder *builder.Builder) {
	controllerBuilder.
		Owns(&corev1.Secret{})
}

func (r *ManagedFusionOfferingReconciler) own(resource metav1.Object) error {
	// Ensure managedFusionOffering ownership on a resource
	if err := ctrl.SetControllerReference(r.managedFusionOffering, resource, r.Scheme); err != nil {
		return err
	}
	return nil
}

func (r *ManagedFusionOfferingReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}
