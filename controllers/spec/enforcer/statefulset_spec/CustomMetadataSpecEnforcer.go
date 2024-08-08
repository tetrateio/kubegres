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

package statefulset_spec

import (
	"maps"
	"strings"

	apps "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
)

const annotationPrefix = "kubegres.reactive-tech.io/"

type (
	CustomMetadataSpecEnforcer struct {
		kubegresContext ctx.KubegresContext
	}
	customMetadata struct {
		Labels      map[string]string
		Annotations map[string]string
	}
)

func CreateMetadataSpecEnforcer(kubegresContext ctx.KubegresContext) CustomMetadataSpecEnforcer {
	return CustomMetadataSpecEnforcer{kubegresContext: kubegresContext}
}

func (r *CustomMetadataSpecEnforcer) GetSpecName() string {
	return "Metadata"
}

func (r *CustomMetadataSpecEnforcer) CheckForSpecDifference(statefulSet *apps.StatefulSet) StatefulSetSpecDifference {

	current := getCustomMetadata(statefulSet.GetObjectMeta())
	expected := getCustomMetadata(r.kubegresContext.Kubegres.GetObjectMeta())

	if !r.equals(current, expected) {
		return StatefulSetSpecDifference{
			SpecName: r.GetSpecName(),
			Current:  r.toString(current),
			Expected: r.toString(expected),
		}
	}

	return StatefulSetSpecDifference{}
}

func getCustomMetadata(obj metav1.Object) customMetadata {
	labels := extractCustom(obj.GetLabels())
	annotations := extractCustom(obj.GetAnnotations())
	return customMetadata{Labels: labels, Annotations: annotations}
}

func extractCustom(src map[string]string) map[string]string {
	custom := make(map[string]string)
	for key, value := range src {
		if strings.HasPrefix(key, annotationPrefix) {
			custom[key] = value
		}
	}
	return custom
}

func (r *CustomMetadataSpecEnforcer) EnforceSpec(statefulSet *apps.StatefulSet) (wasSpecUpdated bool, err error) {
	md := getCustomMetadata(r.kubegresContext.Kubegres.GetObjectMeta())
	merge(statefulSet.ObjectMeta.Labels, md.Labels)
	merge(statefulSet.ObjectMeta.Annotations, md.Annotations)
	merge(statefulSet.Spec.Template.ObjectMeta.Labels, md.Labels)
	merge(statefulSet.Spec.Template.ObjectMeta.Annotations, md.Annotations)
	return true, nil
}

func merge(dst map[string]string, src map[string]string) {
	for key, value := range src {
		dst[key] = value
	}
}

func (r *CustomMetadataSpecEnforcer) OnSpecEnforcedSuccessfully(*apps.StatefulSet) error {
	return nil
}

func (r *CustomMetadataSpecEnforcer) equals(current, expected customMetadata) bool {
	return maps.Equal(current.Labels, expected.Labels) &&
		maps.Equal(current.Annotations, expected.Annotations)
}

func (r *CustomMetadataSpecEnforcer) toString(md customMetadata) string {
	labels := make([]string, 0, len(md.Labels))
	for key, value := range md.Labels {
		labels = append(labels, key+"="+value)
	}

	annotations := make([]string, 0, len(md.Annotations))
	for key, value := range md.Annotations {
		annotations = append(annotations, key+"="+value)
	}

	toString := strings.Builder{}
	toString.WriteString("Labels: ")
	toString.WriteString(strings.Join(labels, ", "))
	toString.WriteString(" - Annotations: ")
	toString.WriteString(strings.Join(annotations, ", "))
	return toString.String()
}
