package controllers

import (
	"fmt"
	"net/url"

	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	ocsclient "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

type dataFoundationClientSpec struct {
	onboardingTicket string
	providerEndpoint string
}

type dataFoundationClientReconciler struct {
	*ManagedFusionOfferingReconciler

	offering                 *v1alpha1.ManagedFusionOffering
	dataFoundationClientSpec dataFoundationClientSpec
	storageClient            ocsclient.StorageClient
}

//+kubebuilder:rbac:groups=ocs.openshift.io,namespace=system,resources=storageclients,verbs=get;list;watch;create;update;patch;delete

func dfcSetupWatches(controllerBuilder *builder.Builder) {
	controllerBuilder.
		Owns(&ocsclient.StorageClient{})
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
	} else {
		if err := r.reconcileStorageClient(); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *dataFoundationClientReconciler) reconcileStorageClient() error {
	r.Log.Info("Reconciling StorageClient")

	if r.dataFoundationClientSpec.providerEndpoint == "" {
		return fmt.Errorf("invalid provider endpoint, empty string")
	}

	providerEndpoint, err := url.ParseRequestURI(r.dataFoundationClientSpec.providerEndpoint)
	if err != nil {
		return fmt.Errorf("failed to parse %s, %v", r.dataFoundationClientSpec.providerEndpoint, err)
	}
	if providerEndpoint.Host == "" {
		return fmt.Errorf("invalid provider endpoint %s, does not contain host", r.dataFoundationClientSpec.providerEndpoint)
	}

	if r.dataFoundationClientSpec.onboardingTicket == "" {
		return fmt.Errorf("invalid onboarding ticket, empty string")

	}
	_, err = r.CreateOrUpdate(&r.storageClient, func() error {
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
