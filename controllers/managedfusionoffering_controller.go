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
	"math"
	"strconv"

	"github.com/go-logr/logr"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/templates"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
)

const (
	enableMCGKey           = "enableMCG"
	usableCapacityInTiBKey = "usableCapacityInTiB"
	deviceSetName          = "default"
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
//+kubebuilder:rbac:groups=ocs.openshift.io,namespace=system,resources=storageclusters,verbs=get;list;watch;create;update;patch;delete

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
	if err := reconcileStorageCluster(r); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func reconcileStorageCluster(r *ManagedFusionOfferingReconciler) error {
	r.Log.Info("Reconciling StorageCluster")
	storageCluster := &ocsv1.StorageCluster{}
	storageCluster.Name = "ocs-storagecluster"
	storageCluster.Namespace = r.managedFusionOffering.Namespace
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, storageCluster, func() error {
		if err := r.own(storageCluster); err != nil {
			return err
		}
		sizeAsString := r.managedFusionOffering.Spec.Config["usableCapacityInTiB"]

		// Setting hardcoded value here to force no MCG deployment
		enableMCGAsString := "false"
		if enableMCGRaw, exists := r.managedFusionOffering.Spec.Config[enableMCGKey]; exists {
			enableMCGAsString = enableMCGRaw
		}
		r.Log.Info("Requested add-on settings", usableCapacityInTiBKey, sizeAsString, enableMCGKey, enableMCGAsString)
		desiredSize, err := strconv.Atoi(sizeAsString)
		if err != nil {
			return fmt.Errorf("Invalid storage cluster size value: %v", sizeAsString)
		}

		// Convert the desired size to the device set count based on the underlaying OSD size
		desiredDeviceSetCount := int(math.Ceil(float64(desiredSize) / templates.ProviderOSDSizeInTiB))

		// Get the storage device set count of the current storage cluster
		currDeviceSetCount := 0
		if desiredStorageDeviceSet := findStorageDeviceSet(storageCluster.Spec.StorageDeviceSets, deviceSetName); desiredStorageDeviceSet != nil {
			currDeviceSetCount = desiredStorageDeviceSet.Count
		}

		// Get the desired storage device set from storage cluster template
		sc := templates.ProviderStorageClusterTemplate.DeepCopy()
		var ds *ocsv1.StorageDeviceSet = nil
		if desiredStorageDeviceSet := findStorageDeviceSet(sc.Spec.StorageDeviceSets, deviceSetName); desiredStorageDeviceSet != nil {
			ds = desiredStorageDeviceSet
		}

		// Prevent downscaling by comparing count from secret and count from storage cluster
		setDeviceSetCount(r, ds, desiredDeviceSetCount, currDeviceSetCount)

		// Check and enable MCG in Storage Cluster spec
		mcgEnable, err := strconv.ParseBool(enableMCGAsString)
		if err != nil {
			return fmt.Errorf("Invalid Enable MCG value: %v", enableMCGAsString)
		}
		if err := ensureMCGDeployment(r, sc, mcgEnable); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("Unable to create/update StorageCluster: %v", err)

	}

	return nil
}

func findStorageDeviceSet(storageDeviceSets []ocsv1.StorageDeviceSet, deviceSetName string) *ocsv1.StorageDeviceSet {
	for index := range storageDeviceSets {
		item := &storageDeviceSets[index]
		if item.Name == deviceSetName {
			return item
		}
	}
	return nil
}

func setDeviceSetCount(r *ManagedFusionOfferingReconciler, deviceSet *ocsv1.StorageDeviceSet, desiredDeviceSetCount int, currDeviceSetCount int) {
	r.Log.Info("Setting storage device set count", "Current", currDeviceSetCount, "New", desiredDeviceSetCount)
	if currDeviceSetCount <= desiredDeviceSetCount {
		deviceSet.Count = desiredDeviceSetCount
	} else {
		r.Log.V(-1).Info("Requested storage device set count will result in downscaling, which is not supported. Skipping")
		deviceSet.Count = currDeviceSetCount
	}
}

func ensureMCGDeployment(r *ManagedFusionOfferingReconciler, storageCluster *ocsv1.StorageCluster, mcgEnable bool) error {
	// Check and enable MCG in Storage Cluster spec
	if mcgEnable {
		r.Log.Info("Enabling Multi Cloud Gateway")
		storageCluster.Spec.MultiCloudGateway.ReconcileStrategy = "manage"
	} else if storageCluster.Spec.MultiCloudGateway.ReconcileStrategy == "manage" {
		r.Log.V(-1).Info("Trying to disable Multi Cloud Gateway, Invalid operation")
	}
	return nil
}

// This function is a placeholder for offering plugin integration
func pluginSetupWatches(controllerBuilder *builder.Builder) {
	controllerBuilder.
		Owns(&ocsv1.StorageCluster{})
}

func (r *ManagedFusionOfferingReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *ManagedFusionOfferingReconciler) own(resource metav1.Object) error {
	// Ensure managedFusionOffering ownership on a resource
	if err := ctrl.SetControllerReference(r.managedFusionOffering, resource, r.Scheme); err != nil {
		return err
	}
	return nil
}
