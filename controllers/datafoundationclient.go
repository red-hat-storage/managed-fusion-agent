package controllers

import (
	"fmt"
	"strings"

	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
	ocsclient "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type dataFoundationClientSpec struct {
	onboardingTicket string
	providerEndpoint string
}

type dataFoundationClientReconciler struct {
	*ManagedFusionOfferingReconciler

	offering                         *v1alpha1.ManagedFusionOffering
	dataFoundationClientSpec         dataFoundationClientSpec
	storageClient                    ocsclient.StorageClient
	defaultBlockStorageClassClaim    ocsclient.StorageClassClaim
	defaultFSStorageClassClaim       ocsclient.StorageClassClaim
	csiKMSConnectionDetailsConfigMap corev1.ConfigMap
}

const (
	ocsClientOperatorName                = "ocs-client-operator"
	csiKMSConnectionDetailsConfigMapName = "csi-kms-connection-details"
)

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclients,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch

func dfcSetupWatches(controllerBuilder *builder.Builder) {
	csvPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(res client.Object) bool {
				return strings.HasPrefix(res.GetName(), ocsClientOperatorName)
			},
		),
	)
	controllerBuilder.
		Owns(&opv1a1.ClusterServiceVersion{}, csvPredicates).
		Owns(&ocsclient.StorageClient{}).
		Owns(&ocsclient.StorageClassClaim{}).
		Owns(&corev1.ConfigMap{})
}

func DFCAddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(ocsclient.AddToScheme(scheme))
}

func dfcReconcile(offeringReconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) (ctrl.Result, error) {
	r := dataFoundationClientReconciler{}
	r.initReconciler(offeringReconciler, offering)

	if err := r.parseSpec(offering); err != nil {
		return ctrl.Result{}, err
	}
	return r.reconcilePhases()
}

func (r *dataFoundationClientReconciler) initReconciler(reconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) {
	r.ManagedFusionOfferingReconciler = reconciler
	r.offering = offering

	r.storageClient.Name = "ocs-storageclient"
	r.storageClient.Namespace = offering.Namespace

	r.defaultBlockStorageClassClaim.Name = "ocs-storagecluster-ceph-rbd"

	r.defaultFSStorageClassClaim.Name = "ocs-storagecluster-cephfs"

	r.csiKMSConnectionDetailsConfigMap.Name = csiKMSConnectionDetailsConfigMapName
	r.csiKMSConnectionDetailsConfigMap.Namespace = offering.Namespace
}

func (r *dataFoundationClientReconciler) parseSpec(offering *v1alpha1.ManagedFusionOffering) error {
	r.Log.Info("Parsing ManagedFusionOffering Data Foundation Client spec")

	isValid := true
	onboardingTicket, found := offering.Spec.Config["onboardingTicket"]
	if !found {
		r.Log.Error(
			fmt.Errorf("missing field: onboardingTicket"),
			"an error occurred while parsing ManagedFusionOffering Data Foundation Client spec",
		)
		isValid = false
	}

	providerEndpoint, found := offering.Spec.Config["dataFoundationProviderEndpoint"]
	if !found {
		r.Log.Error(
			fmt.Errorf("missing field: dataFoundationProviderEndpoint"),
			"an error occurred while parsing ManagedFusionOffering Data Foundation Client spec",
		)
		isValid = false
	}

	if !isValid {
		r.Log.Info("parsing ManagedFusionOffering Data Foundation Client spec failed")
		return fmt.Errorf("invalid ManagedFusionOffering Data Foundation Client spec")
	}
	r.Log.Info("parsing ManagedFusionOffering Data Foundation Client spec completed successfuly")

	r.dataFoundationClientSpec = dataFoundationClientSpec{
		onboardingTicket: onboardingTicket,
		providerEndpoint: providerEndpoint,
	}
	return nil
}

