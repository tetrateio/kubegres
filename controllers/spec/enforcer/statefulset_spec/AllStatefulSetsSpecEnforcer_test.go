package statefulset_spec

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apps "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/ctx/log"
	"reactive-tech.io/kubegres/controllers/ctx/status"
	"reactive-tech.io/kubegres/controllers/operation"
	"reactive-tech.io/kubegres/controllers/states"
	"reactive-tech.io/kubegres/controllers/states/statefulset"
)

const (
	primaryIndex = int32(1)
	replicaIndex = int32(2)
)

// newEnforcer builds the enforcer with a Primary at index 1, a ready Replica at index 2 and the
// given blocking operation loaded, with no Kubernetes API server.
func newEnforcer(t *testing.T, isPrimaryReady bool, standby bool,
	activeOperation v1.KubegresBlockingOperation) (*AllStatefulSetsSpecEnforcer, *operation.BlockingOperation, *v1.Kubegres) {
	t.Helper()

	kubegres := &v1.Kubegres{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "default"},
		Spec:       v1.KubegresSpec{Standby: v1.Standby{Enabled: standby}},
		Status:     v1.KubegresStatus{BlockingOperation: activeOperation},
	}

	logWrapper := log.LogWrapper{
		Kubegres: kubegres,
		Logger:   logr.Discard(),
		Recorder: record.NewFakeRecorder(256),
	}
	kubegresContext := ctx.KubegresContext{
		Kubegres: kubegres,
		Status:   &status.KubegresStatusWrapper{Kubegres: kubegres, Log: logWrapper},
		Ctx:      t.Context(),
		Log:      logWrapper,
	}

	var resourcesStates states.ResourcesStates
	primary := newStatefulSetWrapper("postgres-1", primaryIndex, isPrimaryReady)
	replica := newStatefulSetWrapper("postgres-2", replicaIndex, true)
	resourcesStates.StatefulSets.Primary = primary
	resourcesStates.StatefulSets.Replicas.All.Add(replica)
	resourcesStates.StatefulSets.All.Add(primary)
	resourcesStates.StatefulSets.All.Add(replica)

	blockingOperation := operation.CreateBlockingOperation(kubegresContext)
	enforcer := CreateAllStatefulSetsSpecEnforcer(kubegresContext, resourcesStates, blockingOperation,
		CreateStatefulSetsSpecsEnforcer(kubegresContext))
	blockingOperation.AddConfig(enforcer.CreateOperationConfigForStatefulSetSpecUpdating())
	blockingOperation.AddConfig(enforcer.CreateOperationConfigForStatefulSetSpecPodUpdating())
	blockingOperation.AddConfig(enforcer.CreateOperationConfigForStatefulSetWaitingOnStuckPod())
	blockingOperation.LoadActiveOperation()

	return &enforcer, blockingOperation, kubegres
}

func newStatefulSetWrapper(name string, instanceIndex int32, isReady bool) statefulset.StatefulSetWrapper {
	return statefulset.StatefulSetWrapper{
		IsDeployed:    true,
		IsReady:       isReady,
		InstanceIndex: instanceIndex,
		StatefulSet:   apps.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}},
		Pod: statefulset.PodWrapper{
			IsDeployed:    true,
			IsReady:       isReady,
			InstanceIndex: instanceIndex,
		},
	}
}

func specOperation(stepId string, instanceIndex int32) v1.KubegresBlockingOperation {
	return v1.KubegresBlockingOperation{
		OperationId:          operation.OperationIdStatefulSetSpecEnforcing,
		StepId:               stepId,
		TimeOutEpocInSeconds: time.Now().Add(5 * time.Minute).Unix(),
		StatefulSetOperation: v1.KubegresStatefulSetOperation{
			InstanceIndex: instanceIndex,
			Name:          fmt.Sprintf("postgres-%d", instanceIndex),
		},
	}
}

func inTransition(op v1.KubegresBlockingOperation) v1.KubegresBlockingOperation {
	op.StepId = operation.TransitionOperationStepId
	op.TimeOutEpocInSeconds = 0
	return op
}

