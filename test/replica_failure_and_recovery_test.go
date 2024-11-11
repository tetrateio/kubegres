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
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	postgresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
)

var _ = Describe("Replica instances are not available, checking recovery works", Label("group:2"), func() {

	var test = ReplicaFailureAndRecoveryTest{}

	BeforeEach(func() {
		//Skip("Temporarily skipping test")

		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.connectionPrimaryDb = util.InitDbConnectionDbUtil(test.resourceCreator, resourceConfigs.KubegresResourceName, resourceConfigs.ServiceToSqlQueryPrimaryDbNodePort, true)
		test.connectionReplicaDb = util.InitDbConnectionDbUtil(test.resourceCreator, resourceConfigs.KubegresResourceName, resourceConfigs.ServiceToSqlQueryReplicaDbNodePort, false)
	})

	AfterEach(func() {
		test.resourceCreator.DeleteAllTestResources()
	})

	Context("GIVEN Kubegres with 1 primary and 2 replica AND those 2 replicas are deleted", func() {

		It("THEN the missing 2 replica should be automatically re-created by Kubegres and the existing data replicated", func() {

			log.Print("START OF: Test 'GIVEN Kubegres with 1 primary and 2 replica AND those 2 replicas are deleted'")

			test.givenNewKubegresSpecIsSetTo(3)

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(1, 2)

			expectedNbreUsers := 0

			test.GivenUserAddedInPrimaryDb()
			expectedNbreUsers++

			test.GivenUserAddedInPrimaryDb()
			expectedNbreUsers++

			test.GivenUserAddedInPrimaryDb()
			expectedNbreUsers++

			test.whenAllReplicasStatefulSetAreDeleted(2)

			test.thenPodsStatesShouldBe(1, 2)

			test.ThenPrimaryDbContainsExpectedNbreUsers(expectedNbreUsers)
			test.ThenReplicaDbContainsExpectedNbreUsers(expectedNbreUsers)

			log.Print("END OF: Test 'GIVEN Kubegres with 1 primary and 2 replica AND those 2 replicas are deleted'")
		})
	})
})

type ReplicaFailureAndRecoveryTest struct {
	kubegresResource      *postgresv1.Kubegres
	connectionPrimaryDb   util.DbConnectionDbUtil
	connectionReplicaDb   util.DbConnectionDbUtil
	resourceCreator       util.TestResourceCreator
	resourceRetriever     util.TestResourceRetriever
	customEnvVariableName string
	customEnvVariableKey  string
}

func (r *ReplicaFailureAndRecoveryTest) givenNewKubegresSpecIsSetTo(specNbreReplicas int32) {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *ReplicaFailureAndRecoveryTest) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

func (r *ReplicaFailureAndRecoveryTest) whenAllReplicasStatefulSetAreDeleted(expectedNbreToDelete int) {
	kubegresResources, err := r.resourceRetriever.GetKubegresResources()
	if err != nil {
		Expect(err).Should(Succeed())
		return
	}

	nbreDeleted := 0
	for _, kubegresResource := range kubegresResources.Resources {
		if !kubegresResource.IsPrimary {
			log.Println("Attempting to delete StatefulSet: '" + kubegresResource.StatefulSet.Name + "'")
			if !r.resourceCreator.DeleteResource(kubegresResource.StatefulSet.Resource, kubegresResource.StatefulSet.Name) {
				log.Println("Replica StatefulSet CANNOT BE deleted: '" + kubegresResource.StatefulSet.Name + "'")
			} else {
				nbreDeleted++
				time.Sleep(5 * time.Second)
			}
		}
	}

	Expect(nbreDeleted).Should(Equal(expectedNbreToDelete))
}

func (r *ReplicaFailureAndRecoveryTest) thenPodsStatesShouldBe(nbrePrimary, nbreReplicas int) bool {
	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
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

func (r *ReplicaFailureAndRecoveryTest) GivenUserAddedInPrimaryDb() {
	Eventually(func() bool {
		return r.connectionPrimaryDb.InsertUser()
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *ReplicaFailureAndRecoveryTest) ThenPrimaryDbContainsExpectedNbreUsers(expectedNbreUsers int) {
	Eventually(func() bool {

		users := r.connectionPrimaryDb.GetUsers()
		r.connectionPrimaryDb.Close()

		if len(users) != expectedNbreUsers ||
			r.connectionPrimaryDb.NbreInsertedUsers != expectedNbreUsers {
			log.Println("Primary DB does not contain the expected number of users: " + strconv.Itoa(expectedNbreUsers))
			return false
		}

		return true

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *ReplicaFailureAndRecoveryTest) ThenReplicaDbContainsExpectedNbreUsers(expectedNbreUsers int) {
	Eventually(func() bool {

		users := r.connectionReplicaDb.GetUsers()
		r.connectionReplicaDb.Close()

		if len(users) != expectedNbreUsers ||
			r.connectionReplicaDb.NbreInsertedUsers != expectedNbreUsers {
			log.Println("Replica DB does not contain the expected number of users: " + strconv.Itoa(expectedNbreUsers))
			return false
		}

		return true

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}
