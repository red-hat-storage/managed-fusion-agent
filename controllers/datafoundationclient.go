package controllers

import (
	"fmt"
	"strings"

	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
	ocsclient "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
	spec                             dataFoundationClientSpec
	storageClient                    ocsclient.StorageClient
	defaultBlockStorageClassClaim    ocsclient.StorageClassClaim
	defaultFSStorageClassClaim       ocsclient.StorageClassClaim
	csiKMSConnectionDetailsConfigMap corev1.ConfigMap
	availableCRDs                    map[string]bool
}

const (
	ocsClientOperatorName                = "ocs-client-operator"
	csiKMSConnectionDetailsConfigMapName = "csi-kms-connection-details"
	storageClientCRDName                 = "storageclients.ocs.openshift.io"
	storageClassClaimCRDName             = "storageclassclaims.ocs.openshift.io"
)

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclients,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch

func dfcSetupWatches(offeringReconciler *ManagedFusionOfferingReconciler, controllerBuilder *builder.Builder) {
	csvPredicates := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(res client.Object) bool {
				return strings.HasPrefix(res.GetName(), ocsClientOperatorName)
			},
		),
	)
	controllerBuilder.
		Owns(&opv1a1.ClusterServiceVersion{}, csvPredicates).
		Owns(&corev1.ConfigMap{}).
		Owns(&opv1a1.ClusterServiceVersion{}, csvPredicates)

	if offeringReconciler.AvailableCRDs[storageClientCRDName] {
		controllerBuilder.
			Owns(&ocsclient.StorageClient{})
	}
	if offeringReconciler.AvailableCRDs[storageClassClaimCRDName] {
		controllerBuilder.
			Owns(&ocsclient.StorageClassClaim{})
	}
}

func DFCAddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(ocsclient.AddToScheme(scheme))
}

func dfcGetOfferingSpecInstance() *dataFoundationClientSpec {
	return &dataFoundationClientSpec{}
}

func dfcReconcile(offeringReconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering, offeringSpec interface{}) (ctrl.Result, error) {
	r := dataFoundationClientReconciler{}
	r.initReconciler(offeringReconciler, offering)
	r.spec = offeringSpec.(dataFoundationClientSpec)

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

	r.availableCRDs = reconciler.AvailableCRDs
}

func (r *dataFoundationClientReconciler) reconcilePhases() (ctrl.Result, error) {
	// Checking for CRDs that were installed after the agent was started
	if !r.availableCRDs[storageClientCRDName] {
		crd := apiextensionsv1.CustomResourceDefinition{}
		crd.Name = storageClientCRDName
		if err := r.get(&crd); err != nil {
			return ctrl.Result{}, err
		} else {
			utils.HandleMissingWatch(storageClientCRDName)
		}
	}
	if !r.availableCRDs[storageClassClaimCRDName] {
		crd := apiextensionsv1.CustomResourceDefinition{}
		crd.Name = storageClassClaimCRDName
		if err := r.get(&crd); err != nil {
			return ctrl.Result{}, err
		} else {
			utils.HandleMissingWatch(storageClassClaimCRDName)
		}
	}

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

	if r.spec.providerEndpoint == "" {
		return fmt.Errorf("invalid provider endpoint, empty string")
	}

	if r.spec.onboardingTicket == "" {
		return fmt.Errorf("invalid onboarding ticket, empty string")

	}
	_, err := r.CreateOrUpdate(&r.storageClient, func() error {
		if err := r.own(&r.storageClient, true); err != nil {
			return err
		}
		r.storageClient.Spec.OnboardingTicket = r.spec.onboardingTicket
		r.storageClient.Spec.StorageProviderEndpoint = r.spec.providerEndpoint
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
