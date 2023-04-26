package main

import (
	"context"
	"fmt"
	"os"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const baseDir string = "shim/crds"

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;watch;list;create

func main() {
	ctx := context.Background()

	klog.Info("Setting up k8s client")

	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Errorf("unable to get incluster config: %v", err)
		os.Exit(1)
	}

	clientset, err := clientset.NewForConfig(config)
	if err != nil {
		klog.Errorf("error creating client: %v", err)
		os.Exit(1)
	}

	crdFiles := []string{
		"noobaas.noobaa.io.yaml",
		"objectbucketclaims.objectbucket.io.yaml",
		"objectbuckets.objectbucket.io.yaml",
	}

	for _, f := range crdFiles {
		file, err := os.Open(fmt.Sprintf("%s/%s", baseDir, f))
		if err != nil {
			klog.Errorf("unable to read crd templates: %v", err)
			os.Exit(1)
		}
		crd := &apiextensionsv1.CustomResourceDefinition{}
		decoder := yaml.NewYAMLToJSONDecoder(file)
		if err := decoder.Decode(crd); err != nil {
			klog.Errorf("unable to decode: %v", err)
			os.Exit(1)
		}
		if _, err := clientset.ApiextensionsV1().CustomResourceDefinitions().
			Create(ctx, crd, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			klog.Errorf("unable to create %s CRD: %v", crd.GetName(), err)
			os.Exit(1)
		} else if err != nil && apierrors.IsAlreadyExists(err) {
			klog.Infof("%s CRD already exists", crd.GetName())
		} else {
			klog.Infof("%s CRD Created", crd.GetName())
		}
	}
}
