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

package statefulset

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	postgresV1 "reactive-tech.io/kubegres/api/v1"
	kubegresCtx "reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/operation"
	"reactive-tech.io/kubegres/controllers/spec/template"
	"reactive-tech.io/kubegres/controllers/states"
	"reactive-tech.io/kubegres/controllers/states/statefulset"
	"reactive-tech.io/kubegres/internal/replicationslot"
	replicationSlotRepo "reactive-tech.io/kubegres/internal/replicationslot/repo"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type ReplicaDbCountSpecEnforcer struct {
	kubegresContext               kubegresCtx.KubegresContext
	resourcesStates               states.ResourcesStates
	resourcesCreator              template.ResourcesCreatorFromTemplate
	blockingOperation             *operation.BlockingOperation
	replicationSlotsCreateDeleter replicationSlotsCreateDeleter
}

type replicationSlotsCreateDeleter interface {
	CreateFor(*v1.StatefulSet) (*v1.StatefulSet, error)
	DeleteFor(*v1.StatefulSet) error
	GetFor(*v1.StatefulSet) (replicationslot.ReplicationSlot, error)
}

type noopReplicationSlotsCreateDeleter struct{}

func (n noopReplicationSlotsCreateDeleter) GetFor(_ *v1.StatefulSet) (replicationslot.ReplicationSlot, error) {
	return replicationslot.ReplicationSlot{}, nil
}

func (n noopReplicationSlotsCreateDeleter) CreateFor(ss *v1.StatefulSet) (*v1.StatefulSet, error) {
	return ss, nil
}
func (n noopReplicationSlotsCreateDeleter) DeleteFor(*v1.StatefulSet) error { return nil }

func CreateReplicaDbCountSpecEnforcer(
	kubegresContext kubegresCtx.KubegresContext,
	resourcesStates states.ResourcesStates,
	resourcesCreator template.ResourcesCreatorFromTemplate,
	blockingOperation *operation.BlockingOperation,
	clusterName string,
) (ReplicaDbCountSpecEnforcer, error) {

	enforcer := ReplicaDbCountSpecEnforcer{
		kubegresContext:               kubegresContext,
		resourcesStates:               resourcesStates,
		resourcesCreator:              resourcesCreator,
		blockingOperation:             blockingOperation,
		replicationSlotsCreateDeleter: noopReplicationSlotsCreateDeleter{},
	}

	// TODO(piotrkpc): should this be here ? not testable code really
	if kubegresContext.Kubegres.Spec.ReplicationSlots.Enabled {
		sqlConn, ok := kubegresContext.GetSQLConnection()
		if !ok {
			return ReplicaDbCountSpecEnforcer{}, errors.New("get SQL connection from kubegresContext")
		}
		enforcer.replicationSlotsCreateDeleter = newSimpleReplicationSlotsCreateDeleter(
			kubegresContext,
			resourcesStates,
			replicationSlotRepo.New(sqlConn.DB()),
			clusterName,
		)
	}

	return enforcer, nil
}

type simpleReplicationSlotCreateDeleter struct {
	repo            replicationSlotRepo.Repository
	states          states.ResourcesStates
	kubegresContext kubegresCtx.KubegresContext
	clusterName     string
}

var errNotFound = errors.New("not found")

func (r *simpleReplicationSlotCreateDeleter) GetFor(set *v1.StatefulSet) (replicationslot.ReplicationSlot, error) {
	replicationSlotName, err := buildReplicationSlotName(r.kubegresContext.Kubegres.GetName(), r.clusterName, set.GetName(), r.kubegresContext.ClusterRole())
	if err != nil {
		return replicationslot.ReplicationSlot{}, fmt.Errorf("replication slot name: %v: %w", ctrlclient.ObjectKeyFromObject(set), err)
	}
	replicationSlot, err := r.repo.GetSlot(r.kubegresContext.Ctx, replicationSlotName)
	if err != nil {
		if errors.Is(err, replicationSlotRepo.ErrNotFound) {
			return replicationslot.ReplicationSlot{}, errNotFound
		}
		return replicationslot.ReplicationSlot{}, err
	}
	return replicationSlot, nil
}

