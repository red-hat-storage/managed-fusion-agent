package controllers

import (
	"fmt"
	"strconv"
	"strings"

	v1alpha1 "github.com/red-hat-storage/managed-fusion-agent/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
)

type dataFoundationSpec struct {
	usableCapacityInTiB     int
	onboardingValidationKey string
}

type dataFoundationReconciler struct {
	*ManagedFusionOfferingReconciler

	spec                          dataFoundationSpec
	onboardingValidationKeySecret *corev1.Secret
}

func dfSetupWatches(controllerBuilder *builder.Builder) {
	controllerBuilder.
		Owns(&corev1.Secret{})
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

	return nil
}

func (r *dataFoundationReconciler) initReconciler(offeringReconciler *ManagedFusionOfferingReconciler) {
	r.ManagedFusionOfferingReconciler = offeringReconciler

	r.onboardingValidationKeySecret = &corev1.Secret{}
	r.onboardingValidationKeySecret.Name = "onboarding-ticket-key"
	r.onboardingValidationKeySecret.Namespace = r.Namespace
}

func (r *dataFoundationReconciler) parseSpec(offering *v1alpha1.ManagedFusionOffering) error {
	r.Log.Info("Parsing ManagedFusionOffering Data Foundation spec")

	valid := true
	var usableCapacityInTiB int
	var err error
	if usableCapacityInTiBAsString, found := offering.Spec.Config["usableCapacityInTiB"]; !found {
		r.Log.Error(
			fmt.Errorf("missing field: usableCapacityInTiB"),
			"an error occurred while parsing ManagedFusionOffering Data Foundation spec",
		)
		valid = false
	} else if usableCapacityInTiB, err = strconv.Atoi(usableCapacityInTiBAsString); err != nil {
		r.Log.Error(
			fmt.Errorf("error parsing usableCapacityInTib: %v", err),
			"an error occurred while parsing ManagedFusionOffering Data Foundation spec",
		)
		valid = false
	}

	var onboardingValidationKeyAsString string
	var found bool
	if onboardingValidationKeyAsString, found = offering.Spec.Config["onboardingValidationKey"]; !found {
		r.Log.Error(
			fmt.Errorf("missing field: onboardingValidationKey"),
			"an error occurred while parsing ManagedFusionOffering Data Foundation spec",
		)
		valid = false
	}

	if !valid {
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
