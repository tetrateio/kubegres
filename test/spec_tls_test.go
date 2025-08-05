package test

import (
	"fmt"
	"log"
	"os"
	"path"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/states"
	"reactive-tech.io/kubegres/test/resourceConfigs"
	"reactive-tech.io/kubegres/test/util"
	"reactive-tech.io/kubegres/test/util/testcases"
)

var _ = Describe("Kubegres TLS Spec", Label("tls"), func() {

	var test = TLSTest{}

	BeforeEach(func() {
		namespace := resourceConfigs.DefaultNamespace
		test.resourceRetriever = util.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)

		test.resourceCreator.CreateBackUpPvc()
		test.resourceCreator.CreateTLSSecretEmpty()
		certsSecret := test.resourceCreator.CreateTLSSecretWithValidCerts()
		rootCertPath, clientCertPath, clientKeyPath := test.storeClientTLSCerts(certsSecret)

		primaryBaseConfig := util.WithBaseConfig(test.resourceCreator, resourceConfigs.KubegresResourceName, resourceConfigs.ServiceToSqlQueryPrimaryDbNodePort, true)
		replicaBaseConfig := util.WithBaseConfig(test.resourceCreator, resourceConfigs.KubegresResourceName, resourceConfigs.ServiceToSqlQueryReplicaDbNodePort, false)
		tlsConfig := util.WithSSLConfig("verify-ca", rootCertPath, clientCertPath, clientKeyPath)

		test.secureDBQueryTestCases = testcases.InitDbQueryTestCasesWithConnections(
			util.InitDbConnectionDbUtil(k8sClientTest, primaryBaseConfig, tlsConfig),
			util.InitDbConnectionDbUtil(k8sClientTest, replicaBaseConfig, tlsConfig),
		)

		test.insecureDBQueryTestCases = testcases.InitDbQueryTestCasesWithConnections(
			util.InitDbConnectionDbUtil(k8sClientTest, primaryBaseConfig),
			util.InitDbConnectionDbUtil(k8sClientTest, replicaBaseConfig),
		)

	})

	AfterEach(func() {
		if !test.keepCreatedResourcesForNextTest {
			test.resourceCreator.DeleteAllTestResources()
			_ = os.Remove(test.tmpDir)
		}
		// Reset the field state to ensure each test defines its own state
		test.keepCreatedResourcesForNextTest = false
	})

	Context("GIVEN new Kubegres is created with TLS enabled but no secret defined", func() {
		It("THEN it should fail to create the Kubegres resource", func() {

			log.Println("START OF: Test 'GIVEN new Kubegres is created with TLS enabled but no secret defined'")

			test.givenNewKubegresIsCreatedWithTLS("", 1)

			test.whenKubegresIsCreated()

			test.thenNoSecretDefinedErrorEventShouldBeLogged()

			log.Println("END OF: Test 'GIVEN new Kubegres is created with TLS enabled but no secret defined'")
		})
	})

	Context("GIVEN new Kubegres is created with TLS enabled but secret does not exists", func() {
		It("THEN it should fail to create the Kubegres resource", func() {

			log.Println("START OF: Test 'GIVEN new Kubegres is created with TLS enabled but secret does not exists'")

			test.givenNewKubegresIsCreatedWithTLS("unexisting-tls-secret", 1)

			test.whenKubegresIsCreated()

			test.thenNotExistentErrorEventShouldBeLogged()

			log.Println("END OF: Test 'GIVEN new Kubegres is created with TLS enabled but secret does not exists'")
		})
	})

	Context("GIVEN new Kubegres is created with TLS enabled but secret is empty", func() {
		It("THEN it should fail to create the Kubegres resource", func() {

			log.Println("START OF: Test 'GIVEN new Kubegres is created with TLS enabled but secret is empty'")

			test.givenNewKubegresIsCreatedWithTLS(resourceConfigs.TLSSecretNameEmpty, 1)

			test.whenKubegresIsCreated()

			test.thenMissingKeysErrorEventShouldBeLogged()

			log.Println("END OF: Test 'GIVEN new Kubegres is created with TLS enabled but secret is empty'")
		})
	})

	Context("GIVEN new Kubegres is created with TLS enabled with 1 replica", func() {

		It("THEN it should have the TLS spec set correctly and primary encrypting connections", func() {

			log.Println("START OF: Test 'GIVEN new Kubegres is created with TLS enabled with 1 replica'")

			test.givenNewKubegresIsCreatedWithTLS(resourceConfigs.TLSSecretNameValid, 1)

			test.whenKubegresIsCreated()

			test.thenDeployedKubegresSpecShouldHaveTLS(apiv1.SSLModeVerifyCA)

			test.thenPodsShouldHaveReadyState(1, 0)

			test.thenPodsShouldHaveTLSVolumeMounts()
			test.thenBaseConfigMapShouldHaveTLSKeysAdded()
			test.thenPodsShouldUseTLSConfigMapKeysInVolumeMounts()
			test.thenPodsShouldUseTLSProbes(apiv1.SSLModeVerifyCA)
			test.thenPodsMustUseTLSSecurityContext()

			test.secureDBQueryTestCases.ThenWeCanSqlQueryPrimaryDb()

			log.Println("END OF: Test 'GIVEN new Kubegres is created with TLS enabled with 1 replica'")
		})

	})

	Context("GIVEN new Kubegres is created with TLS enabled with 3 replicas and backup configured", func() {

		It("THEN it should have the TLS spec set correctly and primary encrypting connections and replicas properly replicating "+
			"and backup executing", func() {

			log.Println("START OF: Test 'GIVEN new Kubegres is created with TLS enabled with 3 replicas and backup configured'")

			test.givenNewKubegresIsCreatedWithTLS(resourceConfigs.TLSSecretNameValid, 3)
			test.givenKubegresIsUpdatedWithBackupEnabled(resourceConfigs.BackUpPvcResourceName, "/tmp/my-backup", scheduleBackupEveryMin)

			test.whenKubegresIsCreated()

			test.thenDeployedKubegresSpecShouldHaveTLS(apiv1.SSLModeVerifyCA)
			test.thenNoBlockingOperationShouldBeActive()

			test.thenPodsShouldHaveReadyState(1, 2)

			test.thenPodsShouldHaveTLSVolumeMounts()
			test.thenBaseConfigMapShouldHaveTLSKeysAdded()
			test.thenPodsShouldUseTLSConfigMapKeysInVolumeMounts()
			test.thenPodsShouldUseTLSProbes(apiv1.SSLModeVerifyCA)
			test.thenPodsMustUseTLSSecurityContext()

			test.secureDBQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.secureDBQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.thenCronJobExistsWithTLSConfig(resourceConfigs.BackUpPvcResourceName, "/tmp/my-backup", scheduleBackupEveryMin)
			test.thenCronJobSucceedsAtLeastOnce()

			test.thenNoBlockingOperationShouldBeActive()

			test.keepCreatedResourcesForNextTest = true

			log.Println("END OF: Test 'GIVEN new Kubegres is created with TLS enabled with 3 replicas and backup configured'")

		})

	})

	Context("GIVEN existing Kubegres TLS enabled, 3 replicas and backup configured, AND it is updated to disable TLS", func() {

		It("THEN it should keep working without TLS and backup still working", func() {

			log.Println()
			log.Println()
			log.Println()
			log.Println("START OF: Test 'GIVEN existing Kubegres TLS enabled, 3 replicas and backup configured, AND it is updated to disable TLS'")

			test.givenKubegresIsUpdatedToSetTLS(false)

			test.whenKubegresIsUpdated()

			test.thenDeployedKubegresSpecShouldNOTHaveTLS()
			test.thenNoBlockingOperationShouldBeActive()

			test.thenPodsShouldHaveReadyState(1, 2)

			test.thenPodsShouldNOTHaveTLSVolumeMounts()
			test.thenPodsShouldNOTUseTLSConfigMapKeysInVolumeMounts()
			test.thenPodsShouldNOTUseTLSProbes()

			test.insecureDBQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.insecureDBQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.thenCronJobExistsWithoutTLSConfig(resourceConfigs.BackUpPvcResourceName, "/tmp/my-backup", scheduleBackupEveryMin)
			test.thenCronJobSucceedsAtLeastOnce()

			test.thenNoBlockingOperationShouldBeActive()

			test.keepCreatedResourcesForNextTest = true

			log.Println("END OF: Test 'GIVEN existing Kubegres TLS enabled, 3 replicas and backup configured, AND it is updated to disable TLS'")

		})
	})

	Context("GIVEN existing Kubegres NO TLS enabled, 3 replicas and backup configured, AND it is updated to re-enable TLS", func() {

		It("THEN it should keep working using TLS again and backup still working", func() {

			log.Println("START OF: Test 'GIVEN existing Kubegres NO TLS enabled, 3 replicas and backup configured, AND it is updated to re-enable TLS'")

			test.givenKubegresIsUpdatedToSetTLS(true)

			test.whenKubegresIsUpdated()

			test.thenDeployedKubegresSpecShouldHaveTLS(apiv1.SSLModeVerifyCA)
			test.thenNoBlockingOperationShouldBeActive()

			test.thenPodsShouldHaveReadyState(1, 2)

			test.thenPodsShouldHaveTLSVolumeMounts()
			test.thenBaseConfigMapShouldHaveTLSKeysAdded()
			test.thenPodsShouldUseTLSConfigMapKeysInVolumeMounts()
			test.thenPodsShouldUseTLSProbes(apiv1.SSLModeVerifyCA)
			test.thenPodsMustUseTLSSecurityContext()

			test.secureDBQueryTestCases.ThenWeCanSqlQueryPrimaryDb()
			test.secureDBQueryTestCases.ThenWeCanSqlQueryReplicaDb()

			test.thenCronJobExistsWithTLSConfig(resourceConfigs.BackUpPvcResourceName, "/tmp/my-backup", scheduleBackupEveryMin)
			test.thenCronJobSucceedsAtLeastOnce()

			test.thenNoBlockingOperationShouldBeActive()

			log.Println("END OF: Test 'GIVEN existing Kubegres NO TLS enabled, 3 replicas and backup configured, AND it is updated to re-enable TLS'")

		})

	})

})

