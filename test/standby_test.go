package test

import (
	"log"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v12 "k8s.io/api/core/v1"
	"reactive-tech.io/kubegres/controllers/ctx"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	postgresv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/testcases"
)

var _ = Describe("Setting Kubegres spec 'replica'", Label("group:5"), Label("standby"), func() {

	var test = StandByTest{}

	BeforeEach(func() {
		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
		test.dbQueryTestCases = testcases.InitDbQueryTestCasesWithConnections(
			util.InitExternalDbConnectionDbUtil(test.resourceCreator, resourceConfigs.ServiceToSqlQueryExternalDbNodePort),
			util.InitDbConnectionDbUtil(test.resourceCreator, resourceConfigs.KubegresResourceName, resourceConfigs.ServiceToSqlQueryReplicaDbNodePort, false),
		)
	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
		} else {
			test.keepCreatedResourcesForNextTest = false
		}
	})

	Context("GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to empty", func() {

		It("THEN a validation error event should be logged", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to empty")

			test.givenNewKubegresSpecIsStandbySetToTrue()

			test.whenKubegresIsCreated()

			test.thenErrorEventShouldBeLogged()

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to empty")
		})
	})

	Context("GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to external postgres endpoint", func() {

		It("THEN replica set to 1 should be running and replicating data from external postgres ", func() {

			log.Print("START OF: Test 'GIVEN replica set to 1 should be running and replicating data from external postgres")

			test.givenNewExternalPostgresIsCreatedAndReady()

			test.givenNewKubegresSpecIsStandbySetToTrueAndPrimaryEndpointSetToExternalPostgres()

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(0, 1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'replica set to 1 should be running and replicating data from external postgres")
		})

		It("THEN replica set to 2 should be running and replicating data from external postgres ", func() {

			log.Print("START OF: Test 'GIVEN replica set to 2 should be running and replicating data from external postgres")

			test.givenExistingKubegresSpecIsSetTo(2)

			test.whenKubegresIsUpdated()

			test.thenPodsStatesShouldBe(0, 2)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.keepCreatedResourcesForNextTest = true

			log.Print("END OF: Test 'GIVEN replica set to 2 should be running and replicating data from external postgres")
		})

		It("THEN replica set from 2 to 1 should be running and replicating data from external postgres ", func() {

			log.Print("START OF: Test 'GIVEN replica set from 2 to 1 should be running and replicating data from external postgres")

			test.givenExistingKubegresSpecIsSetTo(1)

			test.whenKubegresIsUpdated()

			test.thenPodsStatesShouldBe(0, 1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			log.Print("END OF: Test 'GIVEN replica set from 2 to 1 should be running and replicating data from external postgres")
		})

	})

	Context("GIVEN new Kubegres is created with spec 'standby.enabled' set to true and 'standby.primaryEndpoint' set to external postgres endpoint and"+
		" 'backup.schedule' AND 'backup.volumeMount' AND 'backup.pvcName' and the given PVC is deployed", func() {

		It("THEN backup CronJob is created AND 1 replica should be replicating from external postgres", func() {

			log.Print("START OF: Test 'GIVEN new Kubegres is created with spec 'backup.schedule' AND 'backup.volumeMount' AND 'backup.pvcName' and the given PVC is deployed")

			test.givenNewExternalPostgresIsCreatedAndReady()
			test.givenBackupPvcIsCreated()

			test.givenNewKubegresSpecIsStandbySetToTrueAndPrimaryEndpointSetToExternalPostgres()
			test.givenKubegresSpecIsSetToBackup(scheduleBackupEveryMin, resourceConfigs.BackUpPvcResourceName, "/tmp/my-kubegres", 3)

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(0, 1)

			test.dbQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.dbQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.thenCronJobExistsWithSpec(ctx.BaseConfigMapName, scheduleBackupEveryMin, resourceConfigs.BackUpPvcResourceName, "/tmp/my-kubegres")

			log.Print("END OF: Test 'GIVEN new Kubegres is created with spec 'backup.schedule' AND 'backup.volumeMount' AND 'backup.pvcName' and the given PVC is deployed")
		})
	})

})

type StandByTest struct {
	keepCreatedResourcesForNextTest bool
	kubegresResource                *postgresv1.Kubegres
	dbQueryTestCases                testcases.DbQueryTestCases
	resourceCreator                 util.TestResourceCreator
	resourceRetriever               util.TestResourceRetriever
}

func (r *StandByTest) givenNewKubegresSpecIsStandbySetToTrue() {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Standby.Enabled = true
}

