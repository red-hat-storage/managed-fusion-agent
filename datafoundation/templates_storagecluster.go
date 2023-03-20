/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package datafoundation

import (
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	rook "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageClusterTemplate is the template that serves as the base for the storage clsuter deployed by the operator

const (
	OSDSizeInTiB = 4
)

var (
	gp2             = "gp2"
	volumeModeBlock = corev1.PersistentVolumeBlock
)

var commonTSC corev1.TopologySpreadConstraint = corev1.TopologySpreadConstraint{
	MaxSkew:           1,
	TopologyKey:       "topology.kubernetes.io/zone",
	WhenUnsatisfiable: corev1.DoNotSchedule,
	LabelSelector: &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				// Key is from rook/pkg/operator/ceph/cluster/osd/labels.go
				Key:      "ceph.rook.io/pvc",
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	},
}

var preparePlacementTSC corev1.TopologySpreadConstraint = corev1.TopologySpreadConstraint{
	MaxSkew:           1,
	TopologyKey:       "kubernetes.io/hostname",
	WhenUnsatisfiable: corev1.DoNotSchedule,
	LabelSelector: &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				// Key is from rook/pkg/operator/ceph/cluster/osd/labels.go
				Key:      "ceph.rook.io/pvc",
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	},
}

var StorageClusterTemplate = ocsv1.StorageCluster{
	Spec: ocsv1.StorageClusterSpec{
		// The label selector is used to select only the worker nodes for
		// both labeling and scheduling.
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "node-role.kubernetes.io/worker",
				Operator: metav1.LabelSelectorOpExists,
			}, {
				Key:      "node-role.kubernetes.io/infra",
				Operator: metav1.LabelSelectorOpDoesNotExist,
			}},
		},
		ManageNodes: false,
		MonPVCTemplate: &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &gp2,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"storage": resource.MustParse("50Gi"),
					},
				},
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
			},
		},
		Resources: map[string]corev1.ResourceRequirements{
			"mds":            GetResourceRequirements("mds"),
			"mgr":            GetResourceRequirements("mgr"),
			"mon":            GetResourceRequirements("mon"),
			"crashcollector": GetResourceRequirements("crashcollector"),
		},
		StorageProfiles: []ocsv1.StorageProfile{{
			DeviceClass: "ssd",
			Name:        "default",
			BlockPoolConfiguration: ocsv1.BlockPoolConfigurationSpec{
				Parameters: map[string]string{
					"pg_autoscale_mode": "on",
					"pg_num":            "128",
					"pgp_num":           "128",
				},
			},
			SharedFilesystemConfiguration: ocsv1.SharedFilesystemConfigurationSpec{
				Parameters: map[string]string{
					"pg_autoscale_mode": "on",
					"pg_num":            "128",
					"pgp_num":           "128",
				},
			},
		}},
		StorageDeviceSets: []ocsv1.StorageDeviceSet{{
			Name:  "default",
			Count: 1,
			Replica:     3,
			DeviceClass: "ssd",
			DataPVCTemplate: corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &gp2,
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					VolumeMode: &volumeModeBlock,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"storage": resource.MustParse(fmt.Sprintf("%dTi", OSDSizeInTiB)),
						},
					},
				},
			},
			Placement: rook.Placement{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					commonTSC,
				},
			},
			PreparePlacement: rook.Placement{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					commonTSC,
					preparePlacementTSC,
				},
			},
			Portable:    true,
			Resources:   GetResourceRequirements("sds"),
		}},
		MultiCloudGateway: &ocsv1.MultiCloudGatewaySpec{
			ReconcileStrategy: "ignore",
		},
		HostNetwork:                 true,
		AllowRemoteStorageConsumers: true,
		ManagedResources: ocsv1.ManagedResourcesSpec{
			CephBlockPools: ocsv1.ManageCephBlockPools{
				ReconcileStrategy:    "ignore",
				DisableStorageClass:  true,
				DisableSnapshotClass: true,
			},
			CephFilesystems: ocsv1.ManageCephFilesystems{
				DisableStorageClass:  true,
				DisableSnapshotClass: true,
			},
		},
		DefaultStorageProfile: "default",
	},
}
