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
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	postgresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/states"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/testcases"
)

var _ = Describe("Managing sidecar containers", Label("sidecars"), func() {

	var (
		test    = sidecarContainerSpec{}
		cmTest  = SpecCustomConfigTest{}
		volTest = SpecVolumeAndVolumeMountTest{}
	)

	BeforeEach(func() {
		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.dbQueryTestCases = testcases.InitDbQueryTestCases(test.resourceCreator, resourceConfigs.KubegresResourceName, k8sClientTest)

		cmTest.resourceRetriever = test.resourceRetriever
		cmTest.resourceCreator = test.resourceCreator
		cmTest.resourceCreator.CreateConfigMapWithPostgresConf()

		volTest.resourceRetriever = test.resourceRetriever
	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
		} else {
			test.keepCreatedResourcesForNextTest = false
		}
	})

	Context("GIVEN new Kubegres is created with spec 'sidecarContainer' and volumes", func() {

		It("THEN attached sidecar container should have the defined volumes mounted", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec sidecarContainer and volumes'")

			shmVolume := volTest.givenVolumeWithMemory("dshm", "200Mi")
			customVolumes := []corev1.Volume{shmVolume}

			shmVolumeMount := volTest.givenVolumeMount("dshm", "/dev/shm")
			customVolumeMounts := []corev1.VolumeMount{shmVolumeMount}

			volTest.givenNewKubegresSpecIsSetTo(customVolumes, customVolumeMounts, 2)
			test.kubegresResource = volTest.kubegresResource
			test.kubegresResource.Spec.SidecarContainers = test.givenSidecarContainers("sidecar", "busybox")

			test.whenKubegresIsCreated()

			test.thenStatefulSetStatesShouldHaveContainer("sidecar", "busybox", []string{"/bin/sleep", "99999"}, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			volTest.thenStatefulSetsStatesShouldBe(customVolumes, customVolumeMounts, 1, 1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec sidecarContainer and volumes'")

		})
		It("THEN replicationSlots are enabled and THEN attached sidecar container should have the defined volumes mounted", func() {
			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec sidecarContainer and volumes and replicationSlots enabled'")

			shmVolume := volTest.givenVolumeWithMemory("dshm", "200Mi")
			customVolumes := []corev1.Volume{shmVolume}

			shmVolumeMount := volTest.givenVolumeMount("dshm", "/dev/shm")
			customVolumeMounts := []corev1.VolumeMount{shmVolumeMount}

			rs := postgresv1.ReplicationSlots{
				Enabled: true,
			}

			test.givenExistingKubegresSpecReplicationSlotsIsSetTo(&rs)
			test.whenKubegresIsUpdated()

			test.thenReplicaShouldHaveReplicationSlotSetAndBeReady()
			test.thenStatefulSetStatesShouldHaveContainer("sidecar", "busybox", []string{"/bin/sleep", "99999"}, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			volTest.thenStatefulSetsStatesShouldBe(customVolumes, customVolumeMounts, 1, 1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec sidecarContainer and volumes and replicationSlots enabled'")

		})

		It("THEN attached sidecar container should have the default config maps mounted", func() {

			log.Print("START OF: Test 'THEN attached sidecar container should have the default config maps mounted'")

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPostgresConf, primary)
			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPostgresConf, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPrimaryInitScript, primary)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPgHbaConf, primary)
			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPgHbaConf, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyCopyPrimaryDataToReplica, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPrimaryCreateReplicaRole, primary)

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN attached sidecar container should have the default config maps mounted'")
		})

		It("THEN if kubegres spec is updated to remove volumes, sidecar container should still have the default config maps mounted", func() {

			log.Print("START OF: Test 'THEN if kubegres spec is updated to remove volumes, sidecar container should still have the default config maps mounted'")

			shmVolume := volTest.givenVolumeWithMemory("dshm", "200Mi")
			customVolumes := []corev1.Volume{shmVolume}

			shmVolumeMount := volTest.givenVolumeMount("dshm", "/dev/shm")
			customVolumeMounts := []corev1.VolumeMount{shmVolumeMount}

			volTest.givenVolumesAreRemovedFromTheExistingKubegresSpec(customVolumes, customVolumeMounts)
			test.kubegresResource = volTest.kubegresResource

			test.whenKubegresIsUpdated()

			test.thenStatefulSetStatesShouldHaveContainer("sidecar", "busybox", []string{"/bin/sleep", "99999"}, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			volTest.thenStatefulSetsStatesShouldBe(make([]corev1.Volume, 0), make([]corev1.VolumeMount, 0), 1, 1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPostgresConf, primary)
			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPostgresConf, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPrimaryInitScript, primary)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPgHbaConf, primary)
			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPgHbaConf, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyCopyPrimaryDataToReplica, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPrimaryCreateReplicaRole, primary)

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN if kubegres spec is updated to remove volumes, sidecar container should still have the default config maps mounted'")

		})

		It("THEN if kubegres spec is updated to use custom config map, sidecar container should have the custom config map mounted", func() {

			log.Print("START OF: Test 'THEN if kubegres spec is updated to use custom config map, sidecar container should have the custom config map mounted'")

			cmTest.givenExistingKubegresSpecIsSetTo(resourceConfigs.CustomConfigMapWithPostgresConfResourceName)
			test.kubegresResource = cmTest.kubegresResource
			test.kubegresResource.Spec.SidecarContainers = test.givenSidecarContainers("sidecar", "busybox")

			test.whenKubegresIsUpdated()

			test.thenStatefulSetStatesShouldHaveContainer("sidecar", "busybox", []string{"/bin/sleep", "99999"}, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			cmTest.thenPodsContainsCustomConfigWithResourceName(resourceConfigs.CustomConfigMapWithPostgresConfResourceName)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.CustomConfigMapVolumeName, states.ConfigMapDataKeyPostgresConf, primary)
			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.CustomConfigMapVolumeName, states.ConfigMapDataKeyPostgresConf, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPrimaryInitScript, primary)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPgHbaConf, primary)
			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPgHbaConf, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyCopyPrimaryDataToReplica, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPrimaryCreateReplicaRole, primary)

			log.Print("END OF: Test 'THEN if kubegres spec is updated to use custom config map, sidecar container should have the custom config map mounted'")
		})

	})

	Context("GIVEN new Kubegres is created with spec 'sidecarContainer'", func() {

		It("THEN created StatefulSets should have sidecarContainer set in pod template", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec sidecarContainer'")

			containers := test.givenSidecarContainers("sidecar", "busybox")

			test.givenNewKubegresSpecHasSidecarContainersSetTo(containers)

			test.whenKubegresIsCreated()

			test.thenStatefulSetStatesShouldHaveContainer("sidecar", "busybox", []string{"/bin/sleep", "99999"}, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec sidecarContainer'")
		})

		It("THEN attached sidecar container should have the default config maps mounted", func() {

			log.Print("START OF: Test 'THEN attached sidecar container should have the default config maps mounted'")

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPostgresConf, primary)
			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPostgresConf, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPrimaryInitScript, primary)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPgHbaConf, primary)
			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPgHbaConf, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyCopyPrimaryDataToReplica, replica)

			cmTest.thenPodsContainsConfigTypeAssociatedToFile(ctx.BaseConfigMapVolumeName, states.ConfigMapDataKeyPrimaryCreateReplicaRole, primary)

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN attached sidecar container should have the default config maps mounted'")

		})

		It("THEN delete `sidecarContainer` field should remove the container form pod template spec", func() {
			log.Print("START OF: Test 'THEN delete `sidecarContainer` field should remove the container form pod template spec")

			test.givenExistingKubegresSpecSidecarContainersIsSetTo(nil)

			test.whenKubegresIsUpdated()

			test.thenStatefulSetStatesShouldNOTHaveContainer("sidecar", "busybox")
			test.thenStatefulSetStatesShouldHaveNbreContainers(1)
			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN delete `sidecarContainer` field shouold remove the container form pod template spec")
		})

		It("THEN add back `sidecarContainer` field should add the container back to pod template spec", func() {
			log.Print("START OF: Test 'THEN add back `sidecarContainer` field should add the container back to pod template spec")

			containers := test.givenSidecarContainers("sidecar", "busybox")
			test.givenExistingKubegresSpecSidecarContainersIsSetTo(containers)

			test.whenKubegresIsUpdated()
			test.thenStatefulSetStatesShouldHaveContainer("sidecar", "busybox", []string{"/bin/sleep", "99999"}, nil)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'THEN add back `sidecarContainer` field should add the container back to pod template spec")
		})

		It("THEN modify `sidecarContainer` args and env should update the container in pod template spec", func() {
			log.Print("START OF: Test 'THEN modify `sidecarContainer` args and env should update the container in pod template spec")

			containers := test.givenSidecarContainers("sidecar", "busybox:1.37.0")
			newCommand := []string{"/bin/sleep", "999991234"}
			newEnv := []corev1.EnvVar{{Name: "FOO", Value: "BAR"}}
			containers[0].Command = newCommand
			containers[0].Env = newEnv
			test.givenExistingKubegresSpecSidecarContainersIsSetTo(containers)

			test.whenKubegresIsUpdated()
			test.thenStatefulSetStatesShouldHaveContainer("sidecar", "busybox:1.37.0", newCommand, newEnv)
			test.thenStatefulSetStatesShouldHaveNbreContainers(2) // 1 main container + 1 sidecar container

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			log.Print("END OF: Test 'THEN modify `sidecarContainer` args and env should update the container in pod template spec")
		})

	})

})

