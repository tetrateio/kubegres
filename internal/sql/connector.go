package sql

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	corev1 "k8s.io/api/core/v1"
	kubegresCtx "reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/states"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TODO(piotrkpc): @Sergi, this is likely to be changed to something more sophisticated that would enable ssl enabled connections
func newConnectorConfig(
	ctx context.Context,
	primarySvcProvider func() (string, int32, error),
	env []corev1.EnvVar,
	client ctrlclient.Client,
	namespace string,
) (*pgx.ConnConfig, error) {

	primarySvcName, port, err := primarySvcProvider()
	if err != nil {
		return nil, fmt.Errorf("get primary service name and port: %w", err)
	}

	user := "replication"

	idx := slices.IndexFunc(env, func(envVar corev1.EnvVar) bool {
		return envVar.Name == kubegresCtx.EnvVarNameOfPostgresReplicationUserPsw
	})
	if idx == -1 {
		return nil, fmt.Errorf("replication user password environment variable '%s' not found", kubegresCtx.EnvVarNameOfPostgresReplicationUserPsw)
	}
	envWithReplicationCreds := env[idx]
	password := envWithReplicationCreds.Value

	if envWithReplicationCreds.ValueFrom != nil {
		if envWithReplicationCreds.ValueFrom.SecretKeyRef != nil {
			var secret corev1.Secret
			objectKey := ctrlclient.ObjectKey{
				Name:      envWithReplicationCreds.ValueFrom.SecretKeyRef.Name,
				Namespace: namespace,
			}
			err := client.Get(ctx, objectKey, &secret)
			if err != nil {
				return nil, fmt.Errorf("get replication user password secret '%v': %w", objectKey, err)
			}
			password = string(secret.Data[envWithReplicationCreds.ValueFrom.SecretKeyRef.Key])
		}
		if envWithReplicationCreds.ValueFrom.ConfigMapKeyRef != nil {
			var configMap corev1.ConfigMap
			objectKey := ctrlclient.ObjectKey{
				Name:      envWithReplicationCreds.ValueFrom.ConfigMapKeyRef.Name,
				Namespace: namespace,
			}
			err := client.Get(ctx, objectKey, &configMap)
			if err != nil {
				return nil, fmt.Errorf("get replication user password config map '%v': %w", objectKey, err)
			}
			password = configMap.Data[envWithReplicationCreds.ValueFrom.ConfigMapKeyRef.Key]
		}
		if envWithReplicationCreds.ValueFrom.FieldRef != nil || envWithReplicationCreds.ValueFrom.ResourceFieldRef != nil {
			return nil, fmt.Errorf("replication user password environment variable '%s' cannot be set from field or resource reference", kubegresCtx.EnvVarNameOfPostgresReplicationUserPsw)
		}
	}

	return pgx.ParseConfig("postgresql://" + user + ":" + password + "@" + primarySvcName + ":" + strconv.Itoa(int(port)) + "/postgres?sslmode=disable")
}

func NewDBFrom(kubegresContext kubegresCtx.KubegresContext, resourceStates states.ResourcesStates, primaryDbSvcRetriever func() (string, int32, error)) (*sql.DB, error) {
	if primaryDbSvcRetriever == nil {
		primaryDbSvcRetriever = func() (string, int32, error) {
			return resourceStates.PrimaryConnectionDetails()
		}
	}

	connConfig, err := newConnectorConfig(kubegresContext.Ctx, primaryDbSvcRetriever, kubegresContext.Kubegres.Spec.Env, kubegresContext.Client, kubegresContext.Kubegres.Namespace)
	if err != nil {
		return nil, fmt.Errorf("create db connector config: %w", err)
	}
	return sql.OpenDB(stdlib.GetConnector(*connConfig)), nil
}
