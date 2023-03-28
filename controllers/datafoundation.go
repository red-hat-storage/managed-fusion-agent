package controllers

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/datafoundation"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	defaultDeviceSetName = "default"
	ocsOperatorName      = "ocs-operator"
)

type dataFoundationSpec struct {
	usableCapacityInTiB     int
	onboardingValidationKey string
}

type dataFoundationReconciler struct {
	*ManagedFusionOfferingReconciler

	dfName                         string
	dfNamespace                    string
	spec                           dataFoundationSpec
	onboardingValidationKeySecret  *corev1.Secret
	storageCluster                 *ocsv1.StorageCluster
	cephIngressNetworkPolicy       *netv1.NetworkPolicy
	providerAPIServerNetworkPolicy *netv1.NetworkPolicy
	rookConfigMap                  *corev1.ConfigMap
	ocsInitialization              *ocsv1.OCSInitialization
}

//+kubebuilder:rbac:groups=ocs.openshift.io,namespace=system,resources=storageclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.openshift.io,resources=ingressnetworkpolicies,verbs=create;get;list;watch;update
//+kubebuilder:rbac:groups=operators.coreos.com,namespace=system,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch
//+kubebuilder:rbac:groups=ocs.openshift.io,namespace=system,resources=ocsinitializations,verbs=get;list;watch;update;patch;delete

func dfSetupWatches(controllerBuilder *builder.Builder) {
	csvPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(res client.Object) bool {
				return strings.HasPrefix(res.GetName(), ocsOperatorName)
			},
		),
	)
	controllerBuilder.
		Owns(&corev1.Secret{}).
		Owns(&ocsv1.StorageCluster{}).
		Owns(&netv1.NetworkPolicy{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&opv1a1.ClusterServiceVersion{}, csvPredicates).
		Owns(&ocsv1.OCSInitialization{})
}

func DFAddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(ocsv1.AddToScheme(scheme))
}

func dfIsReadyToBeRemoved(r *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) (bool, error) {
	return true, nil
}

func dfReconcile(offeringReconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) error {
	r := dataFoundationReconciler{}
	r.initReconciler(offeringReconciler, offering)

	if err := r.parseSpec(offering); err != nil {
		return err
	}
	if err := r.reconcileOnboardingValidationSecret(); err != nil {
		return err
	}
	if err := r.reconcileStorageCluster(); err != nil {
		return err
	}
	if err := r.reconcileCSV(); err != nil {
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
	if err := r.reconcileOCSInitialization(); err != nil {
		return err
	}

	return nil
}

func (r *dataFoundationReconciler) initReconciler(reconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) {
	r.ManagedFusionOfferingReconciler = reconciler

	r.dfName = offering.Name
	r.dfNamespace = offering.Namespace

	r.onboardingValidationKeySecret = &corev1.Secret{}
	r.onboardingValidationKeySecret.Name = "onboarding-ticket-key"
	r.onboardingValidationKeySecret.Namespace = offering.Namespace

	r.storageCluster = &ocsv1.StorageCluster{}
	r.storageCluster.Name = "ocs-storagecluster"
	r.storageCluster.Namespace = offering.Namespace

	r.cephIngressNetworkPolicy = &netv1.NetworkPolicy{}
	r.cephIngressNetworkPolicy.Name = "ceph-ingress-rule"
	r.cephIngressNetworkPolicy.Namespace = offering.Namespace

	r.providerAPIServerNetworkPolicy = &netv1.NetworkPolicy{}
	r.providerAPIServerNetworkPolicy.Name = "provider-api-server-rule"
	r.providerAPIServerNetworkPolicy.Namespace = offering.Namespace

	r.rookConfigMap = &corev1.ConfigMap{}
	r.rookConfigMap.Name = "rook-ceph-operator-config"
	r.rookConfigMap.Namespace = offering.Namespace

	r.providerAPIServerNetworkPolicy = &netv1.NetworkPolicy{}
	r.providerAPIServerNetworkPolicy.Name = "provider-api-server-rule"
	r.providerAPIServerNetworkPolicy.Namespace = offering.Namespace

	r.ocsInitialization = &ocsv1.OCSInitialization{}
	r.ocsInitialization.Name = "ocsinit"
	r.ocsInitialization.Namespace = offering.Namespace
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

	_, err := r.CreateOrUpdate(r.onboardingValidationKeySecret, func() error {
		if err := r.own(r.onboardingValidationKeySecret, true); err != nil {
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

	_, err := r.CreateOrUpdate(r.storageCluster, func() error {
		if err := r.own(r.storageCluster, true); err != nil {
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
	_, err := r.CreateOrUpdate(r.cephIngressNetworkPolicy, func() error {
		if err := r.own(r.cephIngressNetworkPolicy, true); err != nil {
			return err
		}
		desired := datafoundation.CephNetworkPolicyTemplate.DeepCopy()
		r.cephIngressNetworkPolicy.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update ceph ingress NetworkPolicy: %v", err)
	}
	return nil
}

func (r *dataFoundationReconciler) reconcileProviderAPIServerNetworkPolicy() error {
	_, err := r.CreateOrUpdate(r.providerAPIServerNetworkPolicy, func() error {
		if err := r.own(r.providerAPIServerNetworkPolicy, true); err != nil {
			return err
		}
		desired := datafoundation.ProviderApiServerNetworkPolicyTemplate.DeepCopy()
		r.providerAPIServerNetworkPolicy.Spec = desired.Spec
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update provider api server NetworkPolicy: %v", err)
	}

	return nil
}

func (r *dataFoundationReconciler) reconcileRookCephOperatorConfig() error {
	if err := r.get(r.rookConfigMap); err != nil {
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

	err := r.GetAndUpdate(r.ocsInitialization, func() error {
		if err := r.own(r.ocsInitialization, false); err != nil {
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