type TLSTest struct {
	keepCreatedResourcesForNextTest bool
	kubegresResource                *apiv1.Kubegres
	secureDBQueryTestCases          testcases.DbQueryTestCases
	insecureDBQueryTestCases        testcases.DbQueryTestCases
	resourceCreator                 util.TestResourceCreator
	resourceRetriever               util.TestResourceRetriever
	tmpDir                          string
}

func (t *TLSTest) givenNewKubegresIsCreatedWithTLS(secretName string, replicas int32) {
	t.kubegresResource = resourceConfigs.LoadKubegresYaml()
	t.kubegresResource.Spec.Replicas = &replicas
	t.kubegresResource.Spec.TLS = apiv1.TLS{
		Enabled:    true,
		SecretName: secretName,
		SSLMode:    apiv1.SSLModeVerifyCA,
	}
}

func (t *TLSTest) givenKubegresIsUpdatedWithBackupEnabled(backupPvcName, volumeMount, schedule string) {
	if t.kubegresResource == nil {
		t.kubegresResource = resourceConfigs.LoadKubegresYaml()
	}

	t.kubegresResource.Spec.Backup = apiv1.KubegresBackUp{
		Schedule:    schedule,
		PvcName:     backupPvcName,
		VolumeMount: volumeMount,
	}
}

