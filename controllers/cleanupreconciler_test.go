package controllers

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/tools/record"
	"reactive-tech.io/kubegres/test/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestCleanupReplicationSlotsReconciler_Reconcile(t *testing.T) {

	tests := []struct {
		name                  string
		clientSetupFn         func() client.Client
		req                   reconcile.Request
		assertEventRecorder   func(t *testing.T, recorder *record.FakeRecorder)
		assertClient          func(t *testing.T, c client.Client)
		assertMockLogger      func(t *testing.T, sink *util.MockLogSink)
		assertReconcileOutput func(t *testing.T, result reconcile.Result, err error)
	}{
		{
			name: "",
			clientSetupFn: func() client.Client {
				return fake.NewClientBuilder().
					WithObjects(
						primaryStatefulSet()).Build()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocklogger := util.CreateMockLogger()
			fakeRecorder := record.NewFakeRecorder(10)

			if tt.assertEventRecorder == nil || tt.assertClient == nil || tt.assertMockLogger == nil || tt.assertReconcileOutput == nil {
				require.FailNow(t, "Test case is not fully implemented, please provide all assertions")
			}

			c := tt.clientSetupFn()
			reconciler := NewCleanupReplicationSlotsReconciler(
				c,
				mocklogger,
				fakeRecorder,
				nil,
			)
			result, err := reconciler.Reconcile(t.Context(), tt.req)
			tt.assertReconcileOutput(t, result, err)

			tt.assertMockLogger(t, mocklogger.GetSink().(*util.MockLogSink))
			tt.assertEventRecorder(t, fakeRecorder)
			tt.assertClient(t, c)
		})
	}
}

//go:embed testdata/statefulset-primary.yaml
var primaryStatefulSetYaml string

//go:embed testdata/statefulset-replica.yaml
var replicaStatefulSetYaml string

func primaryStatefulSet() *v1.StatefulSet {
	return nil
}