type sidecarContainerSpec struct {
	keepCreatedResourcesForNextTest bool
	kubegresResource                *postgresv1.Kubegres
	dbQueryTestCases                testcases.DbQueryTestCases
	resourceCreator                 util.TestResourceCreator
	resourceRetriever               util.TestResourceRetriever
}

func (r *sidecarContainerSpec) whenKubegresIsUpdated() {
	r.resourceCreator.UpdateResource(r.kubegresResource, "Kubegres")
}

func (r *sidecarContainerSpec) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

func (r *sidecarContainerSpec) givenSidecarContainers(name, image string) []corev1.Container {
	return []corev1.Container{
		{
			Name:    name,
			Image:   image,
			Command: []string{"/bin/sleep", "99999"},
		},
	}
}

func (r *sidecarContainerSpec) givenNewKubegresSpecHasSidecarContainersSetTo(containers []corev1.Container) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.SidecarContainers = containers
	two := int32(2)
	r.kubegresResource.Spec.Replicas = &two
}

func (r *sidecarContainerSpec) givenExistingKubegresSpecSidecarContainersIsSetTo(containers []corev1.Container) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.SidecarContainers = containers
}

func (r *sidecarContainerSpec) thenStatefulSetStatesShouldHaveNbreContainers(numberOfContainers int) bool {
	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		if kubegresResources.AreAllReady != true {
			return false
		}

		for _, res := range kubegresResources.Resources {
			if len(res.StatefulSet.Spec.Template.Spec.Containers) != numberOfContainers {
				log.Println("StatefulSet '" + res.StatefulSet.Name + "' doesn't have the expected number of containers'")
				return false
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *sidecarContainerSpec) thenStatefulSetStatesShouldHaveContainer(name string, image string, cmd []string, vars []corev1.EnvVar) bool {
	return r.assertStatefulSetsSidecarContainers(name, image, cmd, vars, true)
}

func (r *sidecarContainerSpec) thenStatefulSetStatesShouldNOTHaveContainer(name, image string) bool {
	return r.assertStatefulSetsSidecarContainers(name, image, nil, nil, false)
}

func (r *sidecarContainerSpec) assertStatefulSetsSidecarContainers(name, image string, cmd []string, vars []corev1.EnvVar, wantFound bool) bool {
	return Eventually(func() bool {
		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		if !kubegresResources.AreAllReady {
			return false
		}

		var sidecarsFound int
		for _, resource := range kubegresResources.Resources {
			var containerFound bool
			for _, container := range resource.StatefulSet.Spec.Template.Spec.Containers {
				if container.Name == name && container.Image == image {
					containerFound = slices.Equal(container.Command, cmd) && reflect.DeepEqual(container.Env, vars)
					break
				}
			}
			log.Printf("Sidecar found: %v, expected wantFound: %v, name: %s, image: %s, cmd: %s, env: %s\n",
				containerFound, wantFound, name, image, cmd, vars)
			if containerFound {
				sidecarsFound++
			}
		}

		if wantFound {
			return sidecarsFound == len(kubegresResources.Resources)
		}
		return sidecarsFound == 0

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *sidecarContainerSpec) givenExistingKubegresSpecReplicationSlotsIsSetTo(rs *postgresv1.ReplicationSlots) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()
	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.ReplicationSlots = *rs
}

func (r *sidecarContainerSpec) thenReplicaShouldHaveReplicationSlotSetAndBeReady() {
	Eventually(func() bool {
		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}
		for _, resource := range kubegresResources.Resources {
			if resource.IsPrimary {
				continue
			}
			// have env var with replication slot
			var found bool
			for _, container := range resource.StatefulSet.Spec.Template.Spec.Containers {
				for _, envVar := range container.Env {
					if envVar.Name == ctx.EnvVarReplicationSlotName {
						found = true
						break
					}
				}
			}
			if !found {
				log.Printf("Replica StatefulSet '%s' doesn't have the env var for replication slot", resource.StatefulSet.Name)
				return false
			}
			if !resource.IsReady {
				log.Printf("Replica StatefulSet '%s' is not ready", resource.StatefulSet.Name)
				return false
			}
		}
		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}
