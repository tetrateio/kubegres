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

package states

import (
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"reactive-tech.io/kubegres/controllers/ctx"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ConfigMapDataKeyPostgresConf             = "postgres.conf"
	ConfigMapDataKeyPrimaryInitScript        = "primary_init_script.sh"
	ConfigMapDataKeyPgHbaConf                = "pg_hba.conf"
	ConfigMapDataKeyBackUpScript             = "backup_database.sh"
	ConfigMapDataKeyCopyPrimaryDataToReplica = "copy_primary_data_to_replica.sh"
	ConfigMapDataKeyPrimaryCreateReplicaRole = "primary_create_replication_role.sh"
	ConfigMapDataKeyPromoteReplica           = "promote_replica_to_primary.sh"

	ConfigMapDataKeyTLSPostgresConf                   = "tls_postgres.conf"
	ConfigMapDataKeyTLSPgHbaConf                      = "tls_pg_hba.conf"
	ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript = "tls_copy_primary_data_to_replica.sh"
	ConfigMapDataKeyTLSBackupDatabaseScript           = "tls_backup_database.sh"
)

var TLSConfigKeyReplacements = []TLSConfigKeyReplacement{
	{
		OriginalKey:      ConfigMapDataKeyPostgresConf,
		ReplacementKey:   ConfigMapDataKeyTLSPostgresConf,
		AppliesInstance:  PrimaryAndReplicaInstance,
		AppliesContainer: OnlyMainContainer,
	},
	{
		OriginalKey:      ConfigMapDataKeyPgHbaConf,
		ReplacementKey:   ConfigMapDataKeyTLSPgHbaConf,
		AppliesInstance:  PrimaryAndReplicaInstance,
		AppliesContainer: OnlyMainContainer,
	},
	{
		OriginalKey:      ConfigMapDataKeyCopyPrimaryDataToReplica,
		ReplacementKey:   ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript,
		AppliesInstance:  ReplicaInstance,
		AppliesContainer: OnlyInitContainer,
	},
	{
		OriginalKey:      ConfigMapDataKeyBackUpScript,
		ReplacementKey:   ConfigMapDataKeyTLSBackupDatabaseScript,
		AppliesInstance:  BackupJob,
		AppliesContainer: OnlyMainContainer,
	},
}

type (
	TLSConfigKeyReplacement struct {
		OriginalKey      string
		ReplacementKey   string
		AppliesInstance  appliesInstance
		AppliesContainer appliesContainer
	}
	appliesInstance  int
	appliesContainer int
)

const (
	PrimaryInstance appliesInstance = iota
	ReplicaInstance
	PrimaryAndReplicaInstance
	BackupJob
)
const (
	OnlyMainContainer appliesContainer = iota
	OnlyInitContainer
	AllContainers
)

func (t TLSConfigKeyReplacement) DoesApplyContainer() bool {
	return t.AppliesContainer == AllContainers || t.AppliesContainer == OnlyMainContainer
}

func (t TLSConfigKeyReplacement) DoesApplyInitContainer() bool {
	return t.AppliesContainer == AllContainers || t.AppliesContainer == OnlyInitContainer
}

func (t TLSConfigKeyReplacement) DoesApplyStatefulSet(statefulSet *apps.StatefulSet) bool {
	if statefulSet.Labels["replicationRole"] == ctx.PrimaryRoleName {
		return t.AppliesInstance == PrimaryInstance || t.AppliesInstance == PrimaryAndReplicaInstance
	}
	return t.AppliesInstance == ReplicaInstance || t.AppliesInstance == PrimaryAndReplicaInstance
}

type ConfigStates struct {
	IsBaseConfigDeployed   bool
	BaseConfigName         string
	IsCustomConfigDeployed bool
	CustomConfigName       string
	ConfigLocations        ConfigLocations
	IsTLSConfigDeployed    bool

	kubegresContext ctx.KubegresContext
}

// Stores as string the volume-name for each config-type which can be either 'base-config' or 'custom-config'
type ConfigLocations struct {
	PostgresConf             string
	PrimaryInitScript        string
	BackUpScript             string
	PgHbaConf                string
	CopyPrimaryDataToReplica string
	PrimaryCreateReplicaRole string
	PromoteReplica           string
	// TLS
	TLSPostgresConfForPrimary   string
	TLSPostgresConfForReplica   string
	TLSPgHbaConf                string
	TLSCopyPrimaryDataToReplica string
}

func loadConfigStates(kubegresContext ctx.KubegresContext) (ConfigStates, error) {

	configMapStates := ConfigStates{kubegresContext: kubegresContext}
	configMapStates.BaseConfigName = ctx.BaseConfigMapName
	configMapStates.CustomConfigName = kubegresContext.Kubegres.Spec.CustomConfig

	err := configMapStates.loadStates()

	return configMapStates, err
}

