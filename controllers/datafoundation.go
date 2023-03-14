package controllers

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/datafoundation"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

const (
	defaultDeviceSetName = "default"
)

type dataFoundationSpec struct {
	usableCapacityInTiB     int
	onboardingValidationKey string
}

type dataFoundationReconciler struct {
	*ManagedFusionOfferingReconciler

	spec                           dataFoundationSpec
	onboardingValidationKeySecret  *corev1.Secret
	storageCluster                 *ocsv1.StorageCluster
	cephIngressNetworkPolicy       *netv1.NetworkPolicy
	providerAPIServerNetworkPolicy *netv1.NetworkPolicy
	rookConfigMap                  *corev1.ConfigMap
}

//+kubebuilder:rbac:groups=ocs.openshift.io,namespace=system,resources=storageclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.openshift.io,resources=ingressnetworkpolicies,verbs=create;get;list;watch;update

func dfSetupWatches(controllerBuilder *builder.Builder) {
	controllerBuilder.
		Owns(&corev1.Secret{}).
		Owns(&ocsv1.StorageCluster{}).
		Owns(&netv1.NetworkPolicy{}).
		Owns(&corev1.ConfigMap{})
}

func DFAddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(ocsv1.AddToScheme(scheme))
}

func dfReconcile(offeringReconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) error {
	r := dataFoundationReconciler{}
	r.initReconciler(offeringReconciler)

	if err := r.parseSpec(offering); err != nil {
		return err
	}
	if err := r.reconcileOnboardingValidationSecret(); err != nil {
		return err
	}
	if err := r.reconcileStorageCluster(); err != nil {
		return err
	}
	if err := r.reconcileCephIngressNetworkPolicy(); err != nil {
		return err
	}
	if err := r.reconcileProviderAPIServerNetworkPolicy(); err != nil {
		return err
	}
	if err := r.reconcileRookCephOperatorConfig(); err != nil {
		return err
	}

	return nil
}

func (r *dataFoundationReconciler) initReconciler(offeringReconciler *ManagedFusionOfferingReconciler) {
	r.ManagedFusionOfferingReconciler = offeringReconciler

	r.onboardingValidationKeySecret = &corev1.Secret{}
	r.onboardingValidationKeySecret.Name = "onboarding-ticket-key"
	r.onboardingValidationKeySecret.Namespace = r.Namespace

	r.storageCluster = &ocsv1.StorageCluster{}
	r.storageCluster.Name = "ocs-storagecluster"
	r.storageCluster.Namespace = r.Namespace

	r.cephIngressNetworkPolicy = &netv1.NetworkPolicy{}
	r.cephIngressNetworkPolicy.Name = "ceph-ingress-rule"
	r.cephIngressNetworkPolicy.Namespace = r.Namespace

	r.providerAPIServerNetworkPolicy = &netv1.NetworkPolicy{}
	r.providerAPIServerNetworkPolicy.Name = "provider-api-server-rule"
	r.providerAPIServerNetworkPolicy.Namespace = r.Namespace

	r.rookConfigMap = &corev1.ConfigMap{}
	r.rookConfigMap.Name = "rook-ceph-operator-config"
	r.rookConfigMap.Namespace = r.Namespace
}

func (r *dataFoundationReconciler) parseSpec(offering *v1alpha1.ManagedFusionOffering) error {
	r.Log.Info("Parsing ManagedFusionOffering Data Foundation spec")

	isValid := true
	var err error
	var usableCapacityInTiB int
	usableCapacityInTiBAsString, found := offering.Spec.Config["usableCapacityInTiB"]
	if !found {
		r.Log.Error(
			fmt.Errorf("missing field: usableCapacityInTiB"),
			"an error occurred while parsing ManagedFusionOffering Data Foundation spec",
		)
		isValid = false
	} else if usableCapacityInTiB, err = strconv.Atoi(usableCapacityInTiBAsString); err != nil {
		r.Log.Error(
			fmt.Errorf("error parsing usableCapacityInTib: %v", err),
			"an error occurred while parsing ManagedFusionOffering Data Foundation spec",
		)
		isValid = false
	}

	onboardingValidationKeyAsString, found := offering.Spec.Config["onboardingValidationKey"]
	if !found {
		r.Log.Error(
			fmt.Errorf("missing field: onboardingValidationKey"),
			"an error occurred while parsing ManagedFusionOffering Data Foundation spec",
		)
		isValid = false
	}

	if !isValid {
		r.Log.Info("parsing ManagedFusionOffering Data Foundation spec failed")
		return fmt.Errorf("invalid ManagedFusionOffering Data Foundation spec")
	}
	r.Log.Info("parsing ManagedFusionOffering Data Foundation spec completed successfuly")

	r.spec = dataFoundationSpec{
		usableCapacityInTiB:     usableCapacityInTiB,
		onboardingValidationKey: onboardingValidationKeyAsString,
	}
	return nil
}