func (r *simpleReplicationSlotCreateDeleter) CreateFor(statefulSet *v1.StatefulSet) (*v1.StatefulSet, error) {
	replicationSlotName, err := buildReplicationSlotName(r.kubegresContext.Kubegres.GetName(), r.clusterName, statefulSet.GetName(), r.kubegresContext.ClusterRole())
	objKey := ctrlclient.ObjectKeyFromObject(statefulSet)
	if err != nil {
		r.kubegresContext.Log.ErrorEvent("ReplicationSlotCreate", fmt.Errorf("replication slot name: %v: %w", objKey, err), "Failed to create replication slot name")
		return nil, err
	}
	slot, err := r.repo.CreateSlot(r.kubegresContext.Ctx, replicationSlotName)
	if err != nil {
		if errors.Is(err, replicationSlotRepo.ErrAlreadyExist) {
			r.kubegresContext.Log.WarningEvent("ReplicationSlotCreate", fmt.Sprintf("Replication slot '%s' for statefulSet '%s' already exists, skipping creation.", replicationSlotName, statefulSet.GetName()))
			return nil, nil
		}
		r.kubegresContext.Log.ErrorEvent("ReplicationSlotCreate", fmt.Errorf("replication slot: %v for statefulSet: %v: %w", slot, objKey, err), "Failed to create replication slot")
		return nil, err
	}

	ss := statefulSet.DeepCopy()
	for i, container := range ss.Spec.Template.Spec.Containers {
		ss.Spec.Template.Spec.Containers[i].Env = append(container.Env, corev1.EnvVar{
			Name:  kubegresCtx.EnvVarReplicationSlotName,
			Value: slot.Name,
		})
	}
	for i, container := range ss.Spec.Template.Spec.InitContainers {
		ss.Spec.Template.Spec.InitContainers[i].Env = append(container.Env, corev1.EnvVar{
			Name:  kubegresCtx.EnvVarReplicationSlotName,
			Value: slot.Name,
		})
	}
	return ss, nil
}

func buildReplicationSlotName(kubegresName, clusterName, statefulSetName string, role kubegresCtx.ClusterRole) (string, error) {
	// if any of this are empty, we cannot create a valid replication slot name
	if kubegresName == "" || statefulSetName == "" || role == "" {
		return "", fmt.Errorf("replication slot name cannot be created for statefulSet '%s' because it has empty 'name', 'role' or label", statefulSetName)
	}

	replicationSlotName := fmt.Sprintf("%s_%s_%s_%s", kubegresName, clusterName, role, statefulSetName)
	replicationSlotName = strings.ReplaceAll(replicationSlotName, "-", "_") // Replace dashes with underscores to ensure compatibility with PostgreSQL slot names.

	// Validate that the generated name contains only allowed characters.
	// This is a safeguard against any other unexpected characters in the components.
	validSlotNameRegex := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	if !validSlotNameRegex.MatchString(replicationSlotName) {
		return "", fmt.Errorf("generated replication slot name '%s' contains invalid characters (allowed: letters, numbers, underscore)", replicationSlotName)
	}

	//Note: PostgreSQL identifiers are also typically limited to 63 characters.
	if len(replicationSlotName) > 63 {
		return "", fmt.Errorf("generated replication slot name '%s' is longer than the 63-character limit", replicationSlotName)
	}

	return replicationSlotName, nil
}

func (r *simpleReplicationSlotCreateDeleter) DeleteFor(statefulSet *v1.StatefulSet) error {
	replicationSlotName, err := buildReplicationSlotName(r.kubegresContext.Kubegres.GetName(), r.clusterName, statefulSet.GetName(), r.kubegresContext.ClusterRole())
	objKey := ctrlclient.ObjectKeyFromObject(statefulSet)
	if err != nil {
		r.kubegresContext.Log.ErrorEvent("ReplicationSlotDelete", fmt.Errorf("replication slot name: %v: %w", objKey, err), "Failed to create replication slot name")
		return err
	}
	err = r.repo.DeleteSlot(r.kubegresContext.Ctx, replicationSlotName)
	if err != nil {
		if errors.Is(err, replicationSlotRepo.ErrNotFound) {
			r.kubegresContext.Log.InfoEvent("ReplicationSlotDelete", fmt.Sprintf("Replication slot '%s' for statefulSet '%s' does not exist, skipping deletion.", replicationSlotName, statefulSet.GetName()))
			return nil // If the slot does not exist, we can safely ignore this error.
		}
		r.kubegresContext.Log.ErrorEvent("ReplicationSlotDelete", fmt.Errorf("replication slot: %v for statefulSet: %v: %w", replicationSlotName, objKey, err), "Failed to delete replication slot")
		return err
	}
	return nil
}

