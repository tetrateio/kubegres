package template

import (
	"fmt"
	"strings"

	"github.com/golang/protobuf/proto"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiv1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/states"
)

type TLSConfigSpecHelper struct {
	kubegresContext ctx.KubegresContext
}

func CreateTLSConfigSpecHelper(kubegresContext ctx.KubegresContext) TLSConfigSpecHelper {
	return TLSConfigSpecHelper{kubegresContext: kubegresContext}
}

func TLSVolume(tls apiv1.TLS) corev1.Volume {
	defaultMode := ctx.DefaultTLSVolumeMode
	return corev1.Volume{
		Name: ctx.TLSVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  tls.SecretName,
				DefaultMode: &defaultMode,
			},
		},
	}
}

func TLSVolumeMount(tls apiv1.TLS) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      ctx.TLSVolumeName,
		MountPath: tls.MountPath,
		ReadOnly:  true,
	}
}

func (t *TLSConfigSpecHelper) ConfigureStatefulSet(statefulSet *apps.StatefulSet) (string, string, bool) {
	var (
		expected, current []string
		modified          bool
	)

	type configureFunc func(*apps.StatefulSet) (string, string, bool)
	for _, f := range []configureFunc{
		t.ConfigureSecurityContext,
		t.ConfigureLivenessProbe,
		t.ConfigureReadinessProbe,
		t.ConfigureTLSEnvVars,
		t.ConfigureTLSCertsVolume,
		t.ConfigureVolumeMounts,
	} {
		if exp, cur, ok := f(statefulSet); ok {
			expected = append(expected, exp)
			current = append(current, cur)
			modified = true
		}
	}

	if modified {
		return strings.Join(expected, ";\n - "), strings.Join(current, ";\n - "), true
	}
	return "", "", false
}

func (t *TLSConfigSpecHelper) ConfigureVolumeMounts(statefulSet *apps.StatefulSet) (string, string, bool) {
	tlsEnabled := t.kubegresContext.Kubegres.Spec.TLS.Enabled

	applyCorrespondingSubpath := func(c *corev1.Container, replacement states.TLSConfigKeyReplacement) (string, string, bool) {
		var (
			modified          bool
			expected, current []string
		)
		useReplacementKey := replacement.MatchCondition(t.kubegresContext.Kubegres.Spec)
		for i := 0; i < len(c.VolumeMounts); i++ {
			volumeMount := &c.VolumeMounts[i]
			if volumeMount.Name != ctx.BaseConfigMapVolumeName {
				continue
			}

			switch {
			case !tlsEnabled && volumeMount.SubPath == replacement.ReplacementKey:
				// in case of TLS disabled, make sure volumeMounts use the original key
				volumeMount.SubPath = replacement.OriginalKey
				modified = true
				expected = append(expected, fmt.Sprintf("volumeMount[%d] SubPath=%s", i, replacement.OriginalKey))
				current = append(current, fmt.Sprintf("volumeMount[%d] SubPath=%s", i, replacement.ReplacementKey))

			case tlsEnabled && volumeMount.SubPath == replacement.OriginalKey && useReplacementKey:
				// in the case of TLS enabled, make sure volumeMounts use the replacement key when the condition is met.
				volumeMount.SubPath = replacement.ReplacementKey
				modified = true
				expected = append(expected, fmt.Sprintf("volumeMount[%d] SubPath=%s", i, replacement.ReplacementKey))
				current = append(current, fmt.Sprintf("volumeMount[%d] SubPath=%s", i, replacement.OriginalKey))

			case tlsEnabled && volumeMount.SubPath == replacement.ReplacementKey && !useReplacementKey:
				// in the case of TLS enabled, make sure volumeMounts use the original key when the condition is not met.
				volumeMount.SubPath = replacement.OriginalKey
				modified = true
				expected = append(expected, fmt.Sprintf("volumeMount[%d] SubPath=%s\n", i, replacement.OriginalKey))
				current = append(current, fmt.Sprintf("volumeMount[%d] SubPath=%s\n", i, replacement.ReplacementKey))
			}
		}
		if modified {
			return strings.Join(expected, ", "), strings.Join(current, ", "), true
		}
		return "", "", false
	}

	var expected, current []string
	for _, replacement := range states.TLSConfigKeyReplacements {
		if replacement.DoesApplyStatefulSet(statefulSet) {

			if replacement.DoesApplyContainer() {
				for i := 0; i < len(statefulSet.Spec.Template.Spec.Containers); i++ {
					c := &statefulSet.Spec.Template.Spec.Containers[i]
					if vmExp, vmCur, ok := applyCorrespondingSubpath(c, replacement); ok {
						expected = append(expected, fmt.Sprintf("container[%s] %s", c.Name, vmExp))
						current = append(current, fmt.Sprintf("container[%s] %s", c.Name, vmCur))
					}
				}
			}

			if replacement.DoesApplyInitContainer() {
				for i := 0; i < len(statefulSet.Spec.Template.Spec.InitContainers); i++ {
					c := &statefulSet.Spec.Template.Spec.InitContainers[i]
					if vmExp, vmCur, ok := applyCorrespondingSubpath(c, replacement); ok {
						expected = append(expected, fmt.Sprintf("initContainer[%s] %s", c.Name, vmExp))
						current = append(current, fmt.Sprintf("initContainer[%s] %s", c.Name, vmCur))
					}
				}
			}
		}
	}

	if len(expected) > 0 {
		return strings.Join(expected, "; "), strings.Join(current, "; "), true
	}
	return "", "", false
}

