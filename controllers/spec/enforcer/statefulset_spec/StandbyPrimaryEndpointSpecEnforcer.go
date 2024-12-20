/*
Copyright 2023 Reactive Tech Limited.
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
	apps "k8s.io/api/apps/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
)

type StandbyPrimaryEndpointSpecEnforcer struct {
	kubegresContext ctx.KubegresContext
}

func CreateStandbyPrimaryEndpointSpecEnforcer(kubegresContext ctx.KubegresContext) StandbyPrimaryEndpointSpecEnforcer {
	return StandbyPrimaryEndpointSpecEnforcer{kubegresContext: kubegresContext}
}

func (r *StandbyPrimaryEndpointSpecEnforcer) GetSpecName() string {
	return "StandbyPrimaryEndpoint"
}

func (r *StandbyPrimaryEndpointSpecEnforcer) CheckForSpecDifference(statefulSet *apps.StatefulSet) StatefulSetSpecDifference {
	if len(statefulSet.Spec.Template.Spec.InitContainers) == 0 {
		return StatefulSetSpecDifference{}
	}

	current := statefulSet.Spec.Template.Spec.InitContainers[0].Env[0].Value
	expected := r.getExpectedPrimaryServiceName()

	if current != expected {
		return StatefulSetSpecDifference{
			SpecName: r.GetSpecName(),
			Current:  current,
			Expected: expected,
		}
	}

	return StatefulSetSpecDifference{}
}

func (r *StandbyPrimaryEndpointSpecEnforcer) EnforceSpec(statefulSet *apps.StatefulSet) (wasSpecUpdated bool, err error) {
	statefulSet.Spec.Template.Spec.InitContainers[0].Env[0].Value = r.getExpectedPrimaryServiceName()
	return true, nil
}

func (r *StandbyPrimaryEndpointSpecEnforcer) OnSpecEnforcedSuccessfully(_ *apps.StatefulSet) error {
	return nil
}

func (r *StandbyPrimaryEndpointSpecEnforcer) getExpectedPrimaryServiceName() string {
	if r.kubegresContext.Kubegres.Spec.Standby.Enabled {
		return r.kubegresContext.Kubegres.Spec.Standby.PrimaryEndpoint
	}
	return r.kubegresContext.GetServiceResourceName(true)
}
