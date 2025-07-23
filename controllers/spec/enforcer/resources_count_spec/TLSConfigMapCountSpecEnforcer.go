package resources_count_spec

import (
	"errors"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/operation"
	"reactive-tech.io/kubegres/controllers/spec/template"
	"reactive-tech.io/kubegres/controllers/states"
)

type TLSConfigMapCountSpecEnforcer struct {
	kubegresContext   ctx.KubegresContext
	resourcesStates   states.ResourcesStates
	resourcesCreator  template.ResourcesCreatorFromTemplate
	blockingOperation *operation.BlockingOperation
}

func CreateTLSConfigMapCountSpecEnforcer(kubegresContext ctx.KubegresContext,
	resourcesStates states.ResourcesStates,
	resourcesCreator template.ResourcesCreatorFromTemplate,
	blockingOperation *operation.BlockingOperation) TLSConfigMapCountSpecEnforcer {

	return TLSConfigMapCountSpecEnforcer{
		kubegresContext:   kubegresContext,
		resourcesStates:   resourcesStates,
		resourcesCreator:  resourcesCreator,
		blockingOperation: blockingOperation,
	}
}

func (r *TLSConfigMapCountSpecEnforcer) CreateOperationConfig() operation.BlockingOperationConfig {
	return operation.BlockingOperationConfig{
		OperationId:      operation.OperationIdTLSConfigMapCountSpecEnforcement,
		StepId:           operation.OperationStepIdTLSConfigMapDeploying,
		TimeOutInSeconds: 10,
		CompletionChecker: func(operation v1.KubegresBlockingOperation) bool {
			return r.isBaseConfigMapUpdated(r.kubegresContext.Kubegres.Spec.TLS.Enabled)
		},
	}
}

func (r *TLSConfigMapCountSpecEnforcer) EnforceSpec() error {
	if r.blockingOperation.IsActiveOperationIdDifferentOf(operation.OperationIdTLSConfigMapCountSpecEnforcement) {
		return nil
	}

	if r.hasLastAttemptTimedOut() {

		if r.isBaseConfigMapUpdated(r.kubegresContext.Kubegres.Spec.TLS.Enabled) {
			r.blockingOperation.RemoveActiveOperation()
		} else {
			r.logTimedOut()
			return nil
		}
	}

	if r.isBaseConfigMapUpdated(r.kubegresContext.Kubegres.Spec.TLS.Enabled) {
		return nil
	}

	configMap, err := r.resourcesCreator.CreateBaseConfigMap()
	if err != nil {
		return err
	}

	err = r.updateConfigMapKeys(&configMap)
	if err != nil {
		return err
	}

	if err := r.updateConfigMap(&configMap); err != nil {
		return err
	}

	return nil
}

func (r *TLSConfigMapCountSpecEnforcer) updateConfigMapKeys(cm *corev1.ConfigMap) error {
	keyUpdates, err := r.resourcesCreator.CreateTLSConfigMapKeyUpdates()
	if err != nil {
		r.kubegresContext.Log.ErrorEvent("TLSConfigMapKeyUpdatesCreationErr", err,
			"Unable to create TLS ConfigMap key updates.",
			"ConfigMap name", cm.Name)
		r.blockingOperation.RemoveActiveOperation()
		return err
	}

	if r.kubegresContext.Kubegres.Spec.TLS.Enabled {
		for key, value := range keyUpdates {
			cm.Data[key] = string(value)
		}
		return nil
	}

	for key := range keyUpdates {
		delete(cm.Data, key)
	}
	return nil
}

func (r *TLSConfigMapCountSpecEnforcer) updateConfigMap(cm *corev1.ConfigMap) error {
	r.kubegresContext.Log.Info("Updating Base ConfigMap with TLS settings", "name", cm.Name,
		"TLS enabled", r.kubegresContext.Kubegres.Spec.TLS.Enabled)

	if err := r.activateBlockingOperation(); err != nil {
		r.kubegresContext.Log.ErrorEvent("TLSBaseConfigMapActivationErr", err,
			"Unable to activate blocking operation for TLS ConfigMap deployment.",
			"ConfigMap name", cm.Name)
		return err
	}

	if err := r.kubegresContext.Client.Update(r.kubegresContext.Ctx, cm); err != nil {
		r.kubegresContext.Log.ErrorEvent("TLSBaseConfigMapUpdateErr", err,
			"Unable to update Base ConfigMap object with TLS configuration.",
			"ConfigMap name", cm.Name)
		r.blockingOperation.RemoveActiveOperation()
		return err
	}

	r.kubegresContext.Log.InfoEvent("TLSBaseConfigMapUpdated", "Base ConfigMap object updated with TLS configuration.",
		"ConfigMap name", cm.Name)
	return nil
}

func (r *TLSConfigMapCountSpecEnforcer) hasLastAttemptTimedOut() bool {
	return r.blockingOperation.HasActiveOperationIdTimedOut(operation.OperationIdTLSConfigMapCountSpecEnforcement)
}

func (r *TLSConfigMapCountSpecEnforcer) logTimedOut() {
	operationTimeOutStr := strconv.FormatInt(r.CreateOperationConfig().TimeOutInSeconds, 10)
	r.kubegresContext.Log.ErrorEvent("TLSConfigMapCountSpecEnforcementTimedOut", errors.New("TLS ConfigMap deployment timed-out"),
		"Last attempt to update base ConfigMap with TLS settings has timed-out after "+operationTimeOutStr+" seconds. ")
}

func (r *TLSConfigMapCountSpecEnforcer) isBaseConfigMapUpdated(tlsEnabled bool) bool {
	return r.resourcesStates.Config.IsBaseConfigDeployed &&
		(tlsEnabled && r.resourcesStates.Config.IsTLSConfigDeployed ||
			!tlsEnabled && !r.resourcesStates.Config.IsTLSConfigDeployed)
}

func (r *TLSConfigMapCountSpecEnforcer) activateBlockingOperation() error {
	return r.blockingOperation.ActivateOperation(operation.OperationIdTLSConfigMapCountSpecEnforcement,
		operation.OperationStepIdTLSConfigMapDeploying)
}
