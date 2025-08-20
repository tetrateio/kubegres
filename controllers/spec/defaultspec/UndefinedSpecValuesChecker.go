/*
Copyright 2021 Reactive Tech Limited.
"Reactive Tech Limited" is a company located in England, United Kingdom.
https://www.reactive-tech.io

Lead Developer: Alex Arica

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

package defaultspec

import (
	"errors"
	"strconv"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
)

type UndefinedSpecValuesChecker struct {
	kubegresContext     ctx.KubegresContext
	defaultStorageClass DefaultStorageClass
}

func SetDefaultForUndefinedSpecValues(kubegresContext ctx.KubegresContext, defaultStorageClass DefaultStorageClass) error {
	defaultSpec := UndefinedSpecValuesChecker{
		kubegresContext:     kubegresContext,
		defaultStorageClass: defaultStorageClass,
	}

	return defaultSpec.apply()
}

func (r *UndefinedSpecValuesChecker) apply() error {

	wasSpecChanged := false
	kubegresSpec := &r.kubegresContext.Kubegres.Spec
	const emptyStr = ""

	if kubegresSpec.Port <= 0 {
		wasSpecChanged = true
		kubegresSpec.Port = ctx.DefaultContainerPortNumber
		r.createLog("spec.port", strconv.Itoa(int(kubegresSpec.Port)))
	}

	if kubegresSpec.Database.VolumeMount == emptyStr {
		wasSpecChanged = true
		kubegresSpec.Database.VolumeMount = ctx.DefaultDatabaseVolumeMount
		r.createLog("spec.database.volumeMount", kubegresSpec.Database.VolumeMount)
	}

	if kubegresSpec.CustomConfig == emptyStr {
		wasSpecChanged = true
		kubegresSpec.CustomConfig = ctx.BaseConfigMapName
		r.createLog("spec.customConfig", kubegresSpec.CustomConfig)
	}

	if r.isStorageClassNameUndefinedInSpec() {
		wasSpecChanged = true
		defaultStorageClassName, err := r.defaultStorageClass.GetDefaultStorageClassName()
		if err != nil {
			return err
		}

		kubegresSpec.Database.StorageClassName = &defaultStorageClassName
		r.createLog("spec.Database.StorageClassName", defaultStorageClassName)
	}

	if kubegresSpec.Scheduler.Affinity == nil {
		kubegresSpec.Scheduler.Affinity = r.createDefaultAffinity()
		wasSpecChanged = true
		r.createLog("spec.Affinity", kubegresSpec.Scheduler.Affinity.String())
	}

	if kubegresSpec.TLS.Enabled {
		if kubegresSpec.TLS.SSLMode == "" {
			return errors.New("spec.TLS.SSLMode must be specified when spec.TLS.Enabled=true")
		}
		if kubegresSpec.TLS.MountPath == "" {
			wasSpecChanged = true
			kubegresSpec.TLS.MountPath = ctx.DefaultTLSMountPath
			r.createLog("spec.TLS.MountPath", kubegresSpec.TLS.MountPath)
		}
		if kubegresSpec.TLS.RootCertPath == "" {
			wasSpecChanged = true
			kubegresSpec.TLS.RootCertPath = ctx.DefaultTLSRootCertPath
			r.createLog("spec.TLS.RootCertPath", kubegresSpec.TLS.RootCertPath)
		}
		if kubegresSpec.TLS.ServerCertPath == "" {
			wasSpecChanged = true
			kubegresSpec.TLS.ServerCertPath = ctx.DefaultTLSServerCertPath
			r.createLog("spec.TLS.ServerCertPath", kubegresSpec.TLS.ServerCertPath)
		}
		if kubegresSpec.TLS.ServerKeyPath == "" {
			wasSpecChanged = true
			kubegresSpec.TLS.ServerKeyPath = ctx.DefaultTLSServerKeyPath
			r.createLog("spec.TLS.ServerKeyPath", kubegresSpec.TLS.ServerKeyPath)
		}
		if kubegresSpec.TLS.ClientCertPath == "" {
			wasSpecChanged = true
			kubegresSpec.TLS.ClientCertPath = ctx.DefaultTLSClientCertPath
			r.createLog("spec.TLS.ClientCertPath", kubegresSpec.TLS.ClientCertPath)
		}
		if kubegresSpec.TLS.ClientKeyPath == "" {
			wasSpecChanged = true
			kubegresSpec.TLS.ClientKeyPath = ctx.DefaultTLSClientKeyPath
			r.createLog("spec.TLS.ClientKeyPath", kubegresSpec.TLS.ClientKeyPath)
		}

	}

	if wasSpecChanged {
		return r.updateSpec()
	}

	return nil
}

func (r *UndefinedSpecValuesChecker) createLog(specName string, specValue string) {
	r.kubegresContext.Log.InfoEvent("DefaultSpecValue", "A default value was set for a field in Kubegres YAML spec.", specName, "New value: "+specValue+"")
}

func (r *UndefinedSpecValuesChecker) isStorageClassNameUndefinedInSpec() bool {
	storageClassName := r.kubegresContext.Kubegres.Spec.Database.StorageClassName
	return storageClassName == nil || *storageClassName == ""
}

func (r *UndefinedSpecValuesChecker) updateSpec() error {
	r.kubegresContext.Log.Info("Updating Kubegres Spec", "name", r.kubegresContext.Kubegres.Name)
	return r.kubegresContext.Client.Update(r.kubegresContext.Ctx, r.kubegresContext.Kubegres)
}

func (r *UndefinedSpecValuesChecker) createDefaultAffinity() *core.Affinity {

	resourceName := r.kubegresContext.Kubegres.Name

	weightedPodAffinityTerm := core.WeightedPodAffinityTerm{
		Weight: 100,
		PodAffinityTerm: core.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "app",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{resourceName},
					},
				},
			},
			TopologyKey: "kubernetes.io/hostname",
		},
	}

	return &core.Affinity{
		PodAntiAffinity: &core.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []core.WeightedPodAffinityTerm{weightedPodAffinityTerm},
		},
	}
}
