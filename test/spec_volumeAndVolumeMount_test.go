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
package test

import (
	"log"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	postgresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/testcases"
	"sigs.k8s.io/yaml"
)

var _ = Describe("Setting Kubegres specs 'volume.volume' and 'volume.volumeMount'", Label("group:5"), func() {

	var test = SpecVolumeAndVolumeMountTest{}

	BeforeEach(func() {
		//Skip("Temporarily skipping test")

		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.dbQueryTestCases = testcases.InitDbQueryTestCases(test.resourceCreator, resourceConfigs.KubegresResourceName, k8sClientTest)
	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
		} else {
			test.keepCreatedResourcesForNextTest = false
		}
	})

	Context("GIVEN new Kubegres is created with a 'volume.primary.volume' and 'volume.primary.volumeMount'", func() {

		It("THEN only primary statefulset should have volume and volumeMount created", func() {
			volume := test.givenVolumeWithEmptyDir("a-volume")
			volumes := []corev1.Volume{volume}

			volumeMount := test.givenVolumeMount("a-volume", "/mountpoint")
			volumeMounts := []corev1.VolumeMount{volumeMount}

			test.givenNewKubegresSpecPrimaryVolumesIsSetTo(volumes, volumeMounts, 3)

			test.whenKubegresIsCreated()

			test.thenNbreOfReplicasShouldBe(1, 2)

			test.thenPrimaryStatefulSetsShouldHave(volumes, volumeMounts)
			test.thenReplicasStatefulSetsShouldNotHave(volumes, volumeMounts)

			test.keepCreatedResourcesForNextTest = true
		})

		It("THEN we update existing Kubegres by adding new custom 'volume.primary.volume' and 'volume.primary.volumeMount'", func() {
			updatedVolume := test.givenVolumeWithEmptyDir("updated-volume")
			volumes := []corev1.Volume{updatedVolume}
			updatedVolumeMount := test.givenVolumeMount("updated-volume", "/updated-mountpoint")
			volumeMounts := []corev1.VolumeMount{updatedVolumeMount}

			test.givenExistingKubegresSpecPrimaryVolumesIsUpdatedTo(volumes, volumeMounts)

			test.whenKubernetesIsUpdated()

			test.waitForStatefulSetsToBeUpdated()

			test.thenNbreOfReplicasShouldBe(1, 2)

			test.thenPrimaryStatefulSetsShouldHave(volumes, volumeMounts)
			test.thenReplicasStatefulSetsShouldNotHave(volumes, volumeMounts)

			test.keepCreatedResourcesForNextTest = true
		})

		It("THEN we update existing Kubegres by adding new custom 'volume.volumes' and 'volume.volumeMounts' to primary spec", func() {
			exitingPrimaryVolume := test.givenVolumeWithEmptyDir("updated-volume")
			primaryVolumes := []corev1.Volume{exitingPrimaryVolume}
			existingPrimaryVolumeMount := test.givenVolumeMount("updated-volume", "/updated-mountpoint")
			primaryVolumeMounts := []corev1.VolumeMount{existingPrimaryVolumeMount}

			addedVolume := test.givenVolumeWithEmptyDir("added-volume")
			volumes := []corev1.Volume{addedVolume}
			addedVolumeMount := test.givenVolumeMount("added-volume", "/added-mountpoint")
			volumeMounts := []corev1.VolumeMount{addedVolumeMount}

			test.givenExistingKubegresSpecVolumesIsUpdatedTo(volumes, volumeMounts)
			test.whenKubernetesIsUpdated()

			test.waitForStatefulSetsToBeUpdated()

			test.thenNbreOfReplicasShouldBe(1, 2)

			expectedPrimaryVolumes := append(primaryVolumes, volumes...)
			expectedPrimaryVolumeMounts := append(primaryVolumeMounts, volumeMounts...)

			test.thenPrimaryStatefulSetsShouldHave(expectedPrimaryVolumes, expectedPrimaryVolumeMounts)
			test.thenReplicasStatefulSetsShouldHave(volumes, volumeMounts)

			test.keepCreatedResourcesForNextTest = true
		})

		It("THEN we update existing Kubegres by removing custom 'volume.primary.volume' and 'volume.primary.volumeMount'", func() {
			removedPrimaryVol := test.givenVolumeWithEmptyDir("updated-volume")
			removedPrimaryVolumes := []corev1.Volume{removedPrimaryVol}
			removedPrimaryVolumeMount := test.givenVolumeMount("updated-volume", "/updated-mountpoint")
			removedPrimaryVolumeMounts := []corev1.VolumeMount{removedPrimaryVolumeMount}

			existingVolume := test.givenVolumeWithEmptyDir("added-volume")
			volumes := []corev1.Volume{existingVolume}
			existingVolumeMount := test.givenVolumeMount("added-volume", "/added-mountpoint")
			volumeMounts := []corev1.VolumeMount{existingVolumeMount}

			test.givenExistingKubegresSpecPrimaryVolumesIsUpdatedTo(nil, nil)
			test.whenKubernetesIsUpdated()

			test.waitForStatefulSetsToBeUpdated()

			test.thenNbreOfReplicasShouldBe(1, 2)

			test.thenPrimaryStatefulSetsShouldHave(volumes, volumeMounts)
			test.thenReplicasStatefulSetsShouldHave(volumes, volumeMounts)

			test.thenPrimaryStatefulSetsShouldNotHave(removedPrimaryVolumes, removedPrimaryVolumeMounts)

			test.keepCreatedResourcesForNextTest = true
		})

		It("THEN we update existing Kubegres by removing all custom 'volume.volumes' and 'volume.volumeMounts' from primary spec", func() {
			removedVolume := test.givenVolumeWithEmptyDir("added-volume")
			removedVolumes := []corev1.Volume{removedVolume}
			removedVolumeMount := test.givenVolumeMount("added-volume", "/added-mountpoint")
			removedVolumeMounts := []corev1.VolumeMount{removedVolumeMount}

			test.givenExistingKubegresSpecVolumesIsUpdatedTo(nil, nil)
			test.whenKubernetesIsUpdated()

			test.waitForStatefulSetsToBeUpdated()

			test.thenNbreOfReplicasShouldBe(1, 2)

			test.thenPrimaryStatefulSetsShouldNotHave(removedVolumes, removedVolumeMounts)
			test.thenReplicasStatefulSetsShouldNotHave(removedVolumes, removedVolumeMounts)

		})
	})

	Context("GIVEN new Kubegres is created with a 'volume.volume' and 'volume.volumeMount' which have a reserved name", func() {

		It("THEN 2 error events should be logged as it is not possible to use a reserved name", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with a 'volume.volume' and 'volume.volumeMount' " +
				"which have a reserved name'")

			reservedVolumeName := ctx.DatabaseVolumeName

			cacheVolume := test.givenVolumeWithEmptyDir(reservedVolumeName)
			customVolumes := []corev1.Volume{cacheVolume}

			cacheVolumeMount := test.givenVolumeMount(reservedVolumeName, "/cache")
			customVolumeMounts := []corev1.VolumeMount{cacheVolumeMount}

			test.givenNewKubegresSpecIsSetTo(customVolumes, customVolumeMounts, 3)

			test.whenKubegresIsCreated()

			test.thenErrorEventShouldBeLoggedAboutVolumeName()
			test.thenErrorEventShouldBeLoggedAboutVolumeMountName()

			log.Print("END OF: Test 'GIVEN new Kubegres is created with a 'volume.volume' and 'volume.volumeMount' " +
				"which have a reserved name'")
		})
	})

	Context("GIVEN new Kubegres is created with a 'volume.volumeMount' which has a mountPath set to the path of "+
		"Postgres database folder", func() {

		It("THEN an error event should be logged as it is not possible to use Postgres database as a mountPath", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with a 'volume.volumeMount' which has a mountPath " +
				"set to the path of Postgres database folder'")

			cacheVolume := test.givenVolumeWithEmptyDir("cache-volume")
			customVolumes := []corev1.Volume{cacheVolume}

			postgresDatabasePath := test.kubegresResource.Spec.Database.VolumeMount
			cacheVolumeMount := test.givenVolumeMount("cache-volume", postgresDatabasePath)
			customVolumeMounts := []corev1.VolumeMount{cacheVolumeMount}

			test.givenNewKubegresSpecIsSetTo(customVolumes, customVolumeMounts, 3)

			test.whenKubegresIsCreated()

			test.thenErrorEventShouldBeLoggedAboutVolumeMountPath()

			log.Print("END OF: Test 'GIVEN new Kubegres is created with a 'volume.volumeMount' which has a mountPath " +
				"set to the path of Postgres database folder'")
		})
	})

	Context("GIVEN new Kubegres is created with specs 'volume.volume' and 'volume.volumeMount' and later "+
		"we update them and add additional volumes and finally we delete them", func() {

		It("GIVEN new Kubegres is created with a new custom 'volume.volume' and 'volume.volumeMount' AND spec 'replica' "+
			"set to 3 THEN 1 primary and 2 replica should be created with one custom volume and volumeMount in StatefulSets", func() {

			log.Print("GIVEN new Kubegres is created with a new custom 'volume.volume' and 'volume.volumeMount' and spec 'replica' set to 3")

			shmVolume := test.givenVolumeWithMemory("dshm", "200Mi")
			customVolumes := []corev1.Volume{shmVolume}

			shmVolumeMount := test.givenVolumeMount("dshm", "/dev/shm")
			customVolumeMounts := []corev1.VolumeMount{shmVolumeMount}

			test.givenNewKubegresSpecIsSetTo(customVolumes, customVolumeMounts, 3)

			test.whenKubegresIsCreated()

			test.thenStatefulSetsStatesShouldBe(customVolumes, customVolumeMounts, 1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo(customVolumes, customVolumeMounts)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN new Kubegres is created with a new custom 'volume.volume' and 'volume.volumeMount' " +
				"and spec 'replica' set to 3'")
		})

		It("GIVEN existing Kubegres is updated by adding new and updating existing custom 'volume.volume' and 'volume.volumeMount' THEN "+
			"StatefulSets should be updated too", func() {

			log.Print("START OF: Test 'GIVEN existing Kubegres is updated by adding new and updating existing custom 'volume.volume' and 'volume.volumeMount'")

			shmVolume := test.givenVolumeWithMemory("dshm", "300Mi")
			cacheVolume := test.givenVolumeWithEmptyDir("cache-volume")
			customVolumesToAddOrUpdate := []corev1.Volume{shmVolume, cacheVolume}

			cacheVolumeMount := test.givenVolumeMount("cache-volume", "/cache")
			customVolumeMountsToAddOrUpdate := []corev1.VolumeMount{cacheVolumeMount}

			test.givenVolumesAreUpdatedOrAddedToTheExistingKubegresSpec(customVolumesToAddOrUpdate, customVolumeMountsToAddOrUpdate)

			test.whenKubernetesIsUpdated()

			shmVolume = test.givenVolumeWithMemory("dshm", "300Mi")
			cacheVolume = test.givenVolumeWithEmptyDir("cache-volume")
			expectedCustomVolumes := []corev1.Volume{shmVolume, cacheVolume}

			shmVolumeMount := test.givenVolumeMount("dshm", "/dev/shm")
			cacheVolumeMount = test.givenVolumeMount("cache-volume", "/cache")
			expectedCustomVolumeMounts := []corev1.VolumeMount{shmVolumeMount, cacheVolumeMount}

			test.thenStatefulSetsStatesShouldBe(expectedCustomVolumes, expectedCustomVolumeMounts, 1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo(expectedCustomVolumes, expectedCustomVolumeMounts)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN existing Kubegres is updated by adding new and updating existing custom 'volume.volume' and 'volume.volumeMount'")
		})

		It("GIVEN existing Kubegres is updated with the removal of one custom 'volume.volume' and 'volume.volumeMount' "+
			"THEN StatefulSets should be updated too", func() {

			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with the removal of one custom 'volume.volume' and 'volume.volumeMount'")

			shmVolume := test.givenVolumeWithMemory("dshm", "300Mi")
			customVolumesToRemove := []corev1.Volume{shmVolume}

			shmVolumeMount := test.givenVolumeMount("dshm", "/dev/shm")
			customVolumeMountsToRemove := []corev1.VolumeMount{shmVolumeMount}

			test.givenVolumesAreRemovedFromTheExistingKubegresSpec(customVolumesToRemove, customVolumeMountsToRemove)

			test.whenKubernetesIsUpdated()

			cacheVolume := test.givenVolumeWithEmptyDir("cache-volume")
			expectedCustomVolumes := []corev1.Volume{cacheVolume}

			cacheVolumeMount := test.givenVolumeMount("cache-volume", "/cache")
			expectedCustomVolumeMounts := []corev1.VolumeMount{cacheVolumeMount}

			test.thenStatefulSetsStatesShouldBe(expectedCustomVolumes, expectedCustomVolumeMounts, 1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo(expectedCustomVolumes, expectedCustomVolumeMounts)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with the removal of one custom 'volume.volume' and 'volume.volumeMount'")
		})

		It("GIVEN existing Kubegres is updated with the removal of all custom 'volume.volume' and 'volume.volumeMount' "+
			"THEN StatefulSets should be updated too", func() {

			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with the removal of all custom 'volume.volume' and 'volume.volumeMount'")

			cacheVolume := test.givenVolumeWithEmptyDir("cache-volume")
			customVolumesToRemove := []corev1.Volume{cacheVolume}

			cacheVolumeMount := test.givenVolumeMount("cache-volume", "/cache")
			customVolumeMountsToRemove := []corev1.VolumeMount{cacheVolumeMount}

			test.givenVolumesAreRemovedFromTheExistingKubegresSpec(customVolumesToRemove, customVolumeMountsToRemove)

			test.whenKubernetesIsUpdated()

			expectedCustomVolumes := []corev1.Volume{}
			expectedCustomVolumeMounts := []corev1.VolumeMount{}
			test.thenStatefulSetsStatesShouldBe(expectedCustomVolumes, expectedCustomVolumeMounts, 1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo(expectedCustomVolumes, expectedCustomVolumeMounts)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with the removal of all custom 'volume.volume' and 'volume.volumeMount'")
		})
	})

})

type SpecVolumeAndVolumeMountTest struct {
	keepCreatedResourcesForNextTest bool
	kubegresResource                *postgresv1.Kubegres
	dbQueryTestCases                testcases.DbQueryTestCases
	resourceCreator                 util.TestResourceCreator
	resourceRetriever               util.TestResourceRetriever
}

func (r *SpecVolumeAndVolumeMountTest) givenVolumeWithMemory(volumeName, memoryQuantity string) corev1.Volume {

	memQuantity := resource.MustParse(memoryQuantity)

	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: &memQuantity,
			},
		},
	}
}

