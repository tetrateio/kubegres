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

package template

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	v1 "reactive-tech.io/kubegres/api/v1"
	log2 "reactive-tech.io/kubegres/controllers/ctx/log"
	"reactive-tech.io/kubegres/controllers/spec/template/yaml"
)

type ResourceTemplateLoader struct {
	log log2.LogWrapper
}

func (r *ResourceTemplateLoader) LoadBaseConfigMap() (configMap core.ConfigMap, err error) {

	obj, err := r.decodeYaml(yaml.BaseConfigMapTemplate)

	if err != nil {
		r.log.Error(err, "Unable to load Kubegres CustomConfig. Given error:")
		return core.ConfigMap{}, err
	}

	return *obj.(*core.ConfigMap), nil
}

func (r *ResourceTemplateLoader) LoadPrimaryService() (serviceTemplate core.Service, err error) {
	serviceTemplate, err = r.loadService(yaml.PrimaryServiceTemplate)
	return serviceTemplate, err
}

func (r *ResourceTemplateLoader) LoadReplicaService() (serviceTemplate core.Service, err error) {
	return r.loadService(yaml.ReplicaServiceTemplate)
}

func (r *ResourceTemplateLoader) LoadPrimaryStatefulSet() (statefulSetTemplate apps.StatefulSet, err error) {
	return r.loadStatefulSet(yaml.PrimaryStatefulSetTemplate)
}

func (r *ResourceTemplateLoader) LoadReplicaStatefulSet() (statefulSetTemplate apps.StatefulSet, err error) {
	return r.loadStatefulSet(yaml.ReplicaStatefulSetTemplate)
}

func (r *ResourceTemplateLoader) LoadBackUpCronJob() (cronJob batch.CronJob, err error) {
	obj, err := r.decodeYaml(yaml.BackUpCronJobTemplate)

	if err != nil {
		r.log.Error(err, "Unable to load Kubegres BackUp CronJob. Given error:")
		return batch.CronJob{}, err
	}

	return *obj.(*batch.CronJob), nil
}

func (r *ResourceTemplateLoader) loadService(yamlContents string) (serviceTemplate core.Service, err error) {

	obj, err := r.decodeYaml(yamlContents)

	if err != nil {
		r.log.Error(err, "Unable to decode Kubegres Service. Given error: ")
		return core.Service{}, err
	}

	return *obj.(*core.Service), nil
}

func (r *ResourceTemplateLoader) loadStatefulSet(yamlContents string) (statefulSetTemplate apps.StatefulSet, err error) {

	obj, err := r.decodeYaml(yamlContents)

	if err != nil {
		r.log.Error(err, "Unable to decode Kubegres StatefulSet. Given error: ")
		return apps.StatefulSet{}, err
	}

	return *obj.(*apps.StatefulSet), nil
}

func (r *ResourceTemplateLoader) decodeYaml(yamlContents string) (runtime.Object, error) {

	decode := scheme.Codecs.UniversalDeserializer().Decode

	obj, _, err := decode([]byte(yamlContents), nil, nil)

	if err != nil {
		r.log.Error(err, "Error in decode: ", "obj", obj)
	}

	return obj, err
}

var (
	//go:embed tls/postgres.conf
	postgresConfTemplate embed.FS
	//go:embed tls/pg_hba.conf
	pgHbaConf embed.FS
	//go:embed tls/copy_primary_data_to_replica.sh
	copyPrimaryDataToReplicaScript embed.FS
	//go:embed tls/backup_database.sh
	backupDatabaseTemplate embed.FS
)

type templateData struct {
	RootCertPath string
	CertPath     string
	KeyPath      string
}

func LoadTLSPgHbaConf() ([]byte, error) {
	content, err := pgHbaConf.ReadFile("tls/pg_hba.conf")
	if err != nil {
		return nil, fmt.Errorf("read pg_hba.conf file: %w", err)
	}
	return content, nil
}

func LoadTLSPostgresConf(tls v1.TLS) ([]byte, error) {
	return executeTemplate(postgresConfTemplate, "tls/postgres.conf", templateData{
		RootCertPath: tls.RootCertPath,
		CertPath:     tls.ServerCertPath,
		KeyPath:      tls.ServerKeyPath,
	})
}

func LoadTLSCopyPrimaryDataScript(tls v1.TLS) ([]byte, error) {
	return executeTemplate(copyPrimaryDataToReplicaScript, "tls/copy_primary_data_to_replica.sh", templateData{
		RootCertPath: tls.RootCertPath,
		CertPath:     tls.ClientCertPath,
		KeyPath:      tls.ClientKeyPath,
	})
}

func LoadTLSBackupScript(tls v1.TLS) ([]byte, error) {
	return executeTemplate(backupDatabaseTemplate, "tls/backup_database.sh", templateData{
		RootCertPath: tls.RootCertPath,
		CertPath:     tls.ClientCertPath,
		KeyPath:      tls.ClientKeyPath,
	})
}

func executeTemplate(fs embed.FS, path string, data interface{}) ([]byte, error) {
	content, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template file %s: %w", path, err)
	}

	t, err := template.New(path).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", path, err)
	}

	result := bytes.Buffer{}
	err = t.Execute(&result, data)
	if err != nil {
		return nil, fmt.Errorf("execute template %s: %w", path, err)
	}
	return result.Bytes(), nil
}