func newSimpleReplicationSlotsCreateDeleter(kubegresContext kubegresCtx.KubegresContext, resourcesStates states.ResourcesStates, repository replicationSlotRepo.Repository, clusterName string) replicationSlotsCreateDeleter {
	return &simpleReplicationSlotCreateDeleter{
		repo:            repository,
		states:          resourcesStates,
		kubegresContext: kubegresContext,
		clusterName:     clusterName,
	}
}

func (r *ReplicaDbCountSpecEnforcer) CreateOperationConfigForReplicaDbDeploying() operation.BlockingOperationConfig {

	return operation.BlockingOperationConfig{
		OperationId:       operation.OperationIdReplicaDbCountSpecEnforcement,
		StepId:            operation.OperationStepIdReplicaDbDeploying,
		TimeOutInSeconds:  300,
		CompletionChecker: r.isReplicaDbReady,
	}
}

func (r *ReplicaDbCountSpecEnforcer) CreateOperationConfigForReplicaDbUndeploying() operation.BlockingOperationConfig {

	return operation.BlockingOperationConfig{
		OperationId:       operation.OperationIdReplicaDbCountSpecEnforcement,
		StepId:            operation.OperationStepIdReplicaDbUndeploying,
		TimeOutInSeconds:  60,
		CompletionChecker: r.isReplicaDbUndeployed,
	}
}

func (r *ReplicaDbCountSpecEnforcer) Enforce() error {

	if r.blockingOperation.IsActiveOperationIdDifferentOf(operation.OperationIdReplicaDbCountSpecEnforcement) {
		return nil
	}

	if r.hasLastAttemptTimedOut() {

		if r.isPreviouslyFailedAttemptOnReplicaDbFixed() {
			r.blockingOperation.RemoveActiveOperation()
			r.logKubegresFeaturesAreReEnabled()

		} else {
			r.logTimedOut()
			return nil
		}
	}

	if !r.isStandbyEnabled() && !r.isPrimaryDbReady() {
		return nil
	}

	isManualFailoverRequested := r.isManualFailoverRequested()
	if isManualFailoverRequested {
		r.resetInSpecManualFailover()
	}

	if r.isReplicaOperationInProgress() {
		return nil
	}

	if r.isStandbyEnabled() {
		for _, replica := range r.resourcesStates.StatefulSets.Replicas.All.GetAllSortedByInstanceIndex() {
			// if no label is set, we assume it is an update from an older version with no labels applied but no standby config changed
			if value, ok := replica.StatefulSet.Labels[template.LabelModeKey]; ok && value != template.LabelModelStandbyValue {
				r.kubegresContext.Log.InfoEvent("ReplicaDbCountSpecEnforcer",
					"Standby has been enabled and replica DB is not in standby mode. Undeploy it as primary node changed", "InstanceIndex", replica.InstanceIndex)
				return r.undeployReplicaStatefulSets(replica)
			}
		}
	}

	// Check if the number of deployed replicas == spec, if not then deploy one
	nbreDeployedReplicas, err := r.getNbreDeployedReplicas()
	if err != nil {
		return err
	}
	nbreNewReplicaToDeploy := r.getExpectedNbreReplicasToDeploy() - nbreDeployedReplicas

	if nbreNewReplicaToDeploy > 0 {

		if r.isAutomaticFailoverDisabled() &&
			!isManualFailoverRequested &&
			!r.doesSpecRequireTheDeploymentOfAdditionalReplicas() {

			r.logAutomaticFailoverIsDisabled()
			return nil
		}

		return r.deployReplicaStatefulSet()

	} else if nbreNewReplicaToDeploy < 0 {
		replicaToUndeploy := r.getReplicaToUndeploy()
		return r.undeployReplicaStatefulSets(replicaToUndeploy)

	} else if nbreNewReplicaToDeploy == 0 {
		for _, replicaStatefulSet := range r.getDeployedReplicas() {
			if !replicaStatefulSet.IsReady {
				return r.undeployReplicaStatefulSets(replicaStatefulSet)
			}
		}
	}

	// Then undeploy replicas that don't match running configuration
	replicationSlotDesired := r.replicationSlotsEnabled()
	for _, deployedStatefulSet := range r.resourcesStates.StatefulSets.Replicas.All.GetAllReverseSortedByInstanceIndex() {
		hasReplicationSlotsEnabled, err := r.hasReplicationSlotsEnabled(deployedStatefulSet)
		if err != nil {
			return err
		}
		if hasReplicationSlotsEnabled != replicationSlotDesired {
			return r.undeployReplicaStatefulSets(deployedStatefulSet)
		}
	}

	return nil
}