func (r *SpecVolumeAndVolumeMountTest) givenVolumeWithEmptyDir(volumeName string) corev1.Volume {

	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

func (r *SpecVolumeAndVolumeMountTest) givenVolumeMount(volumeName, mountPath string) corev1.VolumeMount {

	return corev1.VolumeMount{
		Name:      volumeName,
		MountPath: mountPath,
	}
}

func (r *SpecVolumeAndVolumeMountTest) givenNewKubegresSpecPrimaryVolumesIsSetTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, specNbreReplicas int32) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Volume.Primary.Volumes = volumes
	r.kubegresResource.Spec.Volume.Primary.VolumeMounts = mounts
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *SpecVolumeAndVolumeMountTest) givenNewKubegresSpecIsSetTo(
	customVolumes []corev1.Volume,
	customVolumeMounts []corev1.VolumeMount,
	specNbreReplicas int32) {

	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Volume.Volumes = append(r.kubegresResource.Spec.Volume.Volumes, customVolumes...)
	r.kubegresResource.Spec.Volume.VolumeMounts = append(r.kubegresResource.Spec.Volume.VolumeMounts, customVolumeMounts...)
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *SpecVolumeAndVolumeMountTest) givenVolumesAreUpdatedOrAddedToTheExistingKubegresSpec(
	customVolumesToAddOrReplace []corev1.Volume,
	customVolumeMountsToAddOrReplace []corev1.VolumeMount) {

	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	for _, customVolume := range customVolumesToAddOrReplace {
		volumeIndex := r.getVolumeIndex(customVolume)
		if volumeIndex >= 0 {
			r.kubegresResource.Spec.Volume.Volumes[volumeIndex] = customVolume
		} else {
			r.kubegresResource.Spec.Volume.Volumes = append(r.kubegresResource.Spec.Volume.Volumes, customVolume)
		}
	}

	for _, customVolumeMount := range customVolumeMountsToAddOrReplace {
		volumeMountIndex := r.getVolumeMountIndex(customVolumeMount)
		if volumeMountIndex >= 0 {
			r.kubegresResource.Spec.Volume.VolumeMounts[volumeMountIndex] = customVolumeMount
		} else {
			r.kubegresResource.Spec.Volume.VolumeMounts = append(r.kubegresResource.Spec.Volume.VolumeMounts, customVolumeMount)
		}
	}
}