func TestSpecUpdateOfAReplicaIsRemovedWhenThePrimaryIsNotReady(t *testing.T) {
	// A Replica's spec update finished and waits in the transition step. It has no timeout, so
	// unless it is removed, the failover never starts and the cluster has no Primary.
	enforcer, blockingOperation, kubegres := newEnforcer(t, false, false,
		inTransition(specOperation(operation.OperationStepIdStatefulSetSpecUpdating, replicaIndex)))

	require.NoError(t, enforcer.EnforceSpec())

	require.Empty(t, kubegres.Status.BlockingOperation.OperationId)
	require.False(t, blockingOperation.IsActiveOperationIdDifferentOf(operation.OperationIdPrimaryDbCountSpecEnforcement),
		"the failover must be able to start")
	require.Equal(t, releasedOperationRequeueInterval, enforcer.RequeueAfter())
	require.Zero(t, enforcer.RequeueAfter(), "the requeue request is cleared once read")
}

func TestPodSpecUpdateOfAReplicaIsRemovedWhenThePrimaryIsNotReady(t *testing.T) {
	enforcer, _, kubegres := newEnforcer(t, false, false,
		specOperation(operation.OperationStepIdStatefulSetPodSpecUpdating, replicaIndex))

	require.NoError(t, enforcer.EnforceSpec())

	require.Empty(t, kubegres.Status.BlockingOperation.OperationId)
	require.Equal(t, releasedOperationRequeueInterval, enforcer.RequeueAfter())
}

func TestSpecUpdateOfThePrimaryIsKeptWhileThePrimaryRestarts(t *testing.T) {
	// The Primary is not ready because its own Pod restarts with the new spec. That is expected
	// and must not start a failover.
	for _, op := range []v1.KubegresBlockingOperation{
		specOperation(operation.OperationStepIdStatefulSetPodSpecUpdating, primaryIndex),
		inTransition(specOperation(operation.OperationStepIdStatefulSetSpecUpdating, primaryIndex)),
	} {
		enforcer, blockingOperation, kubegres := newEnforcer(t, false, false, op)

		require.NoError(t, enforcer.EnforceSpec())

		require.Equal(t, op.StepId, kubegres.Status.BlockingOperation.StepId)
		require.True(t, blockingOperation.IsActiveOperationIdDifferentOf(operation.OperationIdPrimaryDbCountSpecEnforcement))
		require.Zero(t, enforcer.RequeueAfter())
	}
}

func TestOtherOperationsAreKeptWhenThePrimaryIsNotReady(t *testing.T) {
	failingOver := v1.KubegresBlockingOperation{
		OperationId:          operation.OperationIdPrimaryDbCountSpecEnforcement,
		StepId:               operation.OperationStepIdPrimaryDbFailingOver,
		TimeOutEpocInSeconds: time.Now().Add(5 * time.Minute).Unix(),
	}
	enforcer, _, kubegres := newEnforcer(t, false, false, failingOver)

	require.NoError(t, enforcer.EnforceSpec())

	require.Equal(t, failingOver.OperationId, kubegres.Status.BlockingOperation.OperationId)
	require.Zero(t, enforcer.RequeueAfter())
}

func TestNothingIsRemovedWithoutAnActiveOperation(t *testing.T) {
	enforcer, _, kubegres := newEnforcer(t, false, false, v1.KubegresBlockingOperation{})

	require.NoError(t, enforcer.EnforceSpec())

	require.Empty(t, kubegres.Status.BlockingOperation.OperationId)
	require.Zero(t, enforcer.RequeueAfter())
}

func TestStandbyClusterKeepsTheSpecUpdate(t *testing.T) {
	// A standby cluster updates its StatefulSets without a ready Primary, so the operation must
	// follow the normal path.
	op := inTransition(specOperation(operation.OperationStepIdStatefulSetSpecUpdating, replicaIndex))
	enforcer, _, kubegres := newEnforcer(t, false, true, op)

	require.NoError(t, enforcer.EnforceSpec())

	require.Zero(t, enforcer.RequeueAfter())
	require.NotEqual(t, v1.KubegresBlockingOperation{}, kubegres.Status.BlockingOperation)
}