func (t *TLSConfigSpecHelper) ConfigureTLSCertsVolume(statefulSet *apps.StatefulSet) (string, string, bool) {
	var (
		expected, current []string
		modified          bool
	)

	if !t.kubegresContext.Kubegres.Spec.TLS.Enabled {
		for i := 0; i < len(statefulSet.Spec.Template.Spec.Volumes); i++ {
			volume := statefulSet.Spec.Template.Spec.Volumes[i]
			if volume.Name == ctx.TLSVolumeName {
				expected = append(expected, fmt.Sprintf("volume[%d] %s must not exist", i, ctx.TLSVolumeName))
				current = append(current, fmt.Sprintf("volume[%d] %s exists", i, ctx.TLSVolumeName))
				modified = true
				statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes[:i], statefulSet.Spec.Template.Spec.Volumes[i+1:]...)
			}
		}

		for i := 0; i < len(statefulSet.Spec.Template.Spec.Containers); i++ {
			container := &statefulSet.Spec.Template.Spec.Containers[i]
			for j := 0; j < len(container.VolumeMounts); j++ {
				volumeMount := &container.VolumeMounts[j]
				if volumeMount.Name == ctx.TLSVolumeName {
					expected = append(expected, fmt.Sprintf("container[%s] volumeMount[%d] %s must not exist", container.Name, j, ctx.TLSVolumeName))
					current = append(current, fmt.Sprintf("container[%s] volumeMount[%d] %s exists", container.Name, j, ctx.TLSVolumeName))
					modified = true
					container.VolumeMounts = append(container.VolumeMounts[:j], container.VolumeMounts[j+1:]...)
				}
			}
		}

		for i := 0; i < len(statefulSet.Spec.Template.Spec.InitContainers); i++ {
			initContainer := &statefulSet.Spec.Template.Spec.InitContainers[i]
			for j := 0; j < len(initContainer.VolumeMounts); j++ {
				volumeMount := &initContainer.VolumeMounts[j]
				if volumeMount.Name == ctx.TLSVolumeName {
					expected = append(expected, fmt.Sprintf("initContainer[%s] volumeMount[%d] %s must not exist", initContainer.Name, j, ctx.TLSVolumeName))
					current = append(current, fmt.Sprintf("initContainer[%s] volumeMount[%d] %s exists", initContainer.Name, j, ctx.TLSVolumeName))
					modified = true
					initContainer.VolumeMounts = append(initContainer.VolumeMounts[:j], initContainer.VolumeMounts[j+1:]...)
				}
			}
		}

		if modified {
			return strings.Join(expected, "; "), strings.Join(current, "; "), true
		}
		return "", "", false
	}

	tlsVolume := TLSVolume(t.kubegresContext.Kubegres.Spec.TLS)
	tlsVolumeMount := TLSVolumeMount(t.kubegresContext.Kubegres.Spec.TLS)

	var tlsVolumeFound bool
	for i := 0; i < len(statefulSet.Spec.Template.Spec.Volumes); i++ {
		volume := &statefulSet.Spec.Template.Spec.Volumes[i]
		if volume.Name == ctx.TLSVolumeName {
			tlsVolumeFound = true
			if volume.Secret == nil || volume.Secret.SecretName != tlsVolume.Secret.SecretName ||
				volume.Secret.DefaultMode == nil || *volume.Secret.DefaultMode != *tlsVolume.Secret.DefaultMode {
				expected = append(expected, fmt.Sprintf("volume %s: %s", volume.Name, volumeToString(tlsVolume)))
				current = append(current, fmt.Sprintf("volume %s: %s", volume.Name, volumeToString(*volume)))
				modified = true
				volume.Secret = tlsVolume.Secret
			}
			break
		}
	}
	if !tlsVolumeFound {
		expected = append(expected, fmt.Sprintf("volume %s must exist", ctx.TLSVolumeName))
		current = append(current, fmt.Sprintf("volume %s does not exist", ctx.TLSVolumeName))
		modified = true
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, tlsVolume)
	}

	for i := 0; i < len(statefulSet.Spec.Template.Spec.Containers); i++ {
		container := &statefulSet.Spec.Template.Spec.Containers[i]
		var tlsVolumeMountFound bool
		for j := 0; j < len(container.VolumeMounts); j++ {
			volumeMount := &container.VolumeMounts[j]
			if volumeMount.Name == ctx.TLSVolumeName {
				tlsVolumeMountFound = true
				if volumeMount.MountPath != t.kubegresContext.Kubegres.Spec.TLS.MountPath || !volumeMount.ReadOnly {
					expected = append(expected, fmt.Sprintf("container[%s] volumeMount %s: %s", container.Name, volumeMount.Name, volumeMountToString(tlsVolumeMount)))
					current = append(current, fmt.Sprintf("container[%s] volumeMount %s: %s", container.Name, volumeMount.Name, volumeMountToString(*volumeMount)))
					modified = true
					volumeMount.MountPath = t.kubegresContext.Kubegres.Spec.TLS.MountPath
					volumeMount.ReadOnly = true
				}
				break
			}
		}
		if !tlsVolumeMountFound {
			expected = append(expected, fmt.Sprintf("container[%s] volumeMount %s must exist", container.Name, ctx.TLSVolumeName))
			current = append(current, fmt.Sprintf("container[%s] volumeMount %s does not exist", container.Name, ctx.TLSVolumeName))
			modified = true
			container.VolumeMounts = append(container.VolumeMounts, TLSVolumeMount(t.kubegresContext.Kubegres.Spec.TLS))
		}
	}

	for i := 0; i < len(statefulSet.Spec.Template.Spec.InitContainers); i++ {
		initContainer := &statefulSet.Spec.Template.Spec.InitContainers[i]
		var tlsVolumeMountFound bool
		for j := 0; j < len(initContainer.VolumeMounts); j++ {
			volumeMount := &initContainer.VolumeMounts[j]
			if volumeMount.Name == ctx.TLSVolumeName {
				tlsVolumeMountFound = true
				if volumeMount.MountPath != t.kubegresContext.Kubegres.Spec.TLS.MountPath || !volumeMount.ReadOnly {
					expected = append(expected, fmt.Sprintf("initContainer[%s] volumeMount %s: %s", initContainer.Name, volumeMount.Name, volumeMountToString(tlsVolumeMount)))
					current = append(current, fmt.Sprintf("initContainer[%s] volumeMount %s: %s", initContainer.Name, volumeMount.Name, volumeMountToString(*volumeMount)))
					modified = true
					volumeMount.MountPath = t.kubegresContext.Kubegres.Spec.TLS.MountPath
					volumeMount.ReadOnly = true
				}
				break
			}
		}
		if !tlsVolumeMountFound {
			expected = append(expected, fmt.Sprintf("initContainer[%s] volumeMount %s must exist", initContainer.Name, ctx.TLSVolumeName))
			current = append(current, fmt.Sprintf("initContainer[%s] volumeMount %s does not exist", initContainer.Name, ctx.TLSVolumeName))
			modified = true
			initContainer.VolumeMounts = append(initContainer.VolumeMounts, TLSVolumeMount(t.kubegresContext.Kubegres.Spec.TLS))
		}
	}

	if modified {
		return strings.Join(expected, "; "), strings.Join(current, "; "), true
	}
	return "", "", false
}

