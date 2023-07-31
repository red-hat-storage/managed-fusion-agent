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
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"strings"
	"time"

	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/datafoundation"
	"github.com/red-hat-storage/managed-fusion-agent/datafoundation/templates"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	defaultDeviceSetName     = "default"
	ocsOperatorName          = "ocs-operator"
	storageClusterName       = "ocs-storagecluster"
	storageClusterCRDName    = "storageclusters.ocs.openshift.io"
	ocsInitializationCRDName = "ocsinitializations.ocs.openshift.io"
)

type dataFoundationSpec struct {
	UsableCapacityInTiB     int
	OnboardingValidationKey string
}

type dataFoundationReconciler struct {
	*ManagedFusionOfferingReconciler

	offering                       *v1alpha1.ManagedFusionOffering
	spec                           *dataFoundationSpec
	onboardingValidationKeySecret  corev1.Secret
	storageCluster                 ocsv1.StorageCluster
	cephIngressNetworkPolicy       netv1.NetworkPolicy
	providerAPIServerNetworkPolicy netv1.NetworkPolicy
	rookConfigMap                  corev1.ConfigMap
	ocsInitialization              ocsv1.OCSInitialization
	availableCRDs                  map[string]bool
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=ocsinitializations,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;list;watch
//+kubebuilder:rbac:groups=network.openshift.io,resources=ingressnetworkpolicies,verbs=create;get;list;watch;update
//+kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch

func dfSetupWatches(offeringReconciler *ManagedFusionOfferingReconciler, controllerBuilder *builder.Builder) {
	csvPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(res client.Object) bool {
				return strings.HasPrefix(res.GetName(), ocsOperatorName)
			},
		),
	)

	controllerBuilder.
		Owns(&corev1.Secret{}).
		Owns(&netv1.NetworkPolicy{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&opv1a1.ClusterServiceVersion{}, csvPredicates)

	if offeringReconciler.AvailableCRDs[storageClusterCRDName] {
		controllerBuilder.
			Owns(&ocsv1.StorageCluster{})
	}
	if offeringReconciler.AvailableCRDs[ocsInitializationCRDName] {
		controllerBuilder.
			Owns(&ocsv1.OCSInitialization{})
	}
}

func DFAddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(ocsv1.AddToScheme(scheme))
	utilruntime.Must(ocsv1alpha1.AddToScheme(scheme))
}

func dfGetOfferingSpecInstance() *dataFoundationSpec {
	return &dataFoundationSpec{}
}

func dfIsReadyToBeRemoved(r *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) (bool, error) {
	sc := &ocsv1.StorageCluster{}
	sc.Name = storageClusterName
	sc.Namespace = offering.Namespace

	if err := r.get(sc); err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		} else {
			return false, err
		}
	} else {
		return false, nil
	}
}

func dfReconcile(offeringReconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering, offeringSpec interface{}) (ctrl.Result, error) {
	// Set up the datafoundation sub-reconciler
	r := dataFoundationReconciler{}
	r.initReconciler(offeringReconciler, offering)
	r.spec = offeringSpec.(*dataFoundationSpec)

	// Reconcile based on desired state
	return r.reconcilePhases()
}

func (r *dataFoundationReconciler) initReconciler(reconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) {
	r.ManagedFusionOfferingReconciler = reconciler
	r.offering = offering

	r.onboardingValidationKeySecret.Name = "onboarding-ticket-key"
	r.onboardingValidationKeySecret.Namespace = offering.Namespace

	r.storageCluster.Name = storageClusterName
	r.storageCluster.Namespace = offering.Namespace

	r.cephIngressNetworkPolicy.Name = "ceph-ingress-rule"
	r.cephIngressNetworkPolicy.Namespace = offering.Namespace

	r.providerAPIServerNetworkPolicy.Name = "provider-api-server-rule"
	r.providerAPIServerNetworkPolicy.Namespace = offering.Namespace

	r.rookConfigMap.Name = "rook-ceph-operator-config"
	r.rookConfigMap.Namespace = offering.Namespace

	r.ocsInitialization.Name = "ocsinit"
	r.ocsInitialization.Namespace = offering.Namespace

	r.availableCRDs = reconciler.AvailableCRDs
}