func (r *SpecVolumeAndVolumeMountTest) givenVolumesAreRemovedFromTheExistingKubegresSpec(
	customVolumesToRemove []corev1.Volume,
	customVolumeMountsToRemove []corev1.VolumeMount) {

	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	for _, customVolume := range customVolumesToRemove {
		volumeIndex := r.getVolumeIndex(customVolume)
		if volumeIndex >= 0 {
			r.kubegresResource.Spec.Volume.Volumes = append(r.kubegresResource.Spec.Volume.Volumes[:volumeIndex], r.kubegresResource.Spec.Volume.Volumes[volumeIndex+1:]...)
		}
	}

	for _, customVolumeMount := range customVolumeMountsToRemove {
		volumeMountIndex := r.getVolumeMountIndex(customVolumeMount)
		if volumeMountIndex >= 0 {
			r.kubegresResource.Spec.Volume.VolumeMounts = append(r.kubegresResource.Spec.Volume.VolumeMounts[:volumeMountIndex], r.kubegresResource.Spec.Volume.VolumeMounts[volumeMountIndex+1:]...)
		}
	}
}

func (r *SpecVolumeAndVolumeMountTest) getVolumeIndex(customVolume corev1.Volume) int {
	index := 0
	for _, volume := range r.kubegresResource.Spec.Volume.Volumes {
		if customVolume.Name == volume.Name {
			return index
		}
		index++
	}
	return -1
}

