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

package statefulset_spec

import (
	"reflect"

	apps "k8s.io/api/apps/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/spec/template"
)

type LivenessProbeSpecEnforcer struct {
	kubegresContext     ctx.KubegresContext
	tlsConfigSpecHelper template.TLSConfigSpecHelper
}

func CreateLivenessProbeSpecEnforcer(kubegresContext ctx.KubegresContext, tlsConfigSpecHelper template.TLSConfigSpecHelper) LivenessProbeSpecEnforcer {
	return LivenessProbeSpecEnforcer{
		kubegresContext:     kubegresContext,
		tlsConfigSpecHelper: tlsConfigSpecHelper,
	}
}

func (r *LivenessProbeSpecEnforcer) GetSpecName() string {
	return "LivenessProbe"
}

func (r *LivenessProbeSpecEnforcer) CheckForSpecDifference(statefulSet *apps.StatefulSet) StatefulSetSpecDifference {
	current := statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe
	expected := r.kubegresContext.Kubegres.Spec.Probe.LivenessProbe

	if expected == nil {
		// If the expected liveness probe is using the default value,
		// let's create a copy of the current one with the TLS defaults applied to compare.
		expected = current.DeepCopy()
		r.tlsConfigSpecHelper.OverrideDefaultLivenessProbeWithTLS(expected)
	}

	if !reflect.DeepEqual(current, expected) {
		return StatefulSetSpecDifference{
			SpecName: r.GetSpecName(),
			Current:  current.String(),
			Expected: expected.String(),
		}
	}

	return StatefulSetSpecDifference{}
}

func (r *LivenessProbeSpecEnforcer) EnforceSpec(statefulSet *apps.StatefulSet) (wasSpecUpdated bool, err error) {
	statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe = r.kubegresContext.Kubegres.Spec.Probe.LivenessProbe
	r.tlsConfigSpecHelper.OverrideDefaultLivenessProbeWithTLS(statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe)
	return true, nil
}

func (r *LivenessProbeSpecEnforcer) OnSpecEnforcedSuccessfully(statefulSet *apps.StatefulSet) error {
	return nil
}