func (r *ReplicaDbCountSpecEnforcer) isStandbyEnabled() bool {
	return r.kubegresContext.Kubegres.Spec.Standby.Enabled
}

func (r *ReplicaDbCountSpecEnforcer) isReplicaOperationInProgress() bool {
	return r.blockingOperation.GetActiveOperation().OperationId == operation.OperationIdReplicaDbCountSpecEnforcement
}

func (r *ReplicaDbCountSpecEnforcer) getDeployedReplicas() []statefulset.StatefulSetWrapper {
	return r.resourcesStates.StatefulSets.Replicas.All.GetAllSortedByInstanceIndex()
}

func (r *ReplicaDbCountSpecEnforcer) getNbreDeployedReplicas() (int32, error) {
	deployedReplicas := r.resourcesStates.StatefulSets.Replicas.NbreDeployed
	for _, deployedReplica := range r.resourcesStates.StatefulSets.Replicas.All.GetAllReverseSortedByInstanceIndex() {
		hasReplicationSlotsEnabled, err := r.hasReplicationSlotsEnabled(deployedReplica)
		if err != nil {
			return 0, err
		}
		if hasReplicationSlotsEnabled != r.replicationSlotsEnabled() {
			deployedReplicas--
		}
	}
	return deployedReplicas, nil
}

func (r *ReplicaDbCountSpecEnforcer) getExpectedNbreReplicasToDeploy() int32 {
	expectedNbreToDeploy := r.resourcesStates.StatefulSets.SpecExpectedNbreToDeploy

	if r.isStandbyEnabled() {
		return expectedNbreToDeploy
	}

	if expectedNbreToDeploy <= 1 {
		return 0
	}
	return expectedNbreToDeploy - 1 // subtract the primary
}

func (r *ReplicaDbCountSpecEnforcer) hasLastAttemptTimedOut() bool {
	return r.blockingOperation.HasActiveOperationIdTimedOut(operation.OperationIdReplicaDbCountSpecEnforcement)
}

func (r *ReplicaDbCountSpecEnforcer) isPreviouslyFailedAttemptOnReplicaDbFixed() bool {
	activeOperation := r.blockingOperation.GetActiveOperation()
	replicaInstanceIndex := activeOperation.StatefulSetOperation.InstanceIndex
	replica, err := r.resourcesStates.StatefulSets.Replicas.All.GetByInstanceIndex(replicaInstanceIndex)

	return err != nil || replica.IsReady
}

func (r *ReplicaDbCountSpecEnforcer) logKubegresFeaturesAreReEnabled() {
	r.kubegresContext.Log.InfoEvent("KubegresReEnabled", "Replica DB which caused operation to time-out "+
		"is either set to ready again or it was removed. We can safely re-enable all features of Kubegres.")
}

