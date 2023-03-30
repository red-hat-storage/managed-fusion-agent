package controllers

import (
	"fmt"

	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

type dataFoundationClientSpec struct {
	onboardingTicket string
	providerEndpoint string
}

type dataFoundationClientReconciler struct {
	*ManagedFusionOfferingReconciler

	dataFoundationClientSpec dataFoundationClientSpec
}

func dfcSetupWatches(controllerBuilder *builder.Builder) {}

func DFCAddToScheme(scheme *runtime.Scheme) {
}

func dfcReconcile(offeringReconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) error {
	r := dataFoundationClientReconciler{}
	r.initReconciler(offeringReconciler, offering)

	if err := r.parseSpec(offering); err != nil {
		return err
	}
	return nil
}

func (r *dataFoundationClientReconciler) initReconciler(reconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) {
	r.ManagedFusionOfferingReconciler = reconciler
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