func (r *dataFoundationReconciler) reconcilePhases() (ctrl.Result, error) {
	// Checking for CRDs that were installed after the agent was started
	if !r.availableCRDs[storageClusterCRDName] {
		crd := apiextensionsv1.CustomResourceDefinition{}
		crd.Name = storageClusterCRDName
		if err := r.get(&crd); err != nil {
			return ctrl.Result{}, err
		} else {
			utils.HandleMissingWatch(storageClusterCRDName)
		}
	}
	if !r.availableCRDs[ocsInitializationCRDName] {
		crd := apiextensionsv1.CustomResourceDefinition{}
		crd.Name = ocsInitializationCRDName
		if err := r.get(&crd); err != nil {
			return ctrl.Result{}, err
		} else {
			utils.HandleMissingWatch(ocsInitializationCRDName)
		}
	}

	if !r.offering.DeletionTimestamp.IsZero() {
		if err := r.get(&r.storageCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to get storagecluster: %v", err)
		}
		if r.storageCluster.Status.Phase != "Ready" {
			r.Log.Info("storagecluster is not in ready state, cannot proceed with uninstallation")
			return ctrl.Result{}, nil
		}

		storageConsumerList := ocsv1alpha1.StorageConsumerList{}
		if err := r.list(&storageConsumerList, client.Limit(1)); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to list OCS StorageConsumers CRs, %v", err)
		}
		if len(storageConsumerList.Items) > 0 {
			r.Log.Info("Found OCS storage consumers, cannot proceed with uninstallation")
			return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
		}

		r.Log.Info("issuing a delete for storagecluster")
		if err := r.delete(&r.storageCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to issue a delete for storagecluster: %v", err)
		}

	} else {
		if err := r.reconcileOnboardingValidationSecret(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileStorageCluster(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileMonitoringResources(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileCSV(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileCephIngressNetworkPolicy(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileProviderAPIServerNetworkPolicy(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileRookCephOperatorConfig(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileOCSInitialization(); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *dataFoundationReconciler) reconcileOnboardingValidationSecret() error {
	r.Log.Info("Reconciling onboardingValidationKey secret")

	if r.spec.OnboardingValidationKey == "" {
		return fmt.Errorf("invalid onboardingValidationKey ticket, empty string")
	}

	_, err := r.CreateOrUpdate(&r.onboardingValidationKeySecret, func() error {
		if err := r.own(&r.onboardingValidationKeySecret, true); err != nil {
			return err
		}
		onboardingValidationData := fmt.Sprintf(
			"-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----",
			strings.TrimSpace(r.spec.OnboardingValidationKey),
		)
		r.onboardingValidationKeySecret.Data = map[string][]byte{
			"key": []byte(onboardingValidationData),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update onboardingValidationKeySecret: %v", err)
	}
	return nil
}

func (r *dataFoundationReconciler) reconcileStorageCluster() error {
	r.Log.Info("Reconciling StorageCluster")

	if r.spec.UsableCapacityInTiB <= 0 {
		return fmt.Errorf("invalid or missing field: UsableCapacityInTiB, provided value is %v", r.spec.UsableCapacityInTiB)
	}

	_, err := r.CreateOrUpdate(&r.storageCluster, func() error {
		if err := r.own(&r.storageCluster, true); err != nil {
			return err
		}

		UsableCapacityInTiB := resource.MustParse(fmt.Sprintf("%dTi", r.spec.UsableCapacityInTiB))
		OSDSizeUpperBoundInTiB := resource.MustParse(fmt.Sprintf("%dTi", templates.OSDSizeUpperBoundInTiB))

		// Get the desired capacity in TiB planned to be consumed by user in range 1-4TiB
		// Set desired to be the minimum
		desiredOSDSizeInTiB := UsableCapacityInTiB
		if OSDSizeUpperBoundInTiB.Cmp(UsableCapacityInTiB) < 0 {
			desiredOSDSizeInTiB = OSDSizeUpperBoundInTiB
		}

		// Convert the desired size to the device set count based on the underlying OSD size
		desiredDeviceSetCount := int(math.Ceil(float64(UsableCapacityInTiB.ScaledValue(resource.Tera)) / float64(desiredOSDSizeInTiB.ScaledValue(resource.Tera))))

		// Get the desired storage device set from storage cluster template
		sc := templates.StorageClusterTemplate.DeepCopy()
		var ds *ocsv1.StorageDeviceSet = nil
		if desiredStorageDeviceSet := findStorageDeviceSet(sc.Spec.StorageDeviceSets, defaultDeviceSetName); desiredStorageDeviceSet != nil {
			ds = desiredStorageDeviceSet
		}

		// Get the storage device set count and OSD size of the current storage cluster
		if desiredStorageDeviceSet := findStorageDeviceSet(r.storageCluster.Spec.StorageDeviceSets, defaultDeviceSetName); desiredStorageDeviceSet != nil {
			currOSDSizeInTiB := desiredStorageDeviceSet.DataPVCTemplate.Spec.Resources.Requests.Storage()
			currDeviceSetCount := desiredStorageDeviceSet.Count
			// Prevent downscaling by comparing OSD size and count from secret and from storage cluster
			r.setDeviceSetProperties(ds, &desiredOSDSizeInTiB, currOSDSizeInTiB, desiredDeviceSetCount, currDeviceSetCount)
		}

		r.storageCluster.Spec = sc.Spec

		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to create/update StorageCluster: %v", err)

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

// setDeviceSetProperties sets the storage device set's properties OSD size in TiB and count
func (r *dataFoundationReconciler) setDeviceSetProperties(deviceSet *ocsv1.StorageDeviceSet, desiredOSDSize *resource.Quantity, currOSDSize *resource.Quantity, desiredDeviceSetCount int, currDeviceSetCount int) {
	// Downscaling is skipped
	if desiredDeviceSetCount < currDeviceSetCount || desiredOSDSize.Cmp(*currOSDSize) == -1 {
		r.Log.V(-1).Info("Requested storage device set properties will result in downscaling, which is not supported. Skipping")
		deviceSet.Count = currDeviceSetCount
		deviceSet.DataPVCTemplate.Spec.Resources.Requests = corev1.ResourceList{
			"storage": *currOSDSize,
		}
	} else {
		r.Log.Info("Setting storage device set count", "Current", currDeviceSetCount, "New", desiredDeviceSetCount)
		deviceSet.Count = desiredDeviceSetCount
		r.Log.Info("Setting storage device set OSD size", "Current", currOSDSize.String(), "New", desiredOSDSize.String())
		deviceSet.DataPVCTemplate.Spec.Resources.Requests = corev1.ResourceList{
			"storage": *desiredOSDSize,
		}
	}

}

// reconcileMonitoringResources labels all monitoring resources (ServiceMonitors, PodMonitors, and PrometheusRules)
// found in the target namespace with a label that matches the label selector the defined on the Prometheus resource
// we are reconciling in reconcilePrometheus. Doing so instructs the Prometheus instance to notice and react to these labeled
// monitoring resources
func (r *dataFoundationReconciler) reconcileMonitoringResources() error {
	r.Log.Info("reconciling monitoring resources")
	inNamespace := client.InNamespace(r.offering.Namespace)

	podMonitorList := promv1.PodMonitorList{}
	if err := r.list(&podMonitorList, inNamespace); err != nil {
		return fmt.Errorf("could not list pod monitors: %v", err)
	}
	for i := range podMonitorList.Items {
		obj := podMonitorList.Items[i]
		utils.AddLabel(obj, monLabelKey, monLabelValue)
		if err := r.update(obj); err != nil {
			return err
		}
	}

	serviceMonitorList := promv1.ServiceMonitorList{}
	if err := r.list(&serviceMonitorList, inNamespace); err != nil {
		return fmt.Errorf("could not list service monitors: %v", err)
	}
	for i := range serviceMonitorList.Items {
		obj := serviceMonitorList.Items[i]
		utils.AddLabel(obj, monLabelKey, monLabelValue)
		if err := r.update(obj); err != nil {
			return err
		}
	}

	promRuleList := promv1.PrometheusRuleList{}
	if err := r.list(&promRuleList, inNamespace); err != nil {
		return fmt.Errorf("could not list prometheus rules: %v", err)
	}
	for i := range promRuleList.Items {
		obj := promRuleList.Items[i]
		utils.AddLabel(obj, monLabelKey, monLabelValue)
		if err := r.update(obj); err != nil {
			return err
		}
	}

	return nil
}

func (r *dataFoundationReconciler) reconcileCSV() error {
	r.Log.Info("Reconciling OCS Operator CSV")

	csv, err := r.getCSVByPrefix(ocsOperatorName)
	if err != nil {
		return fmt.Errorf("unable to find OCS CSV: %v", err)
	}
	err = r.GetAndUpdate(csv, func() error {
		if err := r.own(csv, false); err != nil {
			return fmt.Errorf("unable to set ownerRef on ocs csv: %v", err)
		}
		deployments := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs
		for i := range deployments {
			containers := deployments[i].Spec.Template.Spec.Containers
			for j := range containers {
				container := &containers[j]
				resources := datafoundation.GetResourceRequirements(container.Name)
				container.Resources = resources
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update OCS CSV: %v", err)
	}
	return nil
}

func (r *dataFoundationReconciler) reconcileCephIngressNetworkPolicy() error {
	_, err := r.CreateOrUpdate(&r.cephIngressNetworkPolicy, func() error {
		if err := r.own(&r.cephIngressNetworkPolicy, true); err != nil {
			return err
		}
		desired := templates.CephNetworkPolicyTemplate.DeepCopy()
		r.cephIngressNetworkPolicy.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update ceph ingress NetworkPolicy: %v", err)
	}
	return nil
}

func (r *dataFoundationReconciler) reconcileProviderAPIServerNetworkPolicy() error {
	_, err := r.CreateOrUpdate(&r.providerAPIServerNetworkPolicy, func() error {
		if err := r.own(&r.providerAPIServerNetworkPolicy, true); err != nil {
			return err
		}
		desired := templates.ProviderApiServerNetworkPolicyTemplate.DeepCopy()
		r.providerAPIServerNetworkPolicy.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update provider api server NetworkPolicy: %v", err)
	}

	return nil
}

func (r *dataFoundationReconciler) reconcileRookCephOperatorConfig() error {
	if err := r.get(&r.rookConfigMap); err != nil {
		return fmt.Errorf("failed to get Rook ConfigMap: %v", err)
	}

	cloneRookConfigMap := r.rookConfigMap.DeepCopy()
	if cloneRookConfigMap.Data == nil {
		cloneRookConfigMap.Data = map[string]string{}
	}

	cloneRookConfigMap.Data["ROOK_CSI_ENABLE_CEPHFS"] = "false"
	cloneRookConfigMap.Data["ROOK_CSI_ENABLE_RBD"] = "false"
	if !equality.Semantic.DeepEqual(r.rookConfigMap, cloneRookConfigMap) {
		if err := r.update(cloneRookConfigMap); err != nil {
			return fmt.Errorf("failed to update Rook ConfigMap: %v", err)
		}
	}

	return nil
}

func (r *dataFoundationReconciler) reconcileOCSInitialization() error {
	r.Log.Info("Reconciling OCSInitialization")

	err := r.GetAndUpdate(&r.ocsInitialization, func() error {
		if err := r.own(&r.ocsInitialization, false); err != nil {
			return fmt.Errorf("unable to set ownerRef on ocs initialization CR: %v", err)
		}
		r.ocsInitialization.Spec.EnableCephTools = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update OCS initialization CR: %v", err)
	}

	return nil
}