func (r *ReplicaDbCountSpecEnforcer) logTimedOut() {

	activeOperation := r.blockingOperation.GetActiveOperation()
	operationTimeOutStr := strconv.FormatInt(r.CreateOperationConfigForReplicaDbDeploying().TimeOutInSeconds, 10)
	replicaStatefulSetName := activeOperation.StatefulSetOperation.Name

	if activeOperation.StepId == operation.OperationStepIdReplicaDbDeploying {

		err := errors.New("Replica DB StatefulSet deployment timed-out")
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetDeploymentTimedOutErr", err,
			"Last deployment attempt of a Replica DB StatefulSet has timed-out after "+operationTimeOutStr+" seconds. "+
				"The new Replica DB is still NOT ready. It must be fixed manually. "+
				"Until the ReplicaDB is ready, most of the features of Kubegres are disabled for safety reason. ",
			"Replica DB StatefulSet to fix", replicaStatefulSetName)

	} else {
		err := errors.New("Replica DB StatefulSet un-deployment timed-out")
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetDeploymentTimedOutErr", err,
			"Last un-deployment attempt of a Replica DB StatefulSet has timed-out after "+operationTimeOutStr+" seconds. "+
				"The new Replica DB is still NOT removed. It must be removed manually. "+
				"Until the ReplicaDB is removed, most of the features of Kubegres are disabled for safety reason. ",
			"Replica DB StatefulSet to remove", replicaStatefulSetName)
	}
}

func (r *ReplicaDbCountSpecEnforcer) isAutomaticFailoverDisabled() bool {
	return r.kubegresContext.Kubegres.Spec.Failover.IsDisabled
}

func (r *ReplicaDbCountSpecEnforcer) isManualFailoverRequested() bool {
	return r.kubegresContext.Kubegres.Spec.Failover.PromotePod != ""
}

func (r *ReplicaDbCountSpecEnforcer) doesSpecRequireTheDeploymentOfAdditionalReplicas() bool {
	return *r.kubegresContext.Kubegres.Spec.Replicas > r.kubegresContext.Kubegres.Status.EnforcedReplicas
}

func (r *ReplicaDbCountSpecEnforcer) resetInSpecManualFailover() error {
	r.kubegresContext.Log.Info("Resetting the field 'failover.promotePod' in spec.")
	r.kubegresContext.Kubegres.Spec.Failover.PromotePod = ""
	return r.kubegresContext.Client.Update(r.kubegresContext.Ctx, r.kubegresContext.Kubegres)
}

func (r *ReplicaDbCountSpecEnforcer) isPrimaryDbReady() bool {
	return r.resourcesStates.StatefulSets.Primary.IsReady
}

func (r *ReplicaDbCountSpecEnforcer) isReplicaDbReady(operation postgresV1.KubegresBlockingOperation) bool {

	statefulSetInstanceIndex := operation.StatefulSetOperation.InstanceIndex
	statefulSetWrapper, err := r.resourcesStates.StatefulSets.Replicas.All.GetByInstanceIndex(statefulSetInstanceIndex)
	if err != nil {
		r.kubegresContext.Log.InfoEvent("A replica StatefulSet's instanceIndex does not exist. As a result "+
			"we will return false inside a blocking operation completion checker 'isReplicaDbReady()'",
			"instanceIndex", statefulSetInstanceIndex)
		return false
	}

	return statefulSetWrapper.IsReady
}

func (r *ReplicaDbCountSpecEnforcer) isReplicaDbUndeployed(operation postgresV1.KubegresBlockingOperation) bool {
	statefulSetInstanceIndex := operation.StatefulSetOperation.InstanceIndex
	_, err := r.resourcesStates.StatefulSets.Replicas.All.GetByInstanceIndex(statefulSetInstanceIndex)
	return err != nil
}

