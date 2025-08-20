package enforcer

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	apiv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/ctx/resources"
	"reactive-tech.io/kubegres/controllers/spec/enforcer/resources_count_spec"
)

var _ resources_count_spec.ResourceCountSpecEnforcer = (*TLSModeTransitEnforcer)(nil)

type TLSModeTransitEnforcer struct {
	kubegresContext ctx.KubegresContext
	resourceContext *resources.ResourcesContext
}

func CreateTLSModeTransitEnforcer(resourceContext *resources.ResourcesContext) TLSModeTransitEnforcer {
	return TLSModeTransitEnforcer{
		kubegresContext: resourceContext.KubegresContext,
		resourceContext: resourceContext,
	}
}

func (t TLSModeTransitEnforcer) LoadCurrentState() {
	tlsTransition := t.kubegresContext.Status.GetTLSTransition()
	if tlsTransition.TransitionInProgress {
		switch tlsTransition.TransitState {
		case apiv1.TLSTransitStateToSecure:
			t.kubegresContext.Log.Info("TLSModeTransitEnforcer: TLS transition is in progress, loading Secure state.")
			t.kubegresContext.Kubegres.Spec.TLS = tlsTransition.SecureSpec
			t.kubegresContext.Kubegres.Spec.TLS.SSLMode = tlsTransition.CurrentTransitMode
		case apiv1.TLSTransitStateToInsecure:
			t.kubegresContext.Log.Info("TLSModeTransitEnforcer: TLS transition is in progress, loading Insecure state.")
			t.kubegresContext.Kubegres.Spec.TLS = tlsTransition.InsecureSpec
			t.kubegresContext.Kubegres.Spec.TLS.SSLMode = tlsTransition.CurrentTransitMode
		}
	}
}

func (t TLSModeTransitEnforcer) EnforceSpec() error {
	defer t.kubegresContext.Log.Info("**********************          **********************")
	defer t.LoadCurrentState()

	t.kubegresContext.Log.Info("**********************          **********************")
	t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Enforcing TLS mode", "Enabled", t.kubegresContext.Kubegres.Spec.TLS.Enabled)

	// If TLS is not enabled, ensure we treat SSL mode as disable.
	if !t.kubegresContext.Kubegres.Spec.TLS.Enabled {
		t.kubegresContext.Kubegres.Spec.TLS.SSLMode = apiv1.SSLModeDisable
	}

	if t.resourceContext.BlockingOperation.GetActiveOperation().OperationId != "" {
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Blocking operation is active, skipping TLS mode transition enforcement.")
		return nil
	}

	if t.resourceContext.ResourcesStates.StatefulSets.NbreDeployed == 0 {
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: No StatefulSets deployed, assuming first time deploy. No TLS mode transition needed.")
		tlsTransition := apiv1.TLSTransition{
			TransitionInProgress: false,
			CurrentTransitMode:   t.kubegresContext.Kubegres.Spec.TLS.SSLMode,
		}
		if t.kubegresContext.Kubegres.Spec.TLS.Enabled {
			tlsTransition.SecureSpec = t.kubegresContext.Kubegres.Spec.TLS
		} else {
			tlsTransition.InsecureSpec = t.kubegresContext.Kubegres.Spec.TLS
		}
		t.kubegresContext.Status.SetTLSTransition(tlsTransition)
		return nil
	}

	if t.resourceContext.ResourcesStates.StatefulSets.NbreDeployed != *t.kubegresContext.Kubegres.Spec.Replicas {
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: StatefulSets deployed count does not match the desired replicas count, skipping TLS mode transition enforcement.")
		return nil
	}

	tlsTransition := t.kubegresContext.Status.GetTLSTransition()

	// If the tls transition is in progress and the current transit mode is the same as the current spec SSL mode,
	// wait until all StatefulSets are ready with the current spec.
	if tlsTransition.TransitionInProgress && tlsTransition.CurrentTransitMode == t.kubegresContext.Kubegres.Spec.TLS.SSLMode {
		for _, ss := range t.resourceContext.ResourcesStates.StatefulSets.All.GetAllReverseSortedByInstanceIndex() {
			if !ss.IsReady {
				t.kubegresContext.Log.Info("TLSModeTransitEnforcer: StatefulSet " + ss.StatefulSet.Name + " is not ready, skipping TLS mode transition enforcement.")
				return nil
			}
			ssCopy := ss
			diff := t.resourceContext.StatefulSetsSpecsEnforcer.CheckForSpecDifferences(&ssCopy.StatefulSet)
			if diff.IsThereDifference() {
				t.kubegresContext.Log.Info("TLSModeTransitEnforcer: StatefulSet " + ss.StatefulSet.Name + " has spec differences, " +
					"skipping TLS mode transition enforcement until all StatefulSets are updated to the current spec.")
				return nil
			}
		}
	}

	switch tlsTransition.TransitState {
	case apiv1.TLSTransitStateNone:
		if t.kubegresContext.Kubegres.Spec.TLS.Enabled {
			diff := cmp.Diff(tlsTransition.SecureSpec, t.kubegresContext.Kubegres.Spec.TLS)
			t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Diff (-secure,+current): " + diff)
			if diff == "" {
				t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Secure TLS spec matches the current TLS spec, no transition needed.")
				return nil
			}
			tlsTransition.SecureSpec = t.kubegresContext.Kubegres.Spec.TLS
		} else {
			diff := cmp.Diff(tlsTransition.InsecureSpec, t.kubegresContext.Kubegres.Spec.TLS)
			t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Diff (-insecure,+current): " + diff)
			if diff == "" {
				t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Insecure TLS spec matches the current TLS spec, no transition needed.")
				return nil
			}
			tlsTransition.InsecureSpec = t.kubegresContext.Kubegres.Spec.TLS
		}

	}

	newSSLMode, transitState, changed := t.transitSSLMode(tlsTransition)
	tlsTransition.CurrentTransitMode = newSSLMode
	tlsTransition.TransitState = transitState

	if changed {
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: TLS mode transition needed, new SSL mode: " + newSSLMode.String())
		tlsTransition.TransitionInProgress = true
		t.kubegresContext.Status.SetTLSTransition(tlsTransition)
		return nil
	}

	opInProgress := t.resourceContext.BlockingOperation.GetActiveOperation().OperationId != ""
	tlsTransition.TransitionInProgress = opInProgress
	t.kubegresContext.Log.Info("TLSModeTransitEnforcer: TLS transition matches the desired state: "+newSSLMode.String(), "operationInProgress", opInProgress)

	// reset the transition state to none if the transition is not in progress.
	if !opInProgress {
		tlsTransition.TransitState = apiv1.TLSTransitStateNone
		tlsTransition.SecureSpec = apiv1.TLS{}
		tlsTransition.InsecureSpec = apiv1.TLS{}
		tlsTransition.CurrentTransitMode = t.kubegresContext.Kubegres.Spec.TLS.SSLMode
	}
	t.kubegresContext.Status.SetTLSTransition(tlsTransition)

	return nil
}

