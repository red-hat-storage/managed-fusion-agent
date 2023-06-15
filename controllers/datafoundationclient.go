package controllers

import (
	"fmt"
	"strings"
	"time"

	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"github.com/red-hat-storage/managed-fusion-agent/utils"
	ocsclient "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type dataFoundationClientSpec struct {
	OnboardingTicket string
	ProviderEndpoint string
}

type dataFoundationClientReconciler struct {
	*ManagedFusionOfferingReconciler

	offering                         *v1alpha1.ManagedFusionOffering
	spec                             *dataFoundationClientSpec
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
	storageClientName                    = "ocs-storageclient"
	ocsCephFsProvisionerSuffix           = ".cephfs.csi.ceph.com"
	ocsRBDProvisionerSuffix              = ".rbd.csi.ceph.com"
)

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclients,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch;delete;update;patch
//+kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch
//+kubebuilder:rbac:groups="storage.k8s.io",resources=storageclass,verbs=get;list;watch

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
	r.spec = offeringSpec.(*dataFoundationClientSpec)

	return r.reconcilePhases()
}

func (r *dataFoundationClientReconciler) initReconciler(reconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) {
	r.ManagedFusionOfferingReconciler = reconciler
	r.offering = offering

	r.storageClient.Name = storageClientName
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
		if pvCount, err := r.countOCSVolumes(); err != nil {
			return ctrl.Result{}, err
		} else if pvCount > 0 {
			r.Log.Info("PVs provisioned by ODF storage classes exists, cannot proceed with uninstallation", "Number of PVs", pvCount)
			return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
		}

		storageClassClaimList := &ocsclient.StorageClassClaimList{}
		if err := r.unrestrictedList(storageClassClaimList); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list storage class claims: %v", err)
		}
		if len(storageClassClaimList.Items) > 0 {
			var errors []error
			for i := range storageClassClaimList.Items {
				storageClassClaim := &storageClassClaimList.Items[i]
				if storageClassClaim.DeletionTimestamp.IsZero() {
					r.Log.Info("Issuing a delete for storageClassClaim %s", storageClassClaim.Name)
					if err := r.unrestrictedDelete(storageClassClaim); err != nil {
						wrapped := fmt.Errorf(
							"failed to issue a delete for storageClassClaim %s: %v",
							storageClassClaim.Name,
							err,
						)
						errors = append(errors, wrapped)
					}
				}
			}
			if len(errors) > 0 {
				return ctrl.Result{}, fmt.Errorf("failed to issue delete for one or more StorageClassClaim CRs, %v", errors)
			}
		}

		r.Log.Info("issuing a delete for StorageClient")
		if err := r.delete(&r.storageClient); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to issue a delete for Storage Client: %v", err)
		}
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

	if r.spec.ProviderEndpoint == "" {
		return fmt.Errorf("invalid provider endpoint, empty string")
	}

	if r.spec.OnboardingTicket == "" {
		return fmt.Errorf("invalid onboarding ticket, empty string")

	}
	_, err := r.CreateOrUpdate(&r.storageClient, func() error {
		if err := r.own(&r.storageClient, true); err != nil {
			return err
		}
		r.storageClient.Spec.OnboardingTicket = r.spec.OnboardingTicket
		r.storageClient.Spec.StorageProviderEndpoint = r.spec.ProviderEndpoint
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

func (r *dataFoundationClientReconciler) countOCSVolumes() (int, error) {
	storageClassList := &storagev1.StorageClassList{}
	if err := r.unrestrictedList(storageClassList); err != nil {
		return 0, fmt.Errorf("failed to list Storage Classes: %v", err)
	}
	ocsStorageClasses := map[string]bool{}
	for i := range storageClassList.Items {
		storageClass := &storageClassList.Items[i]
		if strings.HasSuffix(storageClass.Provisioner, ocsRBDProvisionerSuffix) ||
			strings.HasSuffix(storageClass.Provisioner, ocsCephFsProvisionerSuffix) {
			ocsStorageClasses[storageClass.Name] = true
		}
	}
	pvList := &corev1.PersistentVolumeList{}
	pvCount := 0
	if err := r.unrestrictedList(pvList); err != nil {
		return 0, fmt.Errorf("failed to list persistent volumes: %v", err)
	}
	for i := range pvList.Items {
		scName := pvList.Items[i].Spec.StorageClassName
		if ocsStorageClasses[scName] {
			pvCount++
		}
	}

	return pvCount, nil
}

func dfcIsReadyToBeRemoved(r *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) (bool, error) {
	storageClient := &ocsclient.StorageClient{}
	storageClient.Name = storageClientName
	storageClient.Namespace = offering.Namespace

	if err := r.get(storageClient); err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		} else {
			return false, err
		}
	} else {
		return false, nil
	}
}
