package statefulset_spec

import (
	apps "k8s.io/api/apps/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/spec/template"
)

type TLSSpecEnforcer struct {
	kubegresContext     ctx.KubegresContext
	tlsConfigSpecHelper template.TLSConfigSpecHelper
}

func CreateTLSSpecEnforcer(kubegresContext ctx.KubegresContext, tlsConfigSpecHelper template.TLSConfigSpecHelper) TLSSpecEnforcer {
	return TLSSpecEnforcer{
		kubegresContext:     kubegresContext,
		tlsConfigSpecHelper: tlsConfigSpecHelper,
	}
}

func (r *TLSSpecEnforcer) GetSpecName() string {
	return "TLS"
}

func (r *TLSSpecEnforcer) isEnabled() bool {
	return r.kubegresContext.Kubegres.Spec.TLS.Enabled
}

func (r *TLSSpecEnforcer) CheckForSpecDifference(statefulSet *apps.StatefulSet) StatefulSetSpecDifference {
	deepCopy := statefulSet.DeepCopy()
	expected, current, updated := r.tlsConfigSpecHelper.ConfigureStatefulSet(deepCopy)
	if updated {
		return StatefulSetSpecDifference{
			SpecName: "TLS (StatefulSet)",
			Current:  current,
			Expected: expected,
		}
	}

	return StatefulSetSpecDifference{}
}

func (r *TLSSpecEnforcer) EnforceSpec(statefulSet *apps.StatefulSet) (bool, error) {
	_, _, ok := r.tlsConfigSpecHelper.ConfigureStatefulSet(statefulSet)
	return ok, nil
}

func (r *TLSSpecEnforcer) OnSpecEnforcedSuccessfully(*apps.StatefulSet) error {
	// No specific actions needed after enforcing TLS spec
	return nil
}