func (r *ConfigStates) loadStates() (err error) {

	r.ConfigLocations.PostgresConf = ctx.BaseConfigMapVolumeName
	r.ConfigLocations.PrimaryInitScript = ctx.BaseConfigMapVolumeName
	r.ConfigLocations.BackUpScript = ctx.BaseConfigMapVolumeName
	r.ConfigLocations.PgHbaConf = ctx.BaseConfigMapVolumeName
	r.ConfigLocations.CopyPrimaryDataToReplica = ctx.BaseConfigMapVolumeName
	r.ConfigLocations.PrimaryCreateReplicaRole = ctx.BaseConfigMapVolumeName
	r.ConfigLocations.PromoteReplica = ctx.BaseConfigMapVolumeName

	baseConfigMap, err := r.getBaseDeployedConfigMap()
	if err != nil {
		return err
	}

	if r.isBaseConfigMap(baseConfigMap) {
		r.IsBaseConfigDeployed = true
	}

	if baseConfigMap.Data[ConfigMapDataKeyTLSPostgresConf] != "" {
		r.ConfigLocations.TLSPostgresConfForReplica = ctx.BaseConfigMapVolumeName
	}

	if baseConfigMap.Data[ConfigMapDataKeyTLSPgHbaConf] != "" {
		r.ConfigLocations.TLSPgHbaConf = ctx.BaseConfigMapVolumeName
	}

	if baseConfigMap.Data[ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript] != "" {
		r.ConfigLocations.TLSCopyPrimaryDataToReplica = ctx.BaseConfigMapVolumeName
	}

	if r.isBaseConfigAlsoCustomConfig() {
		r.IsTLSConfigDeployed = r.isTLSConfigDeployed(baseConfigMap)
		return nil
	}

	customConfigMap, err := r.getDeployedCustomCustomConfigMap()
	if err != nil {
		return err
	}

	if r.isCustomConfigDeployed(customConfigMap) {

		r.IsCustomConfigDeployed = true

		if customConfigMap.Data[ConfigMapDataKeyPostgresConf] != "" {
			r.ConfigLocations.PostgresConf = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyPrimaryInitScript] != "" {
			r.ConfigLocations.PrimaryInitScript = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyBackUpScript] != "" {
			r.ConfigLocations.BackUpScript = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyPgHbaConf] != "" {
			r.ConfigLocations.PgHbaConf = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyCopyPrimaryDataToReplica] != "" {
			r.ConfigLocations.CopyPrimaryDataToReplica = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyPrimaryCreateReplicaRole] != "" {
			r.ConfigLocations.PrimaryCreateReplicaRole = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyPromoteReplica] != "" {
			r.ConfigLocations.PromoteReplica = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyTLSPostgresConf] != "" {
			r.ConfigLocations.TLSPostgresConfForPrimary = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyTLSPgHbaConf] != "" {
			r.ConfigLocations.TLSPgHbaConf = ctx.CustomConfigMapVolumeName
		}

		if customConfigMap.Data[ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript] != "" {
			r.ConfigLocations.TLSCopyPrimaryDataToReplica = ctx.CustomConfigMapVolumeName
		}

		r.IsTLSConfigDeployed = r.isTLSConfigDeployed(baseConfigMap, customConfigMap)
	}

	r.IsTLSConfigDeployed = r.isTLSConfigDeployed(baseConfigMap)

	return nil
}

func (r *ConfigStates) isTLSConfigDeployed(cms ...*core.ConfigMap) bool {
	var requiredKeys = make([]string, 0, len(TLSConfigKeyReplacements))
	for _, replacement := range TLSConfigKeyReplacements {
		requiredKeys = append(requiredKeys, replacement.ReplacementKey)
	}

	for _, required := range requiredKeys {
		keyFound := false
		for _, cm := range cms {
			if cm.Data[required] != "" {
				keyFound = true
				break
			}
		}
		// If the key is not found in any of the provided ConfigMaps, early break with false
		if !keyFound {
			return false
		}
	}
	return true
}

func (r *ConfigStates) isBaseConfigAlsoCustomConfig() bool {
	return r.CustomConfigName == r.BaseConfigName
}

func (r *ConfigStates) isBaseConfigMap(configMap *core.ConfigMap) bool {
	return configMap.Name == r.BaseConfigName
}

func (r *ConfigStates) isCustomConfigDeployed(configMap *core.ConfigMap) bool {
	return configMap.Name != "" && configMap.Name != r.BaseConfigName
}

func (r *ConfigStates) getBaseDeployedConfigMap() (*core.ConfigMap, error) {

	namespace := r.kubegresContext.Kubegres.Namespace
	resourceName := r.BaseConfigName
	configMapKey := client.ObjectKey{Namespace: namespace, Name: resourceName}

	return r.getDeployedConfigMap(configMapKey, resourceName, "Base")
}

func (r *ConfigStates) getDeployedCustomCustomConfigMap() (*core.ConfigMap, error) {

	namespace := r.kubegresContext.Kubegres.Namespace
	resourceName := r.CustomConfigName
	configMapKey := client.ObjectKey{Namespace: namespace, Name: resourceName}

	return r.getDeployedConfigMap(configMapKey, resourceName, "Init")
}

func (r *ConfigStates) getDeployedConfigMap(configMapKey client.ObjectKey, resourceName string, logLabel string) (*core.ConfigMap, error) {

	configMap := &core.ConfigMap{}
	err := r.kubegresContext.Client.Get(r.kubegresContext.Ctx, configMapKey, configMap)

	if err != nil {
		if apierrors.IsNotFound(err) {
			err = nil
		} else {
			r.kubegresContext.Log.ErrorEvent("ConfigMapLoadingErr", err, "Unable to load any deployed "+logLabel+" Config.", "Config name", resourceName)
		}
	}

	return configMap, err
}