func (t TLSModeTransitEnforcer) transitSSLMode(tlsTransition apiv1.TLSTransition) (apiv1.SSLMode, apiv1.TLSTransitState, bool) {
	transitState := tlsTransition.TransitState
	secureSSLMode := tlsTransition.SecureSpec.SSLMode
	insecureSSLMode := tlsTransition.InsecureSpec.SSLMode
	currentSSLMode := tlsTransition.CurrentTransitMode
	transitSSLMode := currentSSLMode

	if transitState == apiv1.TLSTransitStateNone {
		if t.kubegresContext.Kubegres.Spec.TLS.Enabled {
			transitState = apiv1.TLSTransitStateToSecure
		} else {
			transitState = apiv1.TLSTransitStateToInsecure
		}
	}

	// If the desired TLS is disabled, set the desired SSL mode to "disable" regardless of the original state.
	if transitState == apiv1.TLSTransitStateToInsecure && insecureSSLMode == "" {
		insecureSSLMode = apiv1.SSLModeDisable
	}

	switch {
	case transitState == apiv1.TLSTransitStateToSecure && currentSSLMode == secureSSLMode:
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Current SSL mode is the same as secure SSL mode, no transition needed.")
		return secureSSLMode, transitState, false

	case transitState == apiv1.TLSTransitStateToSecure && currentSSLMode.Priority() == secureSSLMode.Priority():
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Current SSL mode has the same priority as secure SSL mode, no transition needed.")
		return secureSSLMode, transitState, true

	case transitState == apiv1.TLSTransitStateToSecure && currentSSLMode.Priority() < secureSSLMode.Priority():
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to secure mode.")
		transitSSLMode = currentSSLMode.Higher()

	case transitState == apiv1.TLSTransitStateToSecure && currentSSLMode.Priority() > secureSSLMode.Priority():
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: A lower SSL mode is requested, running smooth transition to secure mode.")
		transitSSLMode = currentSSLMode.Lower()

	case transitState == apiv1.TLSTransitStateToInsecure && currentSSLMode == insecureSSLMode:
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Current SSL mode is the same as insecure SSL mode, no transition needed.")
		return insecureSSLMode, transitState, false

	case transitState == apiv1.TLSTransitStateToInsecure && currentSSLMode.Priority() == insecureSSLMode.Priority():
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: Current SSL mode has the same priority as insecure SSL mode, no transition needed.")
		return insecureSSLMode, transitState, true

	case transitState == apiv1.TLSTransitStateToInsecure && currentSSLMode.Priority() > insecureSSLMode.Priority():
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: A lower SSL mode is requested, running smooth transition to insecure mode.")
		transitSSLMode = currentSSLMode.Lower()

	case transitState == apiv1.TLSTransitStateToInsecure && currentSSLMode.Priority() < insecureSSLMode.Priority():
		t.kubegresContext.Log.Info("TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to insecure mode.")
		transitSSLMode = currentSSLMode.Higher()
	}

	// If it is transitioning to the same secure priority, return the secure SSL mode.
	// For example, if current is "require" and secure is "verify-ca", return the "verify-ca" mode.
	switch {
	case transitState == apiv1.TLSTransitStateToSecure && transitSSLMode.Priority() == secureSSLMode.Priority():
		t.logTransitionMode(currentSSLMode, secureSSLMode)
		return secureSSLMode, transitState, true

	case transitState == apiv1.TLSTransitStateToInsecure && transitSSLMode.Priority() == insecureSSLMode.Priority():
		t.logTransitionMode(currentSSLMode, insecureSSLMode)
		return insecureSSLMode, transitState, true

	default:
		t.logTransitionMode(currentSSLMode, transitSSLMode)
		return transitSSLMode, transitState, true
	}

}

func (t TLSModeTransitEnforcer) logTransitionMode(from, to apiv1.SSLMode) {
	switch {
	case from.Priority() == to.Priority():
		t.kubegresContext.Log.Info(fmt.Sprintf("TLSModeTransitEnforcer: %s == %s", from, to))
	case from.Priority() > to.Priority():
		t.kubegresContext.Log.Info(fmt.Sprintf("TLSModeTransitEnforcer: %s > %s", from, to))
	case from.Priority() < to.Priority():
		t.kubegresContext.Log.Info(fmt.Sprintf("TLSModeTransitEnforcer: %s < %s", from, to))
	}
}