func volumeToString(volume corev1.Volume) string {
	str := strings.Builder{}
	str.WriteString("Name: " + volume.Name)
	if volume.VolumeSource.Secret == nil {
		str.WriteString(", Secret: nil")
		return str.String()
	}
	str.WriteString(", SecretName: " + volume.VolumeSource.Secret.SecretName)
	if volume.VolumeSource.Secret.DefaultMode == nil {
		str.WriteString(", DefaultMode: nil")
		return str.String()
	}
	str.WriteString(", DefaultMode: " + fmt.Sprintf("%d", *volume.VolumeSource.Secret.DefaultMode))
	return str.String()
}

func volumeMountToString(volumeMount corev1.VolumeMount) string {
	return "Name: " + volumeMount.Name +
		", MountPath: " + volumeMount.MountPath +
		", ReadOnly: " + fmt.Sprintf("%v", volumeMount.ReadOnly)
}

func (t *TLSConfigSpecHelper) defaultLivenessProbe() *corev1.Probe {
	if t.kubegresContext.Kubegres.Spec.TLS.Enabled {
		return defaultTLSLivenessProbe(t.kubegresContext.Kubegres.Spec)
	}
	return defaultLivenessProbe()
}

func (t *TLSConfigSpecHelper) defaultReadinessProbe() *corev1.Probe {
	if t.kubegresContext.Kubegres.Spec.TLS.Enabled {
		return defaultTLSReadinessProbe(t.kubegresContext.Kubegres.Spec)
	}
	return defaultReadinessProbe()
}

