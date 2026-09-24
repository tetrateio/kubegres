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

package ctx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx/log"
	"reactive-tech.io/kubegres/controllers/ctx/status"
	"reactive-tech.io/kubegres/internal/sql"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type KubegresContext struct {
	Kubegres        *v1.Kubegres
	Status          *status.KubegresStatusWrapper
	Ctx             context.Context
	Log             log.LogWrapper
	Client          client.Client
	ConnectionStore *sql.ConnectionStore
}

const (
	PrimaryRoleName                        = "primary"
	LabelReplicationRole                   = "replicationRole"
	KindKubegres                           = "Kubegres"
	DeploymentOwnerKey                     = ".metadata.controller"
	DatabaseVolumeName                     = "postgres-db"
	BaseConfigMapVolumeName                = "base-config"
	CustomConfigMapVolumeName              = "custom-config"
	BaseConfigMapName                      = "base-kubegres-config"
	CronJobNamePrefix                      = "backup-"
	DefaultContainerPortNumber             = 5432
	DefaultPodServiceAccountName           = "default"
	DefaultDatabaseVolumeMount             = "/var/lib/postgresql/data"
	DefaultDatabaseFolder                  = "pgdata"
	EnvVarNamePgData                       = "PGDATA"
	EnvVarNameOfPostgresSuperUserPsw       = "POSTGRES_PASSWORD"
	EnvVarNameOfPostgresReplicationUserPsw = "POSTGRES_REPLICATION_PASSWORD"
	EnvVarReplicationSlotName              = "POSTGRES_REPLICATION_SLOT"
)

var (
	DefaultReplicationSlotsInactiveSlotGracePeriod = metav1.Duration{Duration: 10 * time.Minute}
	DefaultReplicationSlotsHealthCheckInterval     = metav1.Duration{Duration: 30 * time.Second}

	// DefaultFailOverHealthCheckTimeout is how long a Replica has to report its replication
	// state before it counts as unreachable.
	DefaultFailOverHealthCheckTimeout = metav1.Duration{Duration: 5 * time.Second}

	// DefaultFailOverMaxReplicationLag is how far behind the promoted Replica may be. 16Mi is
	// one WAL segment: a Replica further behind than that is out of step with the Primary.
	DefaultFailOverMaxReplicationLag = resource.MustParse("16Mi")
)

// DefaultFailOverFallbackToReadiness prefers availability over durability: if the operator
// cannot reach any Replica, the cluster recovers on readiness alone rather than waiting for a
// human.
const DefaultFailOverFallbackToReadiness = true

func (r *KubegresContext) GetServiceResourceName(isPrimary bool) string {
	if isPrimary {
		return r.Kubegres.Name
	}
	return r.Kubegres.Name + "-replica"
}

func (r *KubegresContext) GetStatefulSetResourceName(instanceIndex int32) string {
	return r.Kubegres.Name + "-" + strconv.Itoa(int(instanceIndex))
}

func (r *KubegresContext) IsReservedVolumeName(volumeName string) bool {
	return volumeName == DatabaseVolumeName ||
		volumeName == BaseConfigMapVolumeName ||
		volumeName == CustomConfigMapVolumeName ||
		strings.Contains(volumeName, "kube-api")
}

func (r *KubegresContext) GetSQLConnection() (sql.ConnectionSupplier, bool) {
	if r.ConnectionStore == nil {
		r.Log.Error(nil, "Cannot get an SQL connection from the store")
		return nil, false
	}

	return r.ConnectionStore.Get(sql.ConnectionID{Name: r.Kubegres.Name, Namespace: r.Kubegres.Namespace})
}

type ClusterRole string

const (
	ActiveRoleName  ClusterRole = "active"
	StandbyRoleName ClusterRole = "standby"
)

func (r *KubegresContext) ClusterRole() ClusterRole {
	if r.Kubegres.Spec.Standby.Enabled {
		return StandbyRoleName
	}
	return ActiveRoleName
}