func (r *dataFoundationReconciler) reconcileOnboardingValidationSecret() error {
	r.Log.Info("Reconciling onboardingValidationKey secret")

	_, err := ctrl.CreateOrUpdate(r.Ctx, r.Client, r.onboardingValidationKeySecret, func() error {
		if err := r.own(r.onboardingValidationKeySecret); err != nil {
			return err
		}
		onboardingValidationData := fmt.Sprintf(
			"-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----",
			strings.TrimSpace(r.spec.onboardingValidationKey),
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

	_, err := ctrl.CreateOrUpdate(r.Ctx, r.Client, r.storageCluster, func() error {
		if err := r.own(r.storageCluster); err != nil {
			return err
		}

		// Convert the desired size to the device set count based on the underlaying OSD size
		desiredDeviceSetCount := int(math.Ceil(float64(r.spec.usableCapacityInTiB) / datafoundation.OSDSizeInTiB))

		// Get the storage device set count of the current storage cluster
		currDeviceSetCount := 0
		if desiredStorageDeviceSet := findStorageDeviceSet(r.storageCluster.Spec.StorageDeviceSets, defaultDeviceSetName); desiredStorageDeviceSet != nil {
			currDeviceSetCount = desiredStorageDeviceSet.Count
		}

		// Get the desired storage device set from storage cluster template
		sc := datafoundation.StorageClusterTemplate.DeepCopy()
		var ds *ocsv1.StorageDeviceSet = nil
		if desiredStorageDeviceSet := findStorageDeviceSet(sc.Spec.StorageDeviceSets, defaultDeviceSetName); desiredStorageDeviceSet != nil {
			ds = desiredStorageDeviceSet
		}

		// Prevent downscaling by comparing count from secret and count from storage cluster
		r.setDeviceSetCount(ds, desiredDeviceSetCount, currDeviceSetCount)
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

func (r *dataFoundationReconciler) setDeviceSetCount(deviceSet *ocsv1.StorageDeviceSet, desiredDeviceSetCount int, currDeviceSetCount int) {
	r.Log.Info("Setting storage device set count", "Current", currDeviceSetCount, "New", desiredDeviceSetCount)
	if currDeviceSetCount <= desiredDeviceSetCount {
		deviceSet.Count = desiredDeviceSetCount
	} else {
		r.Log.V(-1).Info("Requested storage device set count will result in downscaling, which is not supported. Skipping")
		deviceSet.Count = currDeviceSetCount
	}
}

func (r *dataFoundationReconciler) reconcileCephIngressNetworkPolicy() error {
	_, err := ctrl.CreateOrUpdate(r.Ctx, r.Client, r.cephIngressNetworkPolicy, func() error {
		if err := r.own(r.cephIngressNetworkPolicy); err != nil {
			return err
		}
		desired := datafoundation.CephNetworkPolicyTemplate.DeepCopy()
		r.cephIngressNetworkPolicy.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to update ceph ingress NetworkPolicy: %v", err)
	}
	return nil
}

func (r *dataFoundationReconciler) reconcileProviderAPIServerNetworkPolicy() error {
	_, err := ctrl.CreateOrUpdate(r.Ctx, r.Client, r.providerAPIServerNetworkPolicy, func() error {
		if err := r.own(r.providerAPIServerNetworkPolicy); err != nil {
			return err
		}
		desired := datafoundation.ProviderApiServerNetworkPolicyTemplate.DeepCopy()
		r.providerAPIServerNetworkPolicy.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to update provider api server NetworkPolicy: %v", err)
	}

	return nil
}

func (r *dataFoundationReconciler) reconcileRookCephOperatorConfig() error {
	if err := r.get(r.rookConfigMap); err != nil {
		return fmt.Errorf("Failed to get Rook ConfigMap: %v", err)
	}

	cloneRookConfigMap := r.rookConfigMap.DeepCopy()
	if cloneRookConfigMap.Data == nil {
		cloneRookConfigMap.Data = map[string]string{}
	}

	cloneRookConfigMap.Data["ROOK_CSI_ENABLE_CEPHFS"] = "false"
	cloneRookConfigMap.Data["ROOK_CSI_ENABLE_RBD"] = "false"
	if !equality.Semantic.DeepEqual(r.rookConfigMap, cloneRookConfigMap) {
		if err := r.update(cloneRookConfigMap); err != nil {
			return fmt.Errorf("Failed to update Rook ConfigMap: %v", err)
		}
	}

	return nil
}