func (r *StandByTest) givenNewKubegresSpecIsStandbySetToTrueAndPrimaryEndpointSetToExternalPostgres() {
	r.kubegresResource = resourceConfigs.LoadKubegresYaml()
	r.kubegresResource.Spec.Standby.Enabled = true
	r.kubegresResource.Spec.Standby.PrimaryEndpoint = "external-postgres"
	replicas := int32(1)
	r.kubegresResource.Spec.Replicas = &replicas
}

func (r *StandByTest) givenNewExternalPostgresIsCreatedAndReady() {
	r.resourceCreator.CreateExternalPostgres()

	for {
		select {
		case <-time.After(time.Second):
			statefulSet, err := r.resourceRetriever.GetStatefulSet(resourceConfigs.StatefulSetExternalDbResourceName)
			if err != nil {
				log.Println("Error while getting StatefulSet resource : ", err)
				continue
			}
			if statefulSet.Status.AvailableReplicas == 1 {
				return
			}
		case <-time.After(resourceConfigs.TestTimeout):
			log.Println("Timeout while waiting for StatefulSet to be ready")
			return
		}
	}
}
func (r *StandByTest) givenBackupPvcIsCreated() {
	r.resourceCreator.CreateBackUpPvc()
}

func (r *StandByTest) givenExistingKubegresSpecIsSetTo(specNbreReplicas int32) {
	var err error
	r.kubegresResource, err = r.resourceRetriever.GetKubegres()

	if err != nil {
		log.Println("Error while getting Kubegres resource : ", err)
		Expect(err).Should(Succeed())
		return
	}

	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *StandByTest) givenKubegresSpecIsSetToBackup(backupSchedule, backupPvcName, backupVolumeMount string, specNbreReplicas int32) {
	if backupSchedule != "" {
		r.kubegresResource.Spec.Backup.Schedule = backupSchedule
		r.kubegresResource.Spec.Backup.PvcName = backupPvcName
		r.kubegresResource.Spec.Backup.VolumeMount = backupVolumeMount
	}
}

func (r *StandByTest) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

func (r *StandByTest) whenKubegresIsUpdated() {
	r.resourceCreator.UpdateResource(r.kubegresResource, "Kubegres")
}
func (r *StandByTest) thenErrorEventShouldBeLogged() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: v12.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message:   "In the Resources Spec the value of 'spec.standby.primaryEndpoint' is undefined. Please set a value otherwise this operator cannot work correctly.",
	}
	Eventually(func() bool {
		_, err := r.resourceRetriever.GetKubegres()
		if err != nil {
			return false
		}
		return eventRecorderTest.CheckEventExist(expectedErrorEvent)

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *StandByTest) thenPodsStatesShouldBe(nbrePrimary, nbreReplicas int) bool {
	return Eventually(func() bool {

		pods, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres pods")
			return false
		}

		if pods.AreAllReady &&
			pods.NbreDeployedPrimary == nbrePrimary &&
			pods.NbreDeployedReplicas == nbreReplicas {

			time.Sleep(resourceConfigs.TestRetryInterval)
			log.Println("Deployed and Ready StatefulSets check successful")
			return true
		}

		return false

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (r *StandByTest) thenCronJobExistsWithSpec(expectedConfigMapName,
	expectedBackupSchedule,
	expectedBackupPvcName,
	expectedBackupVolumeMount string) bool {

	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		backUpCronJob := kubegresResources.BackUpCronJob
		if backUpCronJob.Name == "" {
			return false
		}

		cronJobConfigMapName := backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes[1].ConfigMap.Name
		if expectedConfigMapName != cronJobConfigMapName {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected configMap name: '" + expectedConfigMapName + "'. Waiting...")
			return false
		}

		cronJobSchedule := backUpCronJob.Spec.Schedule
		if expectedBackupSchedule != cronJobSchedule {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected schedule: '" + expectedBackupSchedule + "'. Waiting...")
			return false
		}

		cronJobPvcName := backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
		if expectedBackupPvcName != cronJobPvcName {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected PVC with name: '" + expectedBackupPvcName + "'. Waiting...")
			return false
		}

		cronJobVolumeMount := backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath
		if expectedBackupVolumeMount != cronJobVolumeMount {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected volume mount: '" + expectedBackupVolumeMount + "'. Waiting...")
			return false
		}

		cronJobDBSource := backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Env[3].Value
		extpectedDBSource := r.kubegresResource.Name + "-replica"
		if extpectedDBSource != cronJobDBSource {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected DB source: '" + extpectedDBSource + "'. Waiting...")
			return false
		}

		return true

	}, time.Second*10, time.Second*5).Should(BeTrue())
}