func (r *dataFoundationClientReconciler) reconcilePhases() (ctrl.Result, error) {
	if !r.offering.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	} else {
		if err := r.reconcileCSV(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileStorageClient(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileDefaultBlockStorageClassClaim(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileDefaultFSStorageClassClaim(); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileCSIKMSConnectionDetailsConfigMap(); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
}

func (r *dataFoundationClientReconciler) reconcileStorageClient() error {
	r.Log.Info("Reconciling StorageClient")

	if r.dataFoundationClientSpec.providerEndpoint == "" {
		return fmt.Errorf("invalid provider endpoint, empty string")
	}

	if r.dataFoundationClientSpec.onboardingTicket == "" {
		return fmt.Errorf("invalid onboarding ticket, empty string")

	}
	_, err := r.CreateOrUpdate(&r.storageClient, func() error {
		if err := r.own(&r.storageClient, true); err != nil {
			return err
		}
		r.storageClient.Spec.OnboardingTicket = r.dataFoundationClientSpec.onboardingTicket
		r.storageClient.Spec.StorageProviderEndpoint = r.dataFoundationClientSpec.providerEndpoint
		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to create/update StorageClient: %v", err)
	}
	return nil
}

func (r *dataFoundationClientReconciler) reconcileCSV() error {
	r.Log.Info("Reconciling OCS Client Operator CSV")

	csv, err := r.getCSVByPrefix(ocsClientOperatorName)
	if err != nil {
		return fmt.Errorf("unable to get OCS client CSV: %v", err)
	}
	if err := r.own(csv, false); err != nil {
		return fmt.Errorf("unable to set owner reference on OCS client CSV : %v", err)
	}
	if err := r.update(csv); err != nil {
		return fmt.Errorf("failed to update OCS client CSV: %v", err)
	}
	return nil
}

func (r *dataFoundationClientReconciler) reconcileDefaultBlockStorageClassClaim() error {
	r.Log.Info("Reconciling Default BlockPool StorageClassClaim")

	_, err := r.CreateOrUpdate(&r.defaultBlockStorageClassClaim, func() error {
		r.defaultBlockStorageClassClaim.Spec.Type = "blockpool"
		r.defaultBlockStorageClassClaim.Spec.StorageClient = &ocsclient.StorageClientNamespacedName{
			Name:      r.storageClient.Name,
			Namespace: r.storageClient.Namespace,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to create/update Default blockpool StorageClassClaim: %v", err)
	}
	return nil
}

func (r *dataFoundationClientReconciler) reconcileDefaultFSStorageClassClaim() error {
	r.Log.Info("Reconciling Default Filesystem StorageClassClaim")

	_, err := r.CreateOrUpdate(&r.defaultFSStorageClassClaim, func() error {
		r.defaultFSStorageClassClaim.Spec.Type = "sharedfilesystem"
		r.defaultFSStorageClassClaim.Spec.StorageClient = &ocsclient.StorageClientNamespacedName{
			Name:      r.storageClient.Name,
			Namespace: r.storageClient.Namespace,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to create/update Default FileSystem StorageClassClaim: %v", err)
	}

	return nil
}

func (r *dataFoundationClientReconciler) reconcileCSIKMSConnectionDetailsConfigMap() error {
	r.Log.Info(fmt.Sprintf("Reconciling %s configMap", csiKMSConnectionDetailsConfigMapName))

	_, err := r.CreateOrUpdate(&r.csiKMSConnectionDetailsConfigMap, func() error {
		if err := r.own(&r.csiKMSConnectionDetailsConfigMap, true); err != nil {
			return err
		}

		awsStsMetadataTestAsString := string(
			utils.ToJsonOrDie(
				map[string]string{
					"encryptionKMSType": "aws-sts-metadata",
					"secretName":        "ceph-csi-aws-credentials",
				},
			),
		)
		r.csiKMSConnectionDetailsConfigMap.Data = map[string]string{
			"aws-sts-metadata-test": awsStsMetadataTestAsString,
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("Failed to create or update %s config map: %v", csiKMSConnectionDetailsConfigMapName, err)
	}

	return nil
}