func (t *TLSConfigSpecHelper) ConfigureLivenessProbe(statefulSet *apps.StatefulSet) (string, string, bool) {
	var expected, current string

	expectedProbe := t.kubegresContext.Kubegres.Spec.Probe.LivenessProbe
	if t.kubegresContext.Kubegres.Spec.Probe.LivenessProbe == nil {
		expectedProbe = t.defaultLivenessProbe()
	}

	if !proto.Equal(statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe, expectedProbe) {
		expected = fmt.Sprintf("LivenessProbe: %s", expectedProbe)
		current = fmt.Sprintf("LivenessProbe: %s", statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe)
		statefulSet.Spec.Template.Spec.Containers[0].LivenessProbe = expectedProbe
		return expected, current, true
	}

	return "", "", false
}

func (t *TLSConfigSpecHelper) ConfigureReadinessProbe(statefulSet *apps.StatefulSet) (string, string, bool) {
	var expected, current string

	expectedProbe := t.kubegresContext.Kubegres.Spec.Probe.ReadinessProbe
	if t.kubegresContext.Kubegres.Spec.Probe.ReadinessProbe == nil {
		expectedProbe = t.defaultReadinessProbe()
	}

	if !proto.Equal(statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe, expectedProbe) {
		expected = fmt.Sprintf("ReadinessProbe: %s", expectedProbe)
		current = fmt.Sprintf("ReadinessProbe: %s", statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe)
		statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe = expectedProbe
		return expected, current, true
	}

	return "", "", false
}