func (t *TLSTest) givenKubegresIsUpdatedToSetTLS(enable bool) {
	if t.kubegresResource == nil {
		t.kubegresResource = resourceConfigs.LoadKubegresYaml()
	}

	t.kubegresResource.Spec.TLS.Enabled = enable
}

func (t *TLSTest) whenKubegresIsCreated() {
	t.resourceCreator.CreateKubegres(t.kubegresResource)
}

func (t *TLSTest) whenKubegresIsUpdated() {
	kubegres, err := t.resourceRetriever.GetKubegresByName(t.kubegresResource.Name)
	Expect(err).To(Succeed(), "Failed to retrieve Kubegres resource for update")
	t.kubegresResource.ResourceVersion = kubegres.ResourceVersion
	t.resourceCreator.UpdateResource(t.kubegresResource, t.kubegresResource.Name)
}

func (t *TLSTest) thenDeployedKubegresSpecShouldHaveTLS(sslMode apiv1.SSLMode) {
	Eventually(func() bool {
		kubegres, err := t.resourceRetriever.GetKubegres()
		if err != nil {
			log.Println("Error retrieving Kubegres resource:", err)
			return false
		}

		wantTLS := apiv1.TLS{
			Enabled:        true,
			SecretName:     resourceConfigs.TLSSecretNameValid,
			RootCertPath:   ctx.DefaultTLSRootCertPath,
			ServerCertPath: ctx.DefaultTLSServerCertPath,
			ServerKeyPath:  ctx.DefaultTLSServerKeyPath,
			ClientCertPath: ctx.DefaultTLSClientCertPath,
			ClientKeyPath:  ctx.DefaultTLSClientKeyPath,
			MountPath:      ctx.DefaultTLSMountPath,
			SSLMode:        sslMode,
		}

		if kubegres.Spec.TLS != wantTLS {
			log.Println("Kubegres TLS spec does not match expected:", kubegres.Spec.TLS)
			return false
		}
		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenDeployedKubegresSpecShouldNOTHaveTLS() {
	Eventually(func() bool {
		kubegres, err := t.resourceRetriever.GetKubegres()
		if err != nil {
			log.Println("Error retrieving Kubegres resource:", err)
			return false
		}

		if kubegres.Spec.TLS.Enabled {
			log.Println("Kubegres TLS spec should not be enabled, but it is:", kubegres.Spec.TLS)
			return false
		}
		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenPodsShouldHaveReadyState(numPrimary, numReplicas int) {
	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving Kubegres resources:", err)
			return false
		}

		if !resources.AreAllReady {
			return false
		}

		if resources.NbreDeployedPrimary != numPrimary || resources.NbreDeployedReplicas != numReplicas {
			log.Printf("Expected %d primary and %d replicas, but got %d primary and %d replicas",
				numPrimary, numReplicas, resources.NbreDeployedPrimary, resources.NbreDeployedReplicas)
			return false
		}
		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenPodsShouldHaveTLSVolumeMounts() {
	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving primary StatefulSet:", err)
			return false
		}

		for _, r := range resources.Resources {
			var match bool
			for _, v := range r.Pod.Spec.Volumes {
				if v.Name == ctx.TLSVolumeName &&
					v.VolumeSource.Secret != nil &&
					v.VolumeSource.Secret.SecretName == resourceConfigs.TLSSecretNameValid &&
					v.VolumeSource.Secret.DefaultMode != nil &&
					*v.VolumeSource.Secret.DefaultMode == ctx.DefaultTLSVolumeMode {
					match = true
				}
			}
			if !match {
				log.Println("TLS volume not found in pod spec for resource:", r.Pod.Name, "want name:", ctx.TLSVolumeName,
					"want secret name:", resourceConfigs.TLSSecretNameValid, "want default mode:", ctx.DefaultTLSVolumeMode)
				return false
			}

			match = false
			for _, c := range r.Pod.Spec.Containers {
				for _, vm := range c.VolumeMounts {
					if vm.Name == ctx.TLSVolumeName && vm.MountPath == ctx.DefaultTLSMountPath && vm.ReadOnly {
						match = true
					}
				}
				if !match {
					log.Println("TLS volume mount not found in container spec for pod:", r.Pod.Name, "contianer name:", c.Name,
						"want name:", ctx.TLSVolumeName, "want mount path:", ctx.DefaultTLSMountPath, "want readOnly: true")
					return false
				}
			}

			match = false
			for _, c := range r.Pod.Spec.InitContainers {
				for _, vm := range c.VolumeMounts {
					if vm.Name == ctx.TLSVolumeName && vm.MountPath == ctx.DefaultTLSMountPath && vm.ReadOnly {
						match = true
					}
				}
				if !match {
					log.Println("TLS volume mount not found in init container spec for pod:", r.Pod.Name, "init container name:", c.Name,
						"want name:", ctx.TLSVolumeName, "want mount path:", ctx.DefaultTLSMountPath, "want readOnly: true")
					return false
				}
			}

		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenPodsShouldNOTHaveTLSVolumeMounts() {
	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving primary StatefulSet:", err)
			return false
		}

		for _, r := range resources.Resources {
			for _, v := range r.Pod.Spec.Volumes {
				if v.Name == ctx.TLSVolumeName {
					log.Println("TLS volume found in pod spec for resource:", r.Pod.Name, "but it should not be present")
					return false
				}
			}

			for _, c := range r.Pod.Spec.Containers {
				for _, vm := range c.VolumeMounts {
					if vm.Name == ctx.TLSVolumeName {
						log.Println("TLS volume mount found in container spec for pod:", r.Pod.Name, "container name:", c.Name,
							"but it should not be present")
						return false
					}
				}
			}

			for _, c := range r.Pod.Spec.InitContainers {
				for _, vm := range c.VolumeMounts {
					if vm.Name == ctx.TLSVolumeName {
						log.Println("TLS volume mount found in init container spec for pod:", r.Pod.Name, "init container name:", c.Name,
							"but it should not be present")
						return false
					}
				}
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenBaseConfigMapShouldHaveTLSKeysAdded() {
	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving kubegres resources:", err)
			return false
		}

		for _, key := range states.TLSConfigKeyReplacements {
			if _, ok := resources.BaseConfigMap.Data[key.ReplacementKey]; !ok {
				log.Printf("TLS config map key %s not found in base config map", key.ReplacementKey)
				return false
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenPodsShouldUseTLSConfigMapKeysInVolumeMounts() {
	wantMainContainerKeys := []string{
		states.ConfigMapDataKeyTLSPostgresConf,
		states.ConfigMapDataKeyTLSPgHbaConf,
	}
	wantReplicaInitContainerKeys := []string{
		states.ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript,
	}

	volumeMountsContainsAllKeys := func(wantKeys []string, volumeMounts []corev1.VolumeMount) (bool, string) {
		for _, want := range wantKeys {
			var keyFound bool
			for _, vm := range volumeMounts {
				if vm.Name == ctx.BaseConfigMapVolumeName && vm.SubPath == want {
					keyFound = true
					break
				}
			}
			if !keyFound {
				return false, want
			}
		}
		return true, ""
	}

	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving kubegres resources:", err)
			return false
		}

		for _, r := range resources.Resources {
			c := r.Pod.Spec.Containers[0]
			if ok, key := volumeMountsContainsAllKeys(wantMainContainerKeys, c.VolumeMounts); !ok {
				fmt.Printf("TLS config map key %s not found in volume mounts for pod: %s, container: %s\n", key, r.Pod.Name, c.Name)
				return false
			}

			if r.IsPrimary {
				// we don't expect the primary init container to have any TLS config map keys
				continue
			}

			for _, c := range r.Pod.Spec.InitContainers {
				if ok, key := volumeMountsContainsAllKeys(wantReplicaInitContainerKeys, c.VolumeMounts); !ok {
					fmt.Printf("TLS config map key %s not found in volume mounts for pod: %s, init container: %s\n", key, r.Pod.Name, c.Name)
					return false
				}
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenPodsShouldNOTUseTLSConfigMapKeysInVolumeMounts() {
	volumeMountsContainsAnyKey := func(volumeMounts []corev1.VolumeMount) (bool, string) {
		for _, notWant := range []string{
			states.ConfigMapDataKeyTLSPostgresConf,
			states.ConfigMapDataKeyTLSPgHbaConf,
			states.ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript,
		} {
			for _, vm := range volumeMounts {
				if vm.Name == ctx.BaseConfigMapVolumeName && vm.SubPath == notWant {
					return true, notWant
				}
			}
		}
		return false, ""
	}

	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving kubegres resources:", err)
			return false
		}

		for _, r := range resources.Resources {
			c := r.Pod.Spec.Containers[0]
			if ok, key := volumeMountsContainsAnyKey(c.VolumeMounts); ok {
				fmt.Printf("TLS config map key %s should not be found in volume mounts for pod: %s, container: %s\n", key, r.Pod.Name, c.Name)
				return false
			}

			if r.IsPrimary {
				continue // we don't expect the primary init container to have any TLS config map keys
			}

			for _, c := range r.Pod.Spec.InitContainers {
				if ok, key := volumeMountsContainsAnyKey(c.VolumeMounts); ok {
					fmt.Printf("TLS config map key %s should not be found in volume mounts for pod: %s, init container: %s\n", key, r.Pod.Name, c.Name)
					return false
				}
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenPodsShouldUseTLSProbes(sslMode apiv1.SSLMode) {
	wantCommand := []string{
		"sh",
		"-c",
		fmt.Sprintf("PGPASSWORD=$POSTGRES_PASSWORD psql \"sslmode=%[4]s "+
			"sslrootcert=%[1]s sslcert=%[2]s sslkey=%[3]s "+
			"host=$POD_IP user=postgres\" -c \"SELECT 1\"",
			ctx.DefaultTLSRootCertPath, ctx.DefaultTLSClientCertPath, ctx.DefaultTLSClientKeyPath, sslMode),
	}
	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving kubegres resources:", err)
			return false
		}

		for _, r := range resources.Resources {
			c := r.Pod.Spec.Containers[0]
			if c.LivenessProbe == nil || c.ReadinessProbe == nil {
				log.Printf("Liveness or Readiness probe not set for pod: %s, container: %s", r.Pod.Name, c.Name)
				return false
			}

			if !slices.Equal(c.LivenessProbe.Exec.Command, wantCommand) {
				log.Printf("Liveness probe command not matching for pod: %s, container: %s.\nwant: %v, got: %v"+r.Pod.Name, c.Name,
					wantCommand, c.LivenessProbe.Exec.Command)
				return false
			}

			if !slices.Equal(c.ReadinessProbe.Exec.Command, wantCommand) {
				log.Printf("Readiness probe command not matching for pod: %s, container: %s.\nwant: %v, got: %v"+r.Pod.Name, c.Name, c.ReadinessProbe.Exec.Command,
					wantCommand, c.ReadinessProbe.Exec.Command)
				return false
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenPodsShouldNOTUseTLSProbes() {
	wantCommand := []string{
		"sh",
		"-c",
		"exec pg_isready -U postgres -h $POD_IP",
	}
	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving kubegres resources:", err)
			return false
		}

		for _, r := range resources.Resources {
			c := r.Pod.Spec.Containers[0]
			if c.LivenessProbe == nil || c.ReadinessProbe == nil {
				log.Printf("Liveness or Readiness probe not set for pod: %s, container: %s", r.Pod.Name, c.Name)
				return false
			}

			if !slices.Equal(c.LivenessProbe.Exec.Command, wantCommand) {
				log.Printf("Liveness probe command should match for pod: %s, container: %s.\nwant: %v, got: %v",
					r.Pod.Name, c.Name, wantCommand, c.LivenessProbe.Exec.Command)
				return false
			}

			if !slices.Equal(c.ReadinessProbe.Exec.Command, wantCommand) {
				log.Printf("Readiness probe command should match for pod: %s, container: %s.\nwant: %v, got: %v",
					r.Pod.Name, c.Name, wantCommand, c.ReadinessProbe.Exec.Command)
				return false
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenCronJobExistsWithTLSConfig(wantBackupPvcName, wantVolumeMount, wantSchedule string) {
	Eventually(func() bool {
		kubegresResources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		backUpCronJob := kubegresResources.BackUpCronJob
		if backUpCronJob.Name == "" {
			return false
		}

		if wantSchedule != backUpCronJob.Spec.Schedule {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected schedule: '" + wantSchedule + "'. Waiting...")
			return false
		}

		if wantBackupPvcName != backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected PVC with name: '" + wantBackupPvcName + "'. Waiting...")
			return false
		}

		currentMountPath := backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath
		if wantVolumeMount != currentMountPath {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected volume mount: '" + wantVolumeMount + "'.Current: " + currentMountPath + " Waiting...")
			return false
		}

		if states.ConfigMapDataKeyTLSBackupDatabaseScript != backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].VolumeMounts[1].SubPath {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected volume mount: '" + states.ConfigMapDataKeyTLSBackupDatabaseScript + "'. Waiting...")
			return false
		}

		if len(backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes) < 3 {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the TLS volume. Waiting...")
			return false
		}

		currentTLSVolume := backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes[2]
		if currentTLSVolume.Name != ctx.TLSVolumeName ||
			currentTLSVolume.VolumeSource.Secret == nil ||
			currentTLSVolume.VolumeSource.Secret.SecretName != t.kubegresResource.Spec.TLS.SecretName ||
			currentTLSVolume.VolumeSource.Secret.DefaultMode == nil ||
			*currentTLSVolume.VolumeSource.Secret.DefaultMode != ctx.DefaultTLSVolumeMode {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected TLS volume. Waiting...")
			return false
		}

		if len(backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].VolumeMounts) < 3 {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the TLS volume mount. Waiting...")
			return false
		}

		currentTLSVolumeMount := backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].VolumeMounts[2]
		if currentTLSVolumeMount.Name != ctx.TLSVolumeName ||
			currentTLSVolumeMount.MountPath != t.kubegresResource.Spec.TLS.MountPath ||
			!currentTLSVolumeMount.ReadOnly {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected TLS volume mount. Waiting...")
		}

		return true

	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenCronJobExistsWithoutTLSConfig(wantBackupPvcName, wantVolumeMount, wantSchedule string) {
	Eventually(func() bool {
		kubegresResources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres resources")
			return false
		}

		backUpCronJob := kubegresResources.BackUpCronJob
		if backUpCronJob.Name == "" {
			return false
		}

		if wantSchedule != backUpCronJob.Spec.Schedule {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected schedule: '" + wantSchedule + "'. Waiting...")
			return false
		}

		if wantBackupPvcName != backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected PVC with name: '" + wantBackupPvcName + "'. Waiting...")
			return false
		}

		if wantVolumeMount != backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected volume mount: '" + wantVolumeMount + "'. Waiting...")
			return false
		}

		if states.ConfigMapDataKeyBackUpScript != backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].VolumeMounts[1].SubPath {
			log.Println("CronJob '" + backUpCronJob.Name + "' doesn't have the expected volume mount: '" + states.ConfigMapDataKeyBackUpScript + "'. Waiting...")
			return false
		}

		for _, volume := range backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes {
			if volume.Name == ctx.TLSVolumeName {
				log.Println("CronJob '" + backUpCronJob.Name + "' must not have the TLS volume. Waiting...")
				return false
			}
		}

		for _, vm := range backUpCronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].VolumeMounts {
			if vm.Name == ctx.TLSVolumeName {
				log.Println("CronJob '" + backUpCronJob.Name + "' must not have the TLS volume mount. Waiting...")
				return false
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenPodsMustUseTLSSecurityContext() {
	var (
		wantRunAsNonRoot = true
		wantRunAsUser    = int64(999) // PostgreSQL default user ID
		wantFSGroup      = int64(999) // PostgreSQL default group ID
	)

	Eventually(func() bool {
		resources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil {
			log.Println("Error retrieving kubegres resources:", err)
			return false
		}

		for _, r := range resources.Resources {
			got := r.Pod.Spec.SecurityContext
			if got == nil ||
				got.RunAsNonRoot == nil || *got.RunAsNonRoot != wantRunAsNonRoot ||
				got.RunAsUser == nil || *got.RunAsUser != wantRunAsUser ||
				got.FSGroup == nil || *got.FSGroup != wantFSGroup {
				log.Printf("Security context for pod %s does not match expected:\n"+
					"want: RunAsNonRoot=%t, RunAsUser=%d, FSGroup=%d\n"+
					"got: %+v",
					r.Pod.Name, wantRunAsNonRoot, wantRunAsUser, wantFSGroup, got)
				return false
			}
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenCronJobSucceedsAtLeastOnce() {
	Eventually(func() bool {
		kubegresResources, err := t.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		backUpCronJob := kubegresResources.BackUpCronJob
		if backUpCronJob.Name == "" {
			log.Println("No BackUp CronJob found")
			return false
		}

		if backUpCronJob.Status.LastScheduleTime == nil {
			log.Println("BackUp CronJob '" + backUpCronJob.Name + "' has not been scheduled yet. Waiting...")
			return false
		}

		if backUpCronJob.Status.LastSuccessfulTime == nil {
			log.Println("BackUp CronJob '" + backUpCronJob.Name + "' has not succeeded yet. Waiting...")
			return false
		}

		return true
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) eventuallyErrorEventShouldBeLogged(event util.EventRecord) {
	Eventually(func() bool {
		_, err := t.resourceRetriever.GetKubegres()
		if err != nil {
			return false
		}
		return eventRecorderTest.CheckEventExist(event)
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
}

func (t *TLSTest) thenNoSecretDefinedErrorEventShouldBeLogged() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: corev1.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message: "In the Resources Spec the value of 'spec.tls.enabled' is true but 'spec.tls.secretName' " +
			"has an empty secret name. Please set a valid secret name, otherwise this operator cannot work correctly.",
	}

	t.eventuallyErrorEventShouldBeLogged(expectedErrorEvent)
}

func (t *TLSTest) thenNotExistentErrorEventShouldBeLogged() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: corev1.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message: "In the Resources Spec the value of 'spec.tls.secretName' has a secret name which is not deployed. " +
			"Please deploy this secret, otherwise this operator cannot work correctly.",
	}

	t.eventuallyErrorEventShouldBeLogged(expectedErrorEvent)

}

func (t *TLSTest) thenMissingKeysErrorEventShouldBeLogged() {
	expectedErrorEvent := util.EventRecord{
		Eventtype: corev1.EventTypeWarning,
		Reason:    "SpecCheckErr",
		Message: "In the Resources Spec the value of 'spec.tls' has a secret name which does not have all the required keys. " +
			"Please deploy this secret with all the required keys or change the spec.tls, otherwise this operator cannot work correctly.",
	}

	t.eventuallyErrorEventShouldBeLogged(expectedErrorEvent)
}

func (t *TLSTest) thenNoBlockingOperationShouldBeActive() {
	// we want consecutive successes to ensure we are not considering transitions between blocking operations
	var successes int
	Eventually(func() bool {
		kubegres, err := t.resourceRetriever.GetKubegres()
		if err != nil {
			log.Println("Error retrieving Kubegres resource:", err)
			successes = 0
			return false
		}

		op := kubegres.Status.BlockingOperation
		if op.OperationId != "" {
			log.Printf("Blocking operation is still active: %s (%s)", op.OperationId, op.StepId)
			successes = 0
			return false
		}

		successes++
		return successes >= 3
	}, resourceConfigs.TestTimeout, resourceConfigs.TestRetryInterval).Should(BeTrue())
	log.Println("No blocking operation is active, checked 3 times in a row successfully")

}

func (t *TLSTest) storeClientTLSCerts(secret *corev1.Secret) (string, string, string) {
	t.tmpDir = path.Join(os.TempDir(), "kubegres-tls-test")
	err := os.MkdirAll(t.tmpDir, 0755)
	Expect(err).Should(Succeed(), "Failed to create temporary directory %s", t.tmpDir)

	for _, key := range []string{
		path.Base(ctx.DefaultTLSRootCertPath),
		path.Base(ctx.DefaultTLSServerCertPath),
		path.Base(ctx.DefaultTLSServerKeyPath),
		path.Base(ctx.DefaultTLSClientCertPath),
		path.Base(ctx.DefaultTLSClientKeyPath),
	} {
		data, ok := secret.Data[key]
		Expect(ok).Should(BeTrue(), "Key %s not found in TLS secret %s", key, resourceConfigs.TLSSecretNameValid)
		err = os.WriteFile(path.Join(t.tmpDir, key), data, 0600)
		Expect(err).Should(Succeed(), "Failed to write %s to temp directory %s", key, t.tmpDir)
	}

	return path.Join(t.tmpDir, path.Base(ctx.DefaultTLSRootCertPath)),
		path.Join(t.tmpDir, path.Base(ctx.DefaultTLSClientCertPath)),
		path.Join(t.tmpDir, path.Base(ctx.DefaultTLSClientKeyPath))
}
