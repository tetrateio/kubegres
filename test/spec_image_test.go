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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v12 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	postgresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/testcases"
)

var _ = Describe("Setting Kubegres spec 'image'", Label("group:4"), func() {

	var test = SpecImageTest{}

	BeforeEach(func() {
		//Skip("Temporarily skipping test")

		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.dbQueryTestCases = testcases.InitDbQueryTestCases(test.resourceCreator, resourceConfigs.KubegresResourceName)
	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
		} else {
			test.keepCreatedResourcesForNextTest = false
		}
	})

	Context("GIVEN new Kubegres is created without spec 'image'", func() {

		It("THEN An error event should be logged", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created without spec 'image''")

			test.givenNewKubegresSpecIsSetTo("", 3)

			test.whenKubegresIsCreated()

			test.thenErrorEventShouldBeLogged()

			log.Print("END OF: Test 'GIVEN new Kubegres is created without spec 'image''")
		})
	})

	Context("GIVEN new Kubegres is created with spec 'image' set to 'postgres:14.4' and spec 'replica' set to 3 and later 'image' is updated to 'postgres:14.5'", func() {

		It("GIVEN new Kubegres is created with spec 'image' set to 'postgres:14.4' and spec 'replica' set to 3 THEN 1 primary and 2 replica should be created with spec 'image' set to 'postgres:14.5'", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'image' set to 'postgres:14.4' and spec 'replica' set to 3")

			test.givenNewKubegresSpecIsSetTo("postgres:14.4", 3)

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe("postgres:14.4", 1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo("postgres:14.4")

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'image' set to 'postgres:14.4' and spec 'replica' set to 3'")
		})

		It("GIVEN existing Kubegres is updated with spec 'image' set from 'postgres:14.4' to 'postgres:14.5' THEN 1 primary and 2 replica should be re-deployed with spec 'image' set to 'postgres:14.5'", func() {

			log.Print("START OF: Test 'GIVEN existing Kubegres is updated with spec 'image' set from 'postgres:14.4' to 'postgres:14.5'")

			test.givenExistingKubegresSpecIsSetTo("postgres:14.5")

			test.whenKubernetesIsUpdated()

			test.thenPodsStatesShouldBe("postgres:14.5", 1, 2)

			test.thenDeployedKubegresSpecShouldBeSetTo("postgres:14.5")

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			log.Print("END OF: Test 'GIVEN existing Kubegres is updated with spec 'image' set from 'postgres:14.4' to 'postgres:14.5'")
		})

	})

})

type SpecImageTest struct {
	keepCreatedResourcesForNextTest bool
	kubegresResource                *postgresv1.Kubegres
	dbQueryTestCases                testcases.DbQueryTestCases
	resourceCreator                 util.TestResourceCreator
	resourceRetriever               util.TestResourceRetriever
}

func (r *SpecImageTest) givenNewKubegresSpecIsSetTo(image string, specNbreReplicas int32) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Image = image
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *SpecImageTest) givenExistingKubegresSpecIsSetTo(image string) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.Image = image
}

func (r *SpecImageTest) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

func (r *SpecImageTest) whenKubernetesIsUpdated() {
	r.resourceCreator.UpdateResource(r.kubegresResource, "Kubegres")
}

func (r *SpecImageTest) thenErrorEventShouldBeLogged() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: v12.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message:   "In the Resources Spec the value of 'spec.image' is undefined. Please set a value otherwise this operator cannot work correctly.",
	}
	Eventually(func() bool {
		_, err := r.resourceRetriever.GetKubegres()
		if err != nil {
			return false
		}
		return eventRecorderTest.CheckEventExist(expectedErrorEvent)

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecImageTest) thenPodsStatesShouldBe(image string, nbrePrimary, nbreReplicas int) bool {
	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		for _, resource := range kubegresResources.Resources {
			currentImage := resource.Pod.Spec.Containers[0].Image
			if currentImage != image {
				log.Println("Pod '" + resource.Pod.Name + "' doesn't have the expected image: '" + image + "'. " +
					"Current value: '" + currentImage + "'. Waiting...")
				return false
			}
		}

		if kubegresResources.AreAllReady &&
			kubegresResources.NbreDeployedPrimary == nbrePrimary &&
			kubegresResources.NbreDeployedReplicas == nbreReplicas {

			time.Sleep(resourceConfigs.TestRetryInterval)
			log.Println("Deployed and Ready StatefulSets check successful")
			return true
		}

		return false

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *SpecImageTest) thenDeployedKubegresSpecShouldBeSetTo(image string) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	Expect(r.kubegresResource.Spec.Image).Should(Equal(image))
}