func (t *TLSConfigSpecHelper) ConfigureSecurityContext(statefulSet *apps.StatefulSet) (string, string, bool) {
	var expected, current string

	if t.kubegresContext.Kubegres.Spec.SecurityContext == nil {
		var expectedSC *corev1.PodSecurityContext

		if t.kubegresContext.Kubegres.Spec.TLS.Enabled {
			var (
				runAsNonRoot = true       // Ensures the container runs as a non-root user
				runAsUser    = int64(999) // Default UID for Postgresql official image
				fsGroup      = int64(999) // Default GID for Postgresql official image
			)

			expectedSC = &corev1.PodSecurityContext{
				RunAsUser:    &runAsUser,
				RunAsNonRoot: &runAsNonRoot,
				FSGroup:      &fsGroup,
			}
		}

		if expectedSC == nil && statefulSet.Spec.Template.Spec.SecurityContext == nil {
			return "", "", false
		}

		if expectedSC == nil && proto.Equal(statefulSet.Spec.Template.Spec.SecurityContext, &corev1.PodSecurityContext{}) {
			return "", "", false
		}

		if !proto.Equal(statefulSet.Spec.Template.Spec.SecurityContext, expectedSC) {
			expected = fmt.Sprintf("SecurityContext: %s", expectedSC)
			current = fmt.Sprintf("SecurityContext: %s", statefulSet.Spec.Template.Spec.SecurityContext)
			statefulSet.Spec.Template.Spec.SecurityContext = expectedSC
			return expected, current, true
		}

		return "", "", false
	}

	if !proto.Equal(statefulSet.Spec.Template.Spec.SecurityContext, t.kubegresContext.Kubegres.Spec.SecurityContext) {
		expected = fmt.Sprintf("SecurityContext: %s", t.kubegresContext.Kubegres.Spec.SecurityContext)
		current = fmt.Sprintf("SecurityContext: %s", statefulSet.Spec.Template.Spec.SecurityContext)
		statefulSet.Spec.Template.Spec.SecurityContext = t.kubegresContext.Kubegres.Spec.SecurityContext
		return expected, current, true
	}

	return "", "", false
}

func (t *TLSConfigSpecHelper) ConfigureTLSEnvVars(statefulSet *apps.StatefulSet) (string, string, bool) {
	var expected, current []string
	for i := 0; i < len(statefulSet.Spec.Template.Spec.Containers); i++ {
		c := &statefulSet.Spec.Template.Spec.Containers[i]
		exp, cur, modified := t.ConfigureTLSEnvVarsToContainer(c)
		if modified {
			expected = append(expected, fmt.Sprintf("Container[%s]: %s", c.Name, exp))
			current = append(current, fmt.Sprintf("Container[%s]: %s", c.Name, cur))
		}
	}
	for i := 0; i < len(statefulSet.Spec.Template.Spec.InitContainers); i++ {
		c := &statefulSet.Spec.Template.Spec.InitContainers[i]
		exp, cur, modified := t.ConfigureTLSEnvVarsToContainer(c)
		if modified {
			expected = append(expected, fmt.Sprintf("InitContainer[%s]: %s", c.Name, exp))
			current = append(current, fmt.Sprintf("InitContainer[%s]: %s", c.Name, cur))
		}
	}
	if len(expected) > 0 {
		return strings.Join(expected, "; "), strings.Join(current, "; "), true
	}
	return "", "", false
}