func (r *ReplicaDbCountSpecEnforcer) deployReplicaStatefulSet() error {

	instanceIndex := r.kubegresContext.Status.GetLastCreatedInstanceIndex() + 1

	err := r.activateBlockingOperationForDeployment(instanceIndex)
	if err != nil {
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetOperationActivationErr", err, "Error while activating blocking operation for the deployment of a Replica StatefulSet.", "InstanceIndex", instanceIndex)
		return err
	}

	replicaStatefulSet, err := r.resourcesCreator.CreateReplicaStatefulSet(instanceIndex)
	if err != nil {
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetTemplateErr", err, "Error while creating a Replica StatefulSet object from template.", "InstanceIndex", instanceIndex)
		r.blockingOperation.RemoveActiveOperation()
		return err
	}

	updatedStatefulSet, err := r.replicationSlotsCreateDeleter.CreateFor(&replicaStatefulSet)
	if err != nil {
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetReplicationSlotCreationErr", err, "Error while creating replication slot for the Replica StatefulSet.", "Replica name", replicaStatefulSet.Name)
		r.blockingOperation.RemoveActiveOperation()
		return err
	}

	r.kubegresContext.Log.Info("Deploying Replica statefulSet '" + updatedStatefulSet.Name + "'")
	err = r.kubegresContext.Client.Create(r.kubegresContext.Ctx, updatedStatefulSet)
	if err != nil {
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetDeploymentErr", err, "Unable to deploy Replica StatefulSet.", "Replica name", updatedStatefulSet.Name)
		r.blockingOperation.RemoveActiveOperation()
		return err
	}

	r.kubegresContext.Status.SetEnforcedReplicas(r.kubegresContext.Kubegres.Status.EnforcedReplicas + 1)

	r.kubegresContext.Status.SetLastCreatedInstanceIndex(instanceIndex)
	r.kubegresContext.Log.InfoEvent("ReplicaStatefulSetDeployment", "Deployed Replica StatefulSet.", "Replica name", updatedStatefulSet.Name)
	return nil
}

func (r *ReplicaDbCountSpecEnforcer) activateBlockingOperationForDeployment(statefulSetInstanceIndex int32) error {
	return r.blockingOperation.ActivateOperationOnStatefulSet(operation.OperationIdReplicaDbCountSpecEnforcement,
		operation.OperationStepIdReplicaDbDeploying,
		statefulSetInstanceIndex)
}

func (r *ReplicaDbCountSpecEnforcer) activateBlockingOperationForUndeployment(statefulSetInstanceIndex int32) error {
	return r.blockingOperation.ActivateOperationOnStatefulSet(operation.OperationIdReplicaDbCountSpecEnforcement,
		operation.OperationStepIdReplicaDbUndeploying,
		statefulSetInstanceIndex)
}

func (r *ReplicaDbCountSpecEnforcer) undeployReplicaStatefulSets(replicaToUndeploy statefulset.StatefulSetWrapper) error {

	if replicaToUndeploy.StatefulSet.Name == "" {
		return nil
	}

	r.kubegresContext.Log.Info("We are going to undeploy a Replica statefulSet.", "InstanceIndex", replicaToUndeploy.InstanceIndex)

	err := r.activateBlockingOperationForUndeployment(replicaToUndeploy.InstanceIndex)
	if err != nil {
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetOperationActivationErr", err, "Error while activating blocking operation for the undeployment of a Replica StatefulSet.", "InstanceIndex", replicaToUndeploy.InstanceIndex)
		return err
	}

	err = r.deleteStatefulSet(replicaToUndeploy.StatefulSet)
	if err != nil {
		r.blockingOperation.RemoveActiveOperation()
		return err
	}

	// TODO(piotrkpc): a lot of magic numbers here. We should think how to make it better. Ideally we should not start a retry loop here
	//   and rather we should return an error from a reconciler and relay on the controller-runtime to retry the reconciliation but the problem here
	//   is that reconciliation is not really idempotent as it relies on the blocking operations in statuses.
	//   We are doing this because we cannot delete active replication slots.
	//   Another thing to consider is that should we use `SELECT pg_terminate_backend(<active_pid>);` to force termination of a backend process
	//   and then delete the active replication slots?
	//   Or should we fail here with a warning and relay on asynchronous cleanup loop to delete the replication slots?
	//   Issue discussed in: https://github.com/tetrateio/tetrate/issues/26542
	var attempt int
	err = retry.OnError(wait.Backoff{
		Duration: 3 * time.Second,
		Factor:   2,
		Steps:    10,
		Cap:      12 * time.Second,
	}, func(err error) bool {
		if err == nil {
			return false
		}
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetReplicationSlotDeletionErr", err, "Error while deleting replication slot for the Replica StatefulSet.", "Replica name", replicaToUndeploy.StatefulSet.Name, "Attempt", attempt)
		return true
	}, func() error {
		attempt++
		return r.replicationSlotsCreateDeleter.DeleteFor(&replicaToUndeploy.StatefulSet)
	})

	if err != nil {
		// exhausted all attempts to delete the replication slot
		r.blockingOperation.RemoveActiveOperation()
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetReplicationSlotDeletionErr", err, "Failed to delete replication slot for the Replica StatefulSet after all attempts.", "Replica name", replicaToUndeploy.StatefulSet.Name, "Attempt", attempt)
		return err
	}

	r.kubegresContext.Status.SetEnforcedReplicas(r.kubegresContext.Kubegres.Status.EnforcedReplicas - 1)

	return nil
}

