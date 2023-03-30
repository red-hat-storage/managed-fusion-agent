package controllers

import (
	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

type dataFoundationClientSpec struct {
	onboardingTicket  string
	storageDFEndpoint string
}

type dataFoundationClientReconciler struct {
	*ManagedFusionOfferingReconciler

	dataFoundationClientSpec dataFoundationClientSpec
}

func dfcSetupWatches(controllerBuilder *builder.Builder) {}

func DFCAddToScheme(scheme *runtime.Scheme) {
}

func dfcReconcile(offeringReconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) error {
	return nil
}

func (r *dataFoundationClientReconciler) initReconciler(offeringReconciler *ManagedFusionOfferingReconciler, offering *v1alpha1.ManagedFusionOffering) {
}

func (r *dataFoundationClientReconciler) parseSpec(offering *v1alpha1.ManagedFusionOffering) error {
	return nil
}

func (r *dataFoundationClientReconciler) reconcileStorageClient() error {
	return nil
}

func (r *dataFoundationClientReconciler) reconcileStorageClassClaim() error {
	return nil
}