func (t *TLSConfigSpecHelper) ConfigureTLSEnvVarsToContainer(c *corev1.Container) (string, string, bool) {
	envVar := corev1.EnvVar{
		Name:  ctx.EnvVarNameTLSMode,
		Value: t.kubegresContext.Kubegres.Spec.TLS.SSLMode.String(),
	}

	mustExist := t.kubegresContext.Kubegres.Spec.TLS.Enabled

	var (
		found bool
		i     int
	)
	for i = 0; i < len(c.Env); i++ {
		currentEv := &c.Env[i]
		if currentEv.Name == ctx.EnvVarNameTLSMode {
			if mustExist && currentEv.Value != envVar.Value {
				expected := fmt.Sprintf("EnvVar %s=%s", ctx.EnvVarNameTLSMode, envVar.Value)
				current := fmt.Sprintf("EnvVar %s=%s", ctx.EnvVarNameTLSMode, currentEv.Value)
				currentEv.Value = envVar.Value
				return expected, current, true
			}
			found = true
			break
		}
	}

	switch {
	case !found && mustExist:
		c.Env = append(c.Env, envVar)
		expected := fmt.Sprintf("EnvVar %s=%s", ctx.EnvVarNameTLSMode, envVar.Value)
		current := fmt.Sprintf("EnvVar %s does not exist", ctx.EnvVarNameTLSMode)
		return expected, current, true

	case found && !mustExist:
		c.Env = append(c.Env[:i], c.Env[i+1:]...)
		expected := fmt.Sprintf("EnvVar %s must NOT exist", ctx.EnvVarNameTLSMode)
		current := fmt.Sprintf("EnvVar %s exists", ctx.EnvVarNameTLSMode)
		return expected, current, true
	}

	return "", "", false
}

func tlsProbeCommand(spec apiv1.KubegresSpec) []string {
	postgresUser := "postgres"
	for _, ev := range spec.Env {
		if ev.Name == "POSTGRES_USER" {
			postgresUser = ev.Value
			break
		}
	}

	var sslmode apiv1.SSLMode
	switch spec.TLS.SSLMode {
	case apiv1.SSLModeVerifyFull:
		// VerifyFull will fail the k8s health checks due to the impossibility of create the TLS certs including the k8s api server IP.
		// We use VerifyCA instead, which is sufficient for the health checks.
		sslmode = apiv1.SSLModeVerifyCA
	case "":
		// If no SSLMode is specified, when TLS probe is requested, we assume TLS is transiting, so we default to Disable.
		sslmode = apiv1.SSLModeDisable
	default:
		sslmode = spec.TLS.SSLMode
	}

	return []string{
		"sh",
		"-c",
		fmt.Sprintf("PGPASSWORD=$POSTGRES_PASSWORD psql \"sslmode=%[5]s "+
			"sslrootcert=%[1]s sslcert=%[2]s sslkey=%[3]s "+
			"host=$POD_IP user=%[4]s\" -c \"SELECT 1\"",
			spec.TLS.RootCertPath, spec.TLS.ClientCertPath, spec.TLS.ClientKeyPath, postgresUser, sslmode),
	}
}

func defaultLivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"sh",
					"-c",
					"exec pg_isready -U postgres -h $POD_IP",
				},
			},
		},
		FailureThreshold:    10,
		InitialDelaySeconds: 60,
		PeriodSeconds:       20,
		SuccessThreshold:    1,
		TimeoutSeconds:      15,
	}
}

func defaultReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"sh",
					"-c",
					"exec pg_isready -U postgres -h $POD_IP",
				},
			},
		},
		FailureThreshold:    3,
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      3,
	}
}

func defaultTLSLivenessProbe(spec apiv1.KubegresSpec) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: tlsProbeCommand(spec),
			},
		},
		FailureThreshold:    10,
		InitialDelaySeconds: 60,
		PeriodSeconds:       20,
		SuccessThreshold:    1,
		TimeoutSeconds:      15,
	}
}

func defaultTLSReadinessProbe(spec apiv1.KubegresSpec) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: tlsProbeCommand(spec),
			},
		},
		FailureThreshold:    3,
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      3,
	}
}