func (r *SpecVolumeAndVolumeMountTest) getVolumeMountIndex(customVolumeMount corev1.VolumeMount) int {
	index := 0
	for _, volumeMount := range r.kubegresResource.Spec.Volume.VolumeMounts {
		if customVolumeMount.Name == volumeMount.Name {
			return index
		}
		index++
	}
	return -1
}

func (r *SpecVolumeAndVolumeMountTest) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

func (r *SpecVolumeAndVolumeMountTest) whenKubernetesIsUpdated() {
	r.resourceCreator.UpdateResource(r.kubegresResource, "Kubegres")
}

func (r *SpecVolumeAndVolumeMountTest) thenErrorEventShouldBeLoggedAboutVolumeName() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: corev1.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message: "In the Resources Spec the value of 'spec.Volume.Volumes' has an entry with a volume name " +
			"which is a reserved name: " + ctx.DatabaseVolumeName + " . That name cannot be used and it is reserved for " +
			"Kubegres internal usages. Please change that name in the YAML.",
	}
	Eventually(func() bool {
		_, err := r.resourceRetriever.GetKubegres()
		if err != nil {
			return false
		}
		return eventRecorderTest.CheckEventExist(expectedErrorEvent)

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecVolumeAndVolumeMountTest) thenErrorEventShouldBeLoggedAboutVolumeMountName() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: corev1.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message: "In the Resources Spec the value of 'spec.Volume.VolumeMounts' has an entry with a volume name " +
			"which is a reserved name: " + ctx.DatabaseVolumeName + " . That name cannot be used and it is reserved for " +
			"Kubegres internal usages. Please change that name in the YAML.",
	}
	Eventually(func() bool {
		_, err := r.resourceRetriever.GetKubegres()
		if err != nil {
			return false
		}
		return eventRecorderTest.CheckEventExist(expectedErrorEvent)

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecVolumeAndVolumeMountTest) thenErrorEventShouldBeLoggedAboutVolumeMountPath() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: corev1.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message: "In the Resources Spec the value of 'spec.Volume.VolumeMounts' has an entry with a 'mountPath' value " +
			"which is reserved for the Postgres database: " + r.kubegresResource.Spec.Database.VolumeMount + " . " +
			"That value cannot be used and it is reserved for Kubegres internal usages. Please change that value in the YAML.",
	}
	Eventually(func() bool {
		_, err := r.resourceRetriever.GetKubegres()
		if err != nil {
			return false
		}
		return eventRecorderTest.CheckEventExist(expectedErrorEvent)

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecVolumeAndVolumeMountTest) thenStatefulSetsStatesShouldBe(
	expectedCustomVolumes []corev1.Volume,
	expectedCustomVolumeMounts []corev1.VolumeMount,
	nbrePrimary,
	nbreReplicas int) bool {

	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		kubegresContext := ctx.KubegresContext{}

		for _, resource := range kubegresResources.Resources {

			for _, customVolume := range expectedCustomVolumes {
				if !r.doesCustomVolumeExistsInStatefulSet(customVolume, resource.StatefulSet.Spec.Template.Spec.Volumes) {
					log.Println("StatefulSet '" + resource.StatefulSet.Name + "' doesn't have the expected custom volume with name: '" + customVolume.Name + "'. Waiting...")
					return false
				}
			}

			for _, volumeInStatefulSet := range resource.StatefulSet.Spec.Template.Spec.Volumes {
				if r.isCustomVolume(volumeInStatefulSet, kubegresContext) &&
					!r.isVolumeAnExpectedCustomVolume(volumeInStatefulSet, expectedCustomVolumes) {
					log.Println("StatefulSet '" + resource.StatefulSet.Name + "' still has custom volume with name: '" + volumeInStatefulSet.Name + "'. Waiting...")
					return false
				}
			}

			for _, customVolumeMount := range expectedCustomVolumeMounts {
				for _, container := range resource.StatefulSet.Spec.Template.Spec.Containers {
					if !r.doesCustomVolumeMountExistsInStatefulSet(customVolumeMount, container.VolumeMounts) {
						log.Printf("Container %q of StatefulSet %q doesn't have the expected custom volumeMount with name: %q. Waiting...\n",
							container.Name, resource.StatefulSet.Name, customVolumeMount.Name)
						return false
					}
				}

				if len(resource.StatefulSet.Spec.Template.Spec.InitContainers) > 0 {
					if !r.doesCustomVolumeMountExistsInStatefulSet(customVolumeMount, resource.StatefulSet.Spec.Template.Spec.InitContainers[0].VolumeMounts) {
						log.Println("StatefulSet '" + resource.StatefulSet.Name + "' doesn't have the expected custom volumeMount in init container with name: '" + customVolumeMount.Name + "'. Waiting...")
						return false
					}
				}
			}

			for _, container := range resource.StatefulSet.Spec.Template.Spec.Containers {
				for _, volumeMountInStatefulSet := range container.VolumeMounts {
					if r.isCustomVolumeMount(volumeMountInStatefulSet, kubegresContext) &&
						!r.isVolumeMountAnExpectedCustomVolumeMount(volumeMountInStatefulSet, expectedCustomVolumeMounts) {
						log.Printf("Container %q of StatefulSet %q still has custom volumeMount with name: %q. Waiting...\n", container.Name, resource.StatefulSet.Name,
							volumeMountInStatefulSet.Name)
						return false
					}
				}
			}
			if len(resource.StatefulSet.Spec.Template.Spec.InitContainers) > 0 {
				for _, volumeMountInStatefulSet := range resource.StatefulSet.Spec.Template.Spec.InitContainers[0].VolumeMounts {
					if r.isCustomVolumeMount(volumeMountInStatefulSet, kubegresContext) &&
						!r.isVolumeMountAnExpectedCustomVolumeMount(volumeMountInStatefulSet, expectedCustomVolumeMounts) {
						log.Println("StatefulSet '" + resource.StatefulSet.Name + "' still has custom volumeMount in init container with name: '" + volumeMountInStatefulSet.Name + "'. Waiting...")
						return false
					}
				}
			}
		}

		if kubegresResources.AreAllReady &&
			kubegresResources.NbreDeployedPrimary == nbrePrimary &&
			kubegresResources.NbreDeployedReplicas == nbreReplicas {

			time.Sleep(resourceConfigs.TestRetryInterval)
			log.Println("Deployed and Ready StatefulSet check successful")
			return true
		}

		log.Println("Deployed and Ready StatefulSet check failed. Waiting for the next check...")
		return false

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecVolumeAndVolumeMountTest) thenPrimaryStatefulSetsShouldHave(volumes []corev1.Volume, mounts []corev1.VolumeMount) {
	Eventually(func() bool {
		resources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error while getting Kubegres resources : ", err)
			return false
		}
		if !resources.AreAllReady {
			log.Println("Not all StatefulSets are ready yet")
			return false
		}
		primaryStatefulset, err := r.resourceRetriever.GetPrimaryStatefulSet()
		if err != nil {
			log.Println("Error while getting Primary StatefulSet : ", err)
			return false
		}
		for _, vol := range volumes {
			if !r.doesCustomVolumeExistsInStatefulSet(vol, primaryStatefulset.Spec.Template.Spec.Volumes) {
				got, err := yaml.Marshal(primaryStatefulset.Spec.Template.Spec.Volumes)
				if err != nil {
					log.Println("Error while marshalling volumes : ", err)
				}
				marshal, err := yaml.Marshal(vol)
				if err != nil {
					log.Println("Error while marshalling expected volume : ", err)
				}
				log.Println("Primary StatefulSet volumes are not as expected. Got: ", string(got), " Expected: ", string(marshal))
				return false
			}
		}
		for _, container := range primaryStatefulset.Spec.Template.Spec.Containers {
			for _, mount := range mounts {
				if !r.doesCustomVolumeMountExistsInStatefulSet(mount, container.VolumeMounts) {
					got, err := yaml.Marshal(container.VolumeMounts)
					if err != nil {
						log.Println("Error while marshalling volumeMounts : ", err)
					}
					want, err := yaml.Marshal(mounts)
					if err != nil {
						log.Println("Error while marshalling expected volumeMounts : ", err)
					}
					log.Println("Primary StatefulSet volumeMounts are not as expected. Got: ", string(got), " Expected: ", string(want))
					return false
				}
			}
		}
		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecVolumeAndVolumeMountTest) thenDeployedKubegresSpecShouldBeSetTo(
	expectedCustomVolumes []corev1.Volume,
	expectedCustomVolumeMounts []corev1.VolumeMount) {

	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	if len(expectedCustomVolumes) == 0 && len(expectedCustomVolumeMounts) == 0 {
		return
	}

	Expect(r.kubegresResource.Spec.Volume.Volumes).Should(Equal(expectedCustomVolumes))
	Expect(r.kubegresResource.Spec.Volume.VolumeMounts).Should(Equal(expectedCustomVolumeMounts))
}

func (r *SpecVolumeAndVolumeMountTest) isCustomVolume(volume corev1.Volume, kubegresContext ctx.KubegresContext) bool {
	return !kubegresContext.IsReservedVolumeName(volume.Name)
}

func (r *SpecVolumeAndVolumeMountTest) isCustomVolumeMount(volumeMount corev1.VolumeMount, kubegresContext ctx.KubegresContext) bool {
	return !kubegresContext.IsReservedVolumeName(volumeMount.Name)
}

func (r *SpecVolumeAndVolumeMountTest) doesCustomVolumeExistsInStatefulSet(customVolume corev1.Volume, statefulSetVolumes []corev1.Volume) bool {
	for _, statefulSetVolume := range statefulSetVolumes {
		if reflect.DeepEqual(statefulSetVolume, customVolume) {
			return true
		}
	}
	return false
}

func (r *SpecVolumeAndVolumeMountTest) isVolumeAnExpectedCustomVolume(volumeToCheck corev1.Volume, expectedCustomVolumes []corev1.Volume) bool {
	for _, expectedCustomVolume := range expectedCustomVolumes {
		if reflect.DeepEqual(expectedCustomVolume, volumeToCheck) {
			return true
		}
	}
	return false
}

func (r *SpecVolumeAndVolumeMountTest) isVolumeMountAnExpectedCustomVolumeMount(volumeMountToCheck corev1.VolumeMount, expectedCustomVolumeMounts []corev1.VolumeMount) bool {
	for _, expectedCustomVolumeMount := range expectedCustomVolumeMounts {
		if reflect.DeepEqual(expectedCustomVolumeMount, volumeMountToCheck) {
			return true
		}
	}
	return false
}

func (r *SpecVolumeAndVolumeMountTest) doesCustomVolumeMountExistsInStatefulSet(customVolumeMount corev1.VolumeMount, statefulSetVolumeMounts []corev1.VolumeMount) bool {
	for _, statefulSetVolumeMount := range statefulSetVolumeMounts {
		if reflect.DeepEqual(statefulSetVolumeMount, customVolumeMount) {
			return true
		}
	}
	return false
}

func (r *SpecVolumeAndVolumeMountTest) thenReplicasStatefulSetsShouldNotHave(volumes []corev1.Volume, mounts []corev1.VolumeMount) {
	Eventually(func() bool {
		resources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error while getting Kubegres resources : ", err)
			return false
		}
		if !resources.AreAllReady {
			log.Println("Not all StatefulSets are ready yet")
			return false
		}
		for _, resource := range resources.Resources {
			role, found := resource.StatefulSet.Metadata.Labels["replicationRole"]
			if !found {
				log.Println("Statefulset does not have 'replicationRole' label")
				return false
			}
			if role != "replica" {
				log.Println("Skipping non-replica statefulset: ", resource.StatefulSet.Name)
				continue
			}
			for _, vol := range volumes {
				if r.doesCustomVolumeExistsInStatefulSet(vol, resource.StatefulSet.Spec.Template.Spec.Volumes) {
					log.Println("Replica StatefulSet '" + resource.StatefulSet.Name + "' has custom volume with name: '" + vol.Name)
					return false
				}
			}
			for _, container := range resource.StatefulSet.Spec.Template.Spec.Containers {
				for _, mount := range mounts {
					if r.doesCustomVolumeMountExistsInStatefulSet(mount, container.VolumeMounts) {
						log.Println("Replica StatefulSet '" + resource.StatefulSet.Name + "' has custom volumeMount with name: '" + mount.Name)
						return false
					}
				}
			}
		}
		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())

}

func (r *SpecVolumeAndVolumeMountTest) thenNbreOfReplicasShouldBe(nbrePrimary, nbreReplicas int) {
	Eventually(func() bool {
		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error while retrieving Kubegres kubegresResources")
			return false
		}
		if kubegresResources.NbreDeployedPrimary != nbrePrimary {
			log.Printf("Number of deployed primary is %d but expected %d. Waiting...\n", kubegresResources.NbreDeployedPrimary, nbrePrimary)
			return false
		}
		if kubegresResources.NbreDeployedReplicas != nbreReplicas {
			log.Printf("Number of deployed replicas is %d but expected %d. Waiting...\n", kubegresResources.NbreDeployedReplicas, nbreReplicas)
			return false
		}
		if !kubegresResources.AreAllReady {
			log.Println("Not all StatefulSets are ready yet. Waiting...")
			return false
		}
		return true

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())

}

func (r *SpecVolumeAndVolumeMountTest) givenExistingKubegresSpecPrimaryVolumesIsUpdatedTo(volumes []corev1.Volume, mounts []corev1.VolumeMount) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.Volume.Primary.Volumes = volumes
	r.kubegresResource.Spec.Volume.Primary.VolumeMounts = mounts
}

func (r *SpecVolumeAndVolumeMountTest) givenExistingKubegresSpecVolumesIsUpdatedTo(volumes []corev1.Volume, mounts []corev1.VolumeMount) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.Volume.Volumes = volumes
	r.kubegresResource.Spec.Volume.VolumeMounts = mounts

}