// GetReplicaSQLConnection returns a connection to one replica, creating it on first use and
// keeping it in the shared ConnectionStore.
//
// A Service cannot reach one specific replica: both Kubegres Services are headless and the
// replica Service covers every replica. So the Pod IP goes straight into the DSN as the host.
// Everything else - credentials, database, TLS material - is copied from the primary connection,
// which the DBConnectionReconciler already keeps up to date.
//
// The IP goes in "host" rather than "hostaddr": pgx does not recognise "hostaddr", and silently
// treats it as a server runtime parameter, leaving the host empty and falling back to a Unix
// socket.
//
// The connection is a DynamicDSNConnection, so a replica that moves to a new Pod IP reconnects
// on next use instead of going stale.
func (r *KubegresContext) GetReplicaSQLConnection(instanceIndex int32, hostAddr string, port int32) (sql.ConnectionSupplier, error) {
	if r.ConnectionStore == nil {
		return nil, errors.New("the connection store is not available")
	}
	if hostAddr == "" {
		return nil, fmt.Errorf("replica with instance index %d has no Pod IP yet", instanceIndex)
	}

	primaryConn, found := r.GetSQLConnection()
	if !found {
		return nil, errors.New("the primary connection is not available to derive replica connection settings from")
	}

	template, ok := primaryConn.(sql.DSNDataSupplier)
	if !ok {
		return nil, errors.New("the primary connection does not expose its DSN settings")
	}

	replicaDsn := template.Data().Snapshot()
	replicaDsn.Host = hostAddr
	replicaDsn.HostAddr = ""
	replicaDsn.Port = strconv.Itoa(int(port))

	connID := sql.ReplicaConnectionID(r.Kubegres.Namespace, r.Kubegres.Name, instanceIndex)

	if existing, found := r.ConnectionStore.Get(connID); found {
		if dsnSupplier, ok := existing.(sql.DSNDataSupplier); ok {
			// Re-point the existing connection instead of replacing it, so a Pod IP change does
			// not leak the old *sql.DB. DynamicDSNConnection reconnects when the DSN changes.
			dsnSupplier.Data().Apply(func(d *sql.DSNData) {
				d.HostAddr = replicaDsn.HostAddr
				d.Host = replicaDsn.Host
				d.Port = replicaDsn.Port
				d.Username = replicaDsn.Username
				d.Password = replicaDsn.Password
				d.Database = replicaDsn.Database
				d.SSLMode = replicaDsn.SSLMode
				d.RootCertPath = replicaDsn.RootCertPath
				d.ClientCertPath = replicaDsn.ClientCertPath
				d.ClientKeyPath = replicaDsn.ClientKeyPath
			})
			return existing, nil
		}

		// Unexpected connection type under this key: drop it and build a fresh one.
		if err := r.ConnectionStore.Delete(connID); err != nil {
			r.Log.Error(err, "Failed to close a stale replica connection", "connectionID", connID.String())
		}
	}

	conn, err := sql.NewDynamicDSNConnection(replicaDsn)
	if err != nil {
		return nil, fmt.Errorf("open connection to replica %d: %w", instanceIndex, err)
	}

	r.ConnectionStore.Set(connID, conn)
	r.Log.Info("New replica DB connection created.", "connectionID", connID.String(), "dsn", replicaDsn.String())

	return conn, nil
}

// PruneReplicaSQLConnections closes and forgets connections to replicas that are gone.
//
// Kubegres gives every new replica a higher index, so without this a long-lived cluster would
// collect one dead *sql.DB per replica it ever had.
func (r *KubegresContext) PruneReplicaSQLConnections(liveInstanceIndexes []int32) {
	if r.ConnectionStore == nil {
		return
	}

	live := make(map[string]struct{}, len(liveInstanceIndexes))
	for _, instanceIndex := range liveInstanceIndexes {
		live[strconv.Itoa(int(instanceIndex))] = struct{}{}
	}

	for _, connID := range r.ConnectionStore.ReplicaKeys(r.Kubegres.Namespace, r.Kubegres.Name) {
		if _, stillDeployed := live[connID.Instance]; stillDeployed {
			continue
		}

		if err := r.ConnectionStore.Delete(connID); err != nil {
			r.Log.Error(err, "Failed to close the connection of an undeployed replica", "connectionID", connID.String())
		} else {
			r.Log.Info("Closed the connection of an undeployed replica.", "connectionID", connID.String())
		}
	}
}
