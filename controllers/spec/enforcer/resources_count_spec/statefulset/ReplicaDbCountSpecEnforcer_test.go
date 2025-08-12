package statefulset

import (
	"testing"

	"github.com/stretchr/testify/require"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/operation"
	"reactive-tech.io/kubegres/controllers/spec/template"
	"reactive-tech.io/kubegres/controllers/states"
)

func TestCreateReplicaDbCountSpecEnforcer(t *testing.T) {

	t.Skip("not sure if i want to test here or at the `resources.CreateResourcesContext` level.")

	tests := []struct {
		name              string
		kubegresContext   ctx.KubegresContext
		resourcesStates   states.ResourcesStates
		resourcesCreator  template.ResourcesCreatorFromTemplate
		blockingOperation *operation.BlockingOperation
		clusterName       string
		wantErr           error
		wantEnforceErr    error
	}{
		{},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enforcer, err := CreateReplicaDbCountSpecEnforcer(
				tt.kubegresContext,
				tt.resourcesStates,
				tt.resourcesCreator,
				tt.blockingOperation,
				tt.clusterName,
			)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, enforcer)
			err = enforcer.Enforce()
			if tt.wantEnforceErr != nil {
				require.ErrorIs(t, err, tt.wantEnforceErr)
				return
			}
		})
	}

}

func Test_createReplicationSlotName(t *testing.T) {
	tests := []struct {
		name            string
		kubegresName    string
		clusterName     string
		statefulSetName string
		role            ctx.ClusterRole
		want            string
		wantErr         bool
	}{
		{
			name:            "valid inputs create correct slot name",
			kubegresName:    "my_kubegres",
			clusterName:     "cluster1",
			statefulSetName: "replica_1",
			role:            "replica",
			want:            "my_kubegres_cluster1_replica_replica_1",
			wantErr:         false,
		},
		{
			name:            "replace dashes with underscores",
			kubegresName:    "my-kubegres-test",
			clusterName:     "cluster-1",
			statefulSetName: "replica-1-test",
			role:            "replica",
			want:            "my_kubegres_test_cluster_1_replica_replica_1_test",
			wantErr:         false,
		},
		{
			name:            "empty kubegres name returns error",
			kubegresName:    "",
			clusterName:     "cluster1",
			statefulSetName: "replica-1",
			role:            "replica",
			wantErr:         true,
		},
		{
			name:            "empty stateful set name returns error",
			kubegresName:    "my-kubegres",
			clusterName:     "cluster1",
			statefulSetName: "",
			role:            "replica",
			wantErr:         true,
		},
		{
			name:            "empty role returns error",
			kubegresName:    "my-kubegres",
			clusterName:     "cluster1",
			statefulSetName: "replica-1",
			role:            "",
			wantErr:         true,
		},
		{
			name:            "name exceeds 63 character limit",
			kubegresName:    "very-long-kubegres-name-that-exceeds-limits",
			clusterName:     "very-long-cluster-name-that-also-exceeds-limits",
			statefulSetName: "very-long-statefulset-name",
			role:            "replica",
			wantErr:         true,
		},
		{
			name:            "invalid characters in generated name",
			kubegresName:    "kubegres@test",
			clusterName:     "cluster1",
			statefulSetName: "replica-1",
			role:            "replica",
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createReplicationSlotName(tt.kubegresName, tt.clusterName, tt.statefulSetName, tt.role)

			if tt.wantErr {
				require.Error(t, err)
				require.Empty(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}