func (r *SpecVolumeAndVolumeMountTest) thenReplicasStatefulSetsShouldHave(volumes []corev1.Volume, mounts []corev1.VolumeMount) {

	Eventually(func() bool {
		resources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error while getting Kubegres resources : ", err)
			return false
		}
		if !resources.AreAllReady {
			log.Println("Not all StatefulSets are ready yet")
			return false
		}
		for _, resource := range resources.Resources {
			role, found := resource.StatefulSet.Metadata.Labels["replicationRole"]
			if !found {
				log.Println("Statefulset does not have 'replicationRole' label")
				return false
			}
			if role != "replica" {
				log.Println("Skipping non-replica statefulset: ", resource.StatefulSet.Name)
				continue
			}
			for _, vol := range volumes {
				if !r.doesCustomVolumeExistsInStatefulSet(vol, resource.StatefulSet.Spec.Template.Spec.Volumes) {
					log.Println("Replica StatefulSet '" + resource.StatefulSet.Name + "' doesn't have the expected custom volume with name: '" + vol.Name + "'. Waiting...")
					return false
				}
			}
			for _, container := range resource.StatefulSet.Spec.Template.Spec.Containers {
				for _, mount := range mounts {
					if !r.doesCustomVolumeMountExistsInStatefulSet(mount, container.VolumeMounts) {
						log.Println("Replica StatefulSet '" + resource.StatefulSet.Name + "' doesn't have the expected custom volumeMount with name: '" + mount.Name + "'. Waiting...")
						return false
					}
				}
			}
		}
		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecVolumeAndVolumeMountTest) thenPrimaryStatefulSetsShouldNotHave(volumes []corev1.Volume, mounts []corev1.VolumeMount) {
	Eventually(func() bool {
		resources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error while getting Kubegres resources : ", err)
			return false
		}
		if !resources.AreAllReady {
			log.Println("Not all StatefulSets are ready yet")
			return false
		}
		primaryStatefulset, err := r.resourceRetriever.GetPrimaryStatefulSet()
		if err != nil {
			log.Println("Error while getting Primary StatefulSet : ", err)
			return false
		}
		for _, vol := range volumes {
			if r.doesCustomVolumeExistsInStatefulSet(vol, primaryStatefulset.Spec.Template.Spec.Volumes) {
				log.Println("Primary StatefulSet has custom volume with name: '" + vol.Name + "'")
				return false
			}
		}
		for _, container := range primaryStatefulset.Spec.Template.Spec.Containers {
			for _, mount := range mounts {
				if r.doesCustomVolumeMountExistsInStatefulSet(mount, container.VolumeMounts) {
					log.Println("Primary StatefulSet has custom volumeMount with name: '" + mount.Name + "'")
					return false
				}
			}
		}
		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())

}

func (r *SpecVolumeAndVolumeMountTest) waitForStatefulSetsToBeUpdated() {
	Eventually(func() bool {
		resources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error while getting Kubegres resources : ", err)
			return false
		}
		if resources.AreAllReady {
			log.Println("resources are ready, waiting for update to take effect")
			return false
		}
		return true
	}, resourceConfigs.TestTimeout, 500*time.Millisecond).Should(BeTrue())

}