func (r *ReplicaDbCountSpecEnforcer) getReplicaToUndeploy() statefulset.StatefulSetWrapper {

	replicasToUndeploy := r.getReplicasReverseSortedByInstanceIndex()

	if len(replicasToUndeploy) == 0 {
		return statefulset.StatefulSetWrapper{}
	}

	return replicasToUndeploy[0]
}

func (r *ReplicaDbCountSpecEnforcer) getReplicasReverseSortedByInstanceIndex() []statefulset.StatefulSetWrapper {
	return r.resourcesStates.StatefulSets.Replicas.All.GetAllReverseSortedByInstanceIndex()
}

func (r *ReplicaDbCountSpecEnforcer) deleteStatefulSet(statefulSetToDelete v1.StatefulSet) error {

	r.kubegresContext.Log.Info("Deleting Replica statefulSet", "name", statefulSetToDelete.Name)
	err := r.kubegresContext.Client.Delete(r.kubegresContext.Ctx, &statefulSetToDelete)

	if err != nil {
		r.kubegresContext.Log.ErrorEvent("ReplicaStatefulSetDeletionErr", err, "Unable to delete Replica StatefulSet.", "Replica name", statefulSetToDelete.Name)
		return err
	}

	r.kubegresContext.Log.InfoEvent("ReplicaStatefulSetDeletion", "Deleted Replica StatefulSet.", "Replica name", statefulSetToDelete.Name)
	return nil
}

func (r *ReplicaDbCountSpecEnforcer) logAutomaticFailoverIsDisabled() {
	r.kubegresContext.Log.InfoEvent("AutomaticFailoverIsDisabled",
		"We need to deploy additional Replica(s) because the number of Replicas deployed is less "+
			"than the number of required Replicas in the Spec. "+
			"However, a Replica failover cannot happen because the automatic failover feature is disabled in the YAML. "+
			"To re-enable automatic failover, either set the field 'failover.isDisabled' to false "+
			"or remove that field from the YAML.")
}

func (r *ReplicaDbCountSpecEnforcer) replicationSlotsEnabled() bool {
	return r.kubegresContext.Kubegres.Spec.ReplicationSlots.Enabled
}

func (r *ReplicaDbCountSpecEnforcer) hasReplicationSlotsEnabled(statefulSet statefulset.StatefulSetWrapper) (bool, error) {
	for _, container := range statefulSet.StatefulSet.Spec.Template.Spec.Containers {
		for _, envVar := range container.Env {
			if envVar.Name == kubegresCtx.EnvVarReplicationSlotName && envVar.Value != "" {
				// Also check if the replication slot actually exist in the database.
				// If it does not, treat it as if replication slots are not enabled, so we'll trigger a rollout of the
				// current replica.
				// This could happen after a failover when the primary changes and the new primary does not have
				// replication slots created for the already existing replicas.
				// Causing the rollout of the replicas will make all of them to be registered in the new primary.
				if !r.resourcesStates.StatefulSets.Primary.IsDeployed || !r.resourcesStates.StatefulSets.Primary.IsReady {
					err := errors.New("primary is not ready to check replication slots")
					r.kubegresContext.Log.ErrorEvent("ReplicationSlotCheck", err, "Cannot proceed without Ready primary", "Replica name", statefulSet.StatefulSet.Name)
					return false, err
				}
				_, err := r.replicationSlotsCreateDeleter.GetFor(&statefulSet.StatefulSet)
				if errors.Is(err, errNotFound) {
					return false, nil
				}
				if err != nil {
					r.kubegresContext.Log.ErrorEvent("ReplicationSlotCheck", err, "Error while checking replication slot for the Replica StatefulSet.", "Replica name", statefulSet.StatefulSet.Name)
					return false, err
				}
				return true, nil
			}
		}
	}
	return false, nil
}
