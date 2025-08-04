package template

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/states"
)

func noTLSCommand() []string {
	return []string{"sh", "-c", "exec pg_isready -U postgres -h $POD_IP"}
}

func tlsCommand(sslMode string) []string {
	return []string{
		"sh", "-c",
		fmt.Sprintf("PGPASSWORD=$POSTGRES_PASSWORD psql \"sslmode=%s "+
			"sslrootcert=/var/lib/postgresql/tls/ca.crt sslcert=/var/lib/postgresql/tls/client.crt "+
			"sslkey=/var/lib/postgresql/tls/client.key host=$POD_IP user=postgres\" -c \"SELECT 1\"", sslMode),
	}
}

func newLivenessProbes(command []string) *corev1.Probe {
	return &corev1.Probe{
		InitialDelaySeconds: 60,
		TimeoutSeconds:      15,
		PeriodSeconds:       20,
		SuccessThreshold:    1,
		FailureThreshold:    10,
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: command,
			},
		},
	}
}

func newReadinessProbes(command []string) *corev1.Probe {
	return &corev1.Probe{
		InitialDelaySeconds: 5,
		TimeoutSeconds:      3,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    3,
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: command,
			},
		},
	}
}

func newTLSVolume() *corev1.Volume {
	return &corev1.Volume{
		Name: "tls-certs",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  "tls-certs-secret",
				DefaultMode: &[]int32{416}[0], // 0640
			},
		},
	}
}

func newTLSVolumeMount() *corev1.VolumeMount {
	return &corev1.VolumeMount{
		Name:      "tls-certs",
		MountPath: "/var/lib/postgresql/tls",
		ReadOnly:  true,
	}
}

func withDefaultMode(v *corev1.Volume, mode int32) *corev1.Volume {
	v.VolumeSource.Secret.DefaultMode = &mode
	return v
}

func withReadOnly(v *corev1.VolumeMount, readonly bool) *corev1.VolumeMount {
	v.ReadOnly = readonly
	return v
}

func withVolumes(containers int, volume *corev1.Volume, volumeMount *corev1.VolumeMount) *appsv1.StatefulSet {
	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{},
			},
		},
	}
	if volume != nil {
		ss.Spec.Template.Spec.Volumes = []corev1.Volume{*volume}
	}
	for i := 0; i < containers; i++ {
		c := corev1.Container{Name: fmt.Sprintf("test-container-%d", i)}
		ic := corev1.Container{Name: fmt.Sprintf("test-init-container-%d", i)}
		if volumeMount != nil {
			c.VolumeMounts = []corev1.VolumeMount{*volumeMount}
			ic.VolumeMounts = []corev1.VolumeMount{*volumeMount}
		}
		ss.Spec.Template.Spec.Containers = append(ss.Spec.Template.Spec.Containers, c)
		ss.Spec.Template.Spec.InitContainers = append(ss.Spec.Template.Spec.InitContainers, ic)
	}
	return ss
}

func withVolumeMountKeys(containers int, name string, mainKeys, initKeys []string) *appsv1.StatefulSet {
	volumeMount := &corev1.VolumeMount{
		Name:      name,
		MountPath: "cm-mount-path",
		ReadOnly:  true,
	}
	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{},
			},
		},
	}
	for i := 0; i < containers; i++ {
		c := &corev1.Container{Name: fmt.Sprintf("test-container-%d", i)}
		for _, key := range mainKeys {
			v := volumeMount.DeepCopy()
			v.SubPath = key
			c.VolumeMounts = append(c.VolumeMounts, *v)
		}
		ss.Spec.Template.Spec.Containers = append(ss.Spec.Template.Spec.Containers, *c)

		ic := &corev1.Container{Name: fmt.Sprintf("test-init-container-%d", i)}
		for _, key := range initKeys {
			v := volumeMount.DeepCopy()
			v.SubPath = key
			ic.VolumeMounts = append(ic.VolumeMounts, *v)
		}
		ss.Spec.Template.Spec.InitContainers = append(ss.Spec.Template.Spec.InitContainers, *ic)
	}
	return ss
}

func primary(set *appsv1.StatefulSet) *appsv1.StatefulSet {
	set.Labels = map[string]string{
		"replicationRole": "primary",
	}
	return set
}

func replica(set *appsv1.StatefulSet) *appsv1.StatefulSet {
	set.Labels = map[string]string{
		"replicationRole": "replica",
	}
	return set
}

func TestTLSConfigSpecHelper_ConfigureLivenessProbe(t *testing.T) {

	cases := []struct {
		name         string
		spec         v1.KubegresSpec
		given        *corev1.Probe
		want         *corev1.Probe
		wantExp      string
		wantCurr     string
		wantModified bool
	}{
		{
			name:         "Default liveness probe",
			spec:         v1.KubegresSpec{},
			given:        nil,
			want:         newLivenessProbes(noTLSCommand()),
			wantModified: true,
			wantExp:      fmt.Sprintf("LivenessProbe: %s", newLivenessProbes(noTLSCommand())),
			wantCurr:     "LivenessProbe: nil",
		},
		{
			name: "Default TLS liveness probe",
			spec: v1.KubegresSpec{TLS: v1.TLS{
				Enabled:        true,
				SSLMode:        v1.SSLModeRequire,
				RootCertPath:   "/var/lib/postgresql/tls/ca.crt",
				ClientCertPath: "/var/lib/postgresql/tls/client.crt",
				ClientKeyPath:  "/var/lib/postgresql/tls/client.key",
			}},
			given:        nil,
			want:         newLivenessProbes(tlsCommand("require")),
			wantModified: true,
			wantExp:      fmt.Sprintf("LivenessProbe: %s", newLivenessProbes(tlsCommand("require"))),
			wantCurr:     "LivenessProbe: nil",
		},
		{
			name: "From no TLS default to TLS default",
			spec: v1.KubegresSpec{TLS: v1.TLS{
				Enabled:        true,
				SSLMode:        v1.SSLModeRequire,
				RootCertPath:   "/var/lib/postgresql/tls/ca.crt",
				ClientCertPath: "/var/lib/postgresql/tls/client.crt",
				ClientKeyPath:  "/var/lib/postgresql/tls/client.key",
			}},
			given:        newLivenessProbes(noTLSCommand()),
			want:         newLivenessProbes(tlsCommand("require")),
			wantModified: true,
			wantExp:      fmt.Sprintf("LivenessProbe: %s", newLivenessProbes(tlsCommand("require"))),
			wantCurr:     fmt.Sprintf("LivenessProbe: %s", newLivenessProbes(noTLSCommand())),
		},
		{
			name:         "From TLS default to no TLS default",
			spec:         v1.KubegresSpec{},
			given:        newLivenessProbes(tlsCommand("require")),
			want:         newLivenessProbes(noTLSCommand()),
			wantModified: true,
			wantExp:      fmt.Sprintf("LivenessProbe: %s", newLivenessProbes(noTLSCommand())),
			wantCurr:     fmt.Sprintf("LivenessProbe: %s", newLivenessProbes(tlsCommand("require"))),
		},
		{
			name: "From TLS default to TLS default with different SSL mode",
			spec: v1.KubegresSpec{TLS: v1.TLS{
				Enabled:        true,
				SSLMode:        v1.SSLModeVerifyCA,
				RootCertPath:   "/var/lib/postgresql/tls/ca.crt",
				ClientCertPath: "/var/lib/postgresql/tls/client.crt",
				ClientKeyPath:  "/var/lib/postgresql/tls/client.key",
			}},
			given:        newLivenessProbes(tlsCommand("require")),
			want:         newLivenessProbes(tlsCommand("verify-ca")),
			wantModified: true,
			wantExp:      fmt.Sprintf("LivenessProbe: %s", newLivenessProbes(tlsCommand("verify-ca"))),
			wantCurr:     fmt.Sprintf("LivenessProbe: %s", newLivenessProbes(tlsCommand("require"))),
		},
		{
			name: "From TLS default to TLS default with same SSL mode",
			spec: v1.KubegresSpec{TLS: v1.TLS{
				Enabled:        true,
				SSLMode:        v1.SSLModeAllow,
				RootCertPath:   "/var/lib/postgresql/tls/ca.crt",
				ClientCertPath: "/var/lib/postgresql/tls/client.crt",
				ClientKeyPath:  "/var/lib/postgresql/tls/client.key",
			}},
			given:        newLivenessProbes(tlsCommand("allow")),
			want:         newLivenessProbes(tlsCommand("allow")),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "From no TLS default to no TLS default",
			spec:         v1.KubegresSpec{},
			given:        newLivenessProbes(noTLSCommand()),
			want:         newLivenessProbes(noTLSCommand()),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tlsHelper := CreateTLSConfigSpecHelper(ctx.KubegresContext{Kubegres: &v1.Kubegres{Spec: tc.spec}})

			given := &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:          "test-container",
									LivenessProbe: tc.given.DeepCopy(),
								},
							},
						},
					},
				},
			}

			gotExp, gotCurr, gotModified := tlsHelper.ConfigureLivenessProbe(given)
			if tc.wantModified != gotModified {
				t.Errorf("Expected modified: %v, got: %v", tc.wantModified, gotModified)
			}
			if tc.wantExp != gotExp {
				t.Errorf("Expected expected string: %s, got: %s", tc.wantExp, gotExp)
			}
			if tc.wantCurr != gotCurr {
				t.Errorf("Expected current string: %s, got: %s", tc.wantCurr, gotCurr)
			}

			got := given.Spec.Template.Spec.Containers[0].LivenessProbe
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Got unexpected LivenessProbe (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTLSConfigSpecHelper_ConfigureReadinessProbe(t *testing.T) {

	cases := []struct {
		name         string
		spec         v1.KubegresSpec
		given        *corev1.Probe
		want         *corev1.Probe
		wantExp      string
		wantCurr     string
		wantModified bool
	}{
		{
			name:         "Default readiness probe",
			spec:         v1.KubegresSpec{},
			given:        nil,
			want:         newReadinessProbes(noTLSCommand()),
			wantModified: true,
			wantExp:      fmt.Sprintf("ReadinessProbe: %s", newReadinessProbes(noTLSCommand())),
			wantCurr:     "ReadinessProbe: nil",
		},
		{
			name: "Default TLS readiness probe",
			spec: v1.KubegresSpec{TLS: v1.TLS{
				Enabled:        true,
				SSLMode:        v1.SSLModeRequire,
				RootCertPath:   "/var/lib/postgresql/tls/ca.crt",
				ClientCertPath: "/var/lib/postgresql/tls/client.crt",
				ClientKeyPath:  "/var/lib/postgresql/tls/client.key",
			}},
			given:        nil,
			want:         newReadinessProbes(tlsCommand("require")),
			wantModified: true,
			wantExp:      fmt.Sprintf("ReadinessProbe: %s", newReadinessProbes(tlsCommand("require"))),
			wantCurr:     "ReadinessProbe: nil",
		},
		{
			name: "From no TLS default to TLS default",
			spec: v1.KubegresSpec{TLS: v1.TLS{
				Enabled:        true,
				SSLMode:        v1.SSLModeRequire,
				RootCertPath:   "/var/lib/postgresql/tls/ca.crt",
				ClientCertPath: "/var/lib/postgresql/tls/client.crt",
				ClientKeyPath:  "/var/lib/postgresql/tls/client.key",
			}},
			given:        newReadinessProbes(noTLSCommand()),
			want:         newReadinessProbes(tlsCommand("require")),
			wantModified: true,
			wantExp:      fmt.Sprintf("ReadinessProbe: %s", newReadinessProbes(tlsCommand("require"))),
			wantCurr:     fmt.Sprintf("ReadinessProbe: %s", newReadinessProbes(noTLSCommand())),
		},
		{
			name:         "From TLS default to no TLS default",
			spec:         v1.KubegresSpec{},
			given:        newReadinessProbes(tlsCommand("require")),
			want:         newReadinessProbes(noTLSCommand()),
			wantModified: true,
			wantExp:      fmt.Sprintf("ReadinessProbe: %s", newReadinessProbes(noTLSCommand())),
			wantCurr:     fmt.Sprintf("ReadinessProbe: %s", newReadinessProbes(tlsCommand("require"))),
		},
		{
			name: "From TLS default to TLS default with different SSL mode",
			spec: v1.KubegresSpec{TLS: v1.TLS{
				Enabled:        true,
				SSLMode:        v1.SSLModeVerifyCA,
				RootCertPath:   "/var/lib/postgresql/tls/ca.crt",
				ClientCertPath: "/var/lib/postgresql/tls/client.crt",
				ClientKeyPath:  "/var/lib/postgresql/tls/client.key",
			}},
			given:        newReadinessProbes(tlsCommand("require")),
			want:         newReadinessProbes(tlsCommand("verify-ca")),
			wantModified: true,
			wantExp:      fmt.Sprintf("ReadinessProbe: %s", newReadinessProbes(tlsCommand("verify-ca"))),
			wantCurr:     fmt.Sprintf("ReadinessProbe: %s", newReadinessProbes(tlsCommand("require"))),
		},
		{
			name: "From TLS default to TLS default with same SSL mode",
			spec: v1.KubegresSpec{TLS: v1.TLS{
				Enabled:        true,
				SSLMode:        v1.SSLModeAllow,
				RootCertPath:   "/var/lib/postgresql/tls/ca.crt",
				ClientCertPath: "/var/lib/postgresql/tls/client.crt",
				ClientKeyPath:  "/var/lib/postgresql/tls/client.key",
			}},
			given:        newReadinessProbes(tlsCommand("allow")),
			want:         newReadinessProbes(tlsCommand("allow")),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "From no TLS default to no TLS default",
			spec:         v1.KubegresSpec{},
			given:        newReadinessProbes(noTLSCommand()),
			want:         newReadinessProbes(noTLSCommand()),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tlsHelper := CreateTLSConfigSpecHelper(ctx.KubegresContext{Kubegres: &v1.Kubegres{Spec: tc.spec}})

			given := &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:           "test-container",
									ReadinessProbe: tc.given.DeepCopy(),
								},
							},
						},
					},
				},
			}

			gotExp, gotCurr, gotModified := tlsHelper.ConfigureReadinessProbe(given)
			if tc.wantModified != gotModified {
				t.Errorf("Expected modified: %v, got: %v", tc.wantModified, gotModified)
			}
			if tc.wantExp != gotExp {
				t.Errorf("Expected expected string: %s, got: %s", tc.wantExp, gotExp)
			}
			if tc.wantCurr != gotCurr {
				t.Errorf("Expected current string: %s, got: %s", tc.wantCurr, gotCurr)
			}

			got := given.Spec.Template.Spec.Containers[0].ReadinessProbe
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Got unexpected ReadinessProbe (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTLSConfigSpecHelper_ConfigureSecurityContext(t *testing.T) {
	var (
		otherUser    int64 = 1000
		runAsUser    int64 = 999
		runAsNonRoot       = true
		fsGroup      int64 = 999
	)

	cases := []struct {
		name         string
		spec         v1.KubegresSpec
		given        *corev1.PodSecurityContext
		want         *corev1.PodSecurityContext
		wantExp      string
		wantCurr     string
		wantModified bool
	}{
		{
			name:         "Default security context",
			spec:         v1.KubegresSpec{},
			given:        nil,
			want:         nil,
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "Default TLS security context",
			spec:         v1.KubegresSpec{TLS: v1.TLS{Enabled: true}},
			given:        nil,
			want:         &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, FSGroup: &fsGroup},
			wantModified: true,
			wantExp:      fmt.Sprintf("SecurityContext: %s", &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, FSGroup: &fsGroup}),
			wantCurr:     "SecurityContext: nil",
		},
		{
			name:         "From no TLS default to TLS default",
			spec:         v1.KubegresSpec{TLS: v1.TLS{Enabled: true}},
			given:        nil,
			want:         &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, FSGroup: &fsGroup},
			wantModified: true,
			wantExp:      fmt.Sprintf("SecurityContext: %s", &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, FSGroup: &fsGroup}),
			wantCurr:     "SecurityContext: nil",
		},
		{
			name:         "From TLS default to no TLS default",
			spec:         v1.KubegresSpec{},
			given:        &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, FSGroup: &fsGroup},
			want:         nil,
			wantModified: true,
			wantExp:      "SecurityContext: nil",
			wantCurr:     fmt.Sprintf("SecurityContext: %s", &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, FSGroup: &fsGroup}),
		},
		{
			name:         "Custom security context with NO TLS",
			spec:         v1.KubegresSpec{SecurityContext: &corev1.PodSecurityContext{RunAsUser: &otherUser}},
			given:        nil,
			want:         &corev1.PodSecurityContext{RunAsUser: &otherUser},
			wantModified: true,
			wantExp:      fmt.Sprintf("SecurityContext: %s", &corev1.PodSecurityContext{RunAsUser: &otherUser}),
			wantCurr:     "SecurityContext: nil",
		},
		{
			name: "Custom security context with TLS",
			spec: v1.KubegresSpec{
				TLS:             v1.TLS{Enabled: true},
				SecurityContext: &corev1.PodSecurityContext{RunAsUser: &otherUser},
			},
			given:        nil,
			want:         &corev1.PodSecurityContext{RunAsUser: &otherUser},
			wantModified: true,
			wantExp:      fmt.Sprintf("SecurityContext: %s", &corev1.PodSecurityContext{RunAsUser: &otherUser}),
			wantCurr:     "SecurityContext: nil",
		},
		{
			name:         "Default TLS security context no change",
			spec:         v1.KubegresSpec{TLS: v1.TLS{Enabled: true}},
			given:        &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, FSGroup: &fsGroup},
			want:         &corev1.PodSecurityContext{RunAsUser: &runAsUser, RunAsNonRoot: &runAsNonRoot, FSGroup: &fsGroup},
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "Default NO TLS security context no change",
			spec:         v1.KubegresSpec{},
			given:        nil,
			want:         nil,
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "Custom security context NO TLS no change",
			spec:         v1.KubegresSpec{SecurityContext: &corev1.PodSecurityContext{RunAsUser: &otherUser}},
			given:        &corev1.PodSecurityContext{RunAsUser: &otherUser},
			want:         &corev1.PodSecurityContext{RunAsUser: &otherUser},
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name: "Custom security context with TLS no change",
			spec: v1.KubegresSpec{
				TLS:             v1.TLS{Enabled: true},
				SecurityContext: &corev1.PodSecurityContext{RunAsUser: &otherUser},
			},
			given:        &corev1.PodSecurityContext{RunAsUser: &otherUser},
			want:         &corev1.PodSecurityContext{RunAsUser: &otherUser},
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "Given empty not nil security context, no TLS",
			spec:         v1.KubegresSpec{},
			given:        &corev1.PodSecurityContext{},
			want:         &corev1.PodSecurityContext{},
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tlsHelper := CreateTLSConfigSpecHelper(ctx.KubegresContext{Kubegres: &v1.Kubegres{Spec: tc.spec}})

			given := &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							SecurityContext: tc.given.DeepCopy(),
						},
					},
				},
			}

			gotExp, gotCurr, gotModified := tlsHelper.ConfigureSecurityContext(given)
			if tc.wantModified != gotModified {
				t.Errorf("Expected modified: %v, got: %v", tc.wantModified, gotModified)
			}
			if tc.wantExp != gotExp {
				t.Errorf("Expected expected string: %s, got: %s", tc.wantExp, gotExp)
			}
			if tc.wantCurr != gotCurr {
				t.Errorf("Expected current string: %s, got: %s", tc.wantCurr, gotCurr)
			}

			got := given.Spec.Template.Spec.SecurityContext
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Got unexpected SecurityContext (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTLSConfigSpecHelper_ConfigureTLSEnvVars(t *testing.T) {

	cases := []struct {
		name         string
		spec         v1.KubegresSpec
		given        []corev1.EnvVar
		want         []corev1.EnvVar
		wantExp      string
		wantCurr     string
		wantModified bool
	}{
		{
			name:         "Default env vars NO TLS",
			spec:         v1.KubegresSpec{},
			given:        nil,
			want:         nil,
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "Default env vars TLS",
			spec:         v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeVerifyCA}},
			given:        nil,
			want:         []corev1.EnvVar{{Name: "SSL_MODE", Value: "verify-ca"}},
			wantModified: true,
			wantExp:      "Container[test-container]: EnvVar SSL_MODE=verify-ca",
			wantCurr:     "Container[test-container]: EnvVar SSL_MODE does not exist",
		},
		{
			name:         "From TLS default to NO TLS default",
			spec:         v1.KubegresSpec{},
			given:        []corev1.EnvVar{{Name: "SSL_MODE", Value: "verify-ca"}},
			want:         []corev1.EnvVar{},
			wantModified: true,
			wantExp:      "Container[test-container]: EnvVar SSL_MODE must NOT exist",
			wantCurr:     "Container[test-container]: EnvVar SSL_MODE exists",
		},
		{
			name:         "Given other env vars, no TLS",
			spec:         v1.KubegresSpec{},
			given:        []corev1.EnvVar{{Name: "OTHER_VAR", Value: "value"}},
			want:         []corev1.EnvVar{{Name: "OTHER_VAR", Value: "value"}},
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:  "Given other env vars, TLS",
			spec:  v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeVerifyCA}},
			given: []corev1.EnvVar{{Name: "OTHER_VAR", Value: "value"}},
			want: []corev1.EnvVar{
				{Name: "OTHER_VAR", Value: "value"},
				{Name: "SSL_MODE", Value: "verify-ca"},
			},
			wantModified: true,
			wantExp:      "Container[test-container]: EnvVar SSL_MODE=verify-ca",
			wantCurr:     "Container[test-container]: EnvVar SSL_MODE does not exist",
		},
		{
			name: "Given other env vars and TLS with different value",
			spec: v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeRequire}},
			given: []corev1.EnvVar{
				{Name: "SSL_MODE", Value: "verify-ca"},
				{Name: "OTHER_VAR", Value: "value"},
			},
			want: []corev1.EnvVar{
				{Name: "SSL_MODE", Value: "require"},
				{Name: "OTHER_VAR", Value: "value"},
			},
			wantModified: true,
			wantExp:      "Container[test-container]: EnvVar SSL_MODE=require",
			wantCurr:     "Container[test-container]: EnvVar SSL_MODE=verify-ca",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tlsHelper := CreateTLSConfigSpecHelper(ctx.KubegresContext{Kubegres: &v1.Kubegres{Spec: tc.spec}})

			given := &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "test-container",
									Env:  tc.given,
								},
							},
						},
					},
				},
			}

			gotExp, gotCurr, gotModified := tlsHelper.ConfigureTLSEnvVars(given)
			if tc.wantModified != gotModified {
				t.Errorf("Expected modified: %v, got: %v", tc.wantModified, gotModified)
			}
			if tc.wantExp != gotExp {
				t.Errorf("Expected expected string: %s, got: %s", tc.wantExp, gotExp)
			}
			if tc.wantCurr != gotCurr {
				t.Errorf("Expected current string: %s, got: %s", tc.wantCurr, gotCurr)
			}

			got := given.Spec.Template.Spec.Containers[0].Env
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Got unexpected EnvVars (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTLSConfigSpecHelper_ConfigureTLSVolume(t *testing.T) {
	cases := []struct {
		name         string
		spec         v1.KubegresSpec
		given        *appsv1.StatefulSet
		want         *appsv1.StatefulSet
		wantModified bool
		wantExp      string
		wantCurr     string
	}{
		{
			name:         "From no TLS to no TLS volume",
			spec:         v1.KubegresSpec{},
			given:        withVolumes(1, nil, nil),
			want:         withVolumes(1, nil, nil),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "From no TLS to TLS volume",
			spec:         v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls", SecretName: "tls-certs-secret"}},
			given:        withVolumes(1, nil, nil),
			want:         withVolumes(1, newTLSVolume(), newTLSVolumeMount()),
			wantModified: true,
			wantExp: "volume tls-certs must exist; container[test-container-0] volumeMount tls-certs must exist; " +
				"initContainer[test-init-container-0] volumeMount tls-certs must exist",
			wantCurr: "volume tls-certs does not exist; container[test-container-0] volumeMount tls-certs does not exist; " +
				"initContainer[test-init-container-0] volumeMount tls-certs does not exist",
		},
		{
			name:         "From TLS to no TLS volume",
			spec:         v1.KubegresSpec{},
			given:        withVolumes(1, newTLSVolume(), newTLSVolumeMount()),
			want:         withVolumes(1, nil, nil),
			wantModified: true,
			wantExp: "volume[0] tls-certs must not exist; container[test-container-0] volumeMount[0] tls-certs must not exist; " +
				"initContainer[test-init-container-0] volumeMount[0] tls-certs must not exist",
			wantCurr: "volume[0] tls-certs exists; container[test-container-0] volumeMount[0] tls-certs exists; " +
				"initContainer[test-init-container-0] volumeMount[0] tls-certs exists",
		},
		{
			name:         "From TLS to TLS volume with different spec",
			spec:         v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls", SecretName: "tls-certs-secret"}},
			given:        withVolumes(1, withDefaultMode(newTLSVolume(), 420), withReadOnly(newTLSVolumeMount(), false)),
			want:         withVolumes(1, newTLSVolume(), newTLSVolumeMount()),
			wantModified: true,
			wantExp: "volume tls-certs: Name: tls-certs, SecretName: tls-certs-secret, DefaultMode: 416; " +
				"container[test-container-0] volumeMount tls-certs: Name: tls-certs, MountPath: /var/lib/postgresql/tls, ReadOnly: true; " +
				"initContainer[test-init-container-0] volumeMount tls-certs: Name: tls-certs, MountPath: /var/lib/postgresql/tls, ReadOnly: true",
			wantCurr: "volume tls-certs: Name: tls-certs, SecretName: tls-certs-secret, DefaultMode: 420; " +
				"container[test-container-0] volumeMount tls-certs: Name: tls-certs, MountPath: /var/lib/postgresql/tls, ReadOnly: false; " +
				"initContainer[test-init-container-0] volumeMount tls-certs: Name: tls-certs, MountPath: /var/lib/postgresql/tls, ReadOnly: false",
		},
		{
			name:         "From TLS to TLS volume with same spec",
			spec:         v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls", SecretName: "tls-certs-secret"}},
			given:        withVolumes(1, newTLSVolume(), newTLSVolumeMount()),
			want:         withVolumes(1, newTLSVolume(), newTLSVolumeMount()),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "From no TLS to no TLS volume with same spec",
			spec:         v1.KubegresSpec{},
			given:        withVolumes(1, nil, nil),
			want:         withVolumes(1, nil, nil),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tlsHelper := CreateTLSConfigSpecHelper(ctx.KubegresContext{Kubegres: &v1.Kubegres{Spec: tc.spec}})

			gotExp, gotCurr, gotModified := tlsHelper.ConfigureTLSCertsVolume(tc.given)
			if tc.wantModified != gotModified {
				t.Errorf("Expected modified: %v, got: %v", tc.wantModified, gotModified)
			}
			if tc.wantExp != gotExp {
				t.Errorf("Expected expected string: %s, got: %s", tc.wantExp, gotExp)
			}
			if tc.wantCurr != gotCurr {
				t.Errorf("Expected current string: %s, got: %s", tc.wantCurr, gotCurr)
			}

			if diff := cmp.Diff(tc.want, tc.given, protocmp.Transform()); diff != "" {
				t.Errorf("Got unexpected StatefulSet (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTLSConfigSpecHelper_ConfigureTLSVolumeMounts(t *testing.T) {
	cases := []struct {
		name         string
		spec         v1.KubegresSpec
		given        *appsv1.StatefulSet
		want         *appsv1.StatefulSet
		wantModified bool
		wantExp      string
		wantCurr     string
	}{
		{
			name:         "default no TLS",
			spec:         v1.KubegresSpec{},
			given:        primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName, []string{states.ConfigMapDataKeyPostgresConf}, nil)),
			want:         primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName, []string{states.ConfigMapDataKeyPostgresConf}, nil)),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name:         "default TLS",
			spec:         v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls"}},
			given:        primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName, []string{states.ConfigMapDataKeyPostgresConf}, nil)),
			want:         primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName, []string{states.ConfigMapDataKeyTLSPostgresConf}, nil)),
			wantModified: true,
			wantExp:      "container[test-container-0] volumeMount[0] SubPath=tls_postgres.conf",
			wantCurr:     "container[test-container-0] volumeMount[0] SubPath=postgres.conf",
		},
		{
			name: "default TLS no change",
			spec: v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls"}},
			given: primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyTLSPostgresConf},
				[]string{states.ConfigMapDataKeyPrimaryInitScript},
			)),
			want: primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyTLSPostgresConf},
				[]string{states.ConfigMapDataKeyPrimaryInitScript},
			)),
			wantModified: false,
			wantExp:      "",
			wantCurr:     "",
		},
		{
			name: "primary: from TLS to no TLS",
			spec: v1.KubegresSpec{},
			given: primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyTLSPostgresConf, states.ConfigMapDataKeyTLSPgHbaConf},
				[]string{states.ConfigMapDataKeyPrimaryInitScript},
			)),
			want: primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyPostgresConf, states.ConfigMapDataKeyPgHbaConf},
				[]string{states.ConfigMapDataKeyPrimaryInitScript},
			)),
			wantModified: true,
			wantExp:      "container[test-container-0] volumeMount[0] SubPath=postgres.conf; container[test-container-0] volumeMount[1] SubPath=pg_hba.conf",
			wantCurr:     "container[test-container-0] volumeMount[0] SubPath=tls_postgres.conf; container[test-container-0] volumeMount[1] SubPath=tls_pg_hba.conf",
		},
		{
			name: "primary: from no TLS to TLS and ssl mode verify-ca",
			spec: v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls", SSLMode: v1.SSLModeVerifyCA}},
			given: primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyPostgresConf, states.ConfigMapDataKeyPgHbaConf},
				[]string{states.ConfigMapDataKeyPrimaryInitScript},
			)),
			want: primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyTLSPostgresConf, states.ConfigMapDataKeyTLSPgHbaConf},
				[]string{states.ConfigMapDataKeyPrimaryInitScript},
			)),
			wantModified: true,
			wantExp:      "container[test-container-0] volumeMount[0] SubPath=tls_postgres.conf; container[test-container-0] volumeMount[1] SubPath=tls_pg_hba.conf",
			wantCurr:     "container[test-container-0] volumeMount[0] SubPath=postgres.conf; container[test-container-0] volumeMount[1] SubPath=pg_hba.conf",
		},
		{
			name: "primary: from no TLS to TLS and ssl mode disable",
			spec: v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls", SSLMode: v1.SSLModeDisable}},
			given: primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyPostgresConf, states.ConfigMapDataKeyPgHbaConf},
				[]string{states.ConfigMapDataKeyPrimaryInitScript},
			)),
			want: primary(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyTLSPostgresConf, states.ConfigMapDataKeyPgHbaConf},
				[]string{states.ConfigMapDataKeyPrimaryInitScript},
			)),
			wantModified: true,
			wantExp:      "container[test-container-0] volumeMount[0] SubPath=tls_postgres.conf",
			wantCurr:     "container[test-container-0] volumeMount[0] SubPath=postgres.conf",
		},
		{
			name: "replica: from TLS to no TLS",
			spec: v1.KubegresSpec{},
			given: replica(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyTLSPostgresConf, states.ConfigMapDataKeyTLSPgHbaConf},
				[]string{states.ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript},
			)),
			want: replica(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyPostgresConf, states.ConfigMapDataKeyPgHbaConf},
				[]string{states.ConfigMapDataKeyCopyPrimaryDataToReplica},
			)),
			wantModified: true,
			wantExp: "container[test-container-0] volumeMount[0] SubPath=postgres.conf; " +
				"container[test-container-0] volumeMount[1] SubPath=pg_hba.conf; " +
				"initContainer[test-init-container-0] volumeMount[0] SubPath=copy_primary_data_to_replica.sh",
			wantCurr: "container[test-container-0] volumeMount[0] SubPath=tls_postgres.conf; " +
				"container[test-container-0] volumeMount[1] SubPath=tls_pg_hba.conf; " +
				"initContainer[test-init-container-0] volumeMount[0] SubPath=tls_copy_primary_data_to_replica.sh",
		},
		{
			name: "replica: from no TLS to TLS and ssl mode verify-ca",
			spec: v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls", SSLMode: v1.SSLModeVerifyCA}},
			given: replica(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyPostgresConf, states.ConfigMapDataKeyPgHbaConf},
				[]string{states.ConfigMapDataKeyCopyPrimaryDataToReplica},
			)),
			want: replica(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyTLSPostgresConf, states.ConfigMapDataKeyTLSPgHbaConf},
				[]string{states.ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript},
			)),
			wantModified: true,
			wantExp: "container[test-container-0] volumeMount[0] SubPath=tls_postgres.conf; " +
				"container[test-container-0] volumeMount[1] SubPath=tls_pg_hba.conf; " +
				"initContainer[test-init-container-0] volumeMount[0] SubPath=tls_copy_primary_data_to_replica.sh",
			wantCurr: "container[test-container-0] volumeMount[0] SubPath=postgres.conf; " +
				"container[test-container-0] volumeMount[1] SubPath=pg_hba.conf; " +
				"initContainer[test-init-container-0] volumeMount[0] SubPath=copy_primary_data_to_replica.sh",
		},
		{
			name: "replica: from no TLS to TLS and ssl mode disable",
			spec: v1.KubegresSpec{TLS: v1.TLS{Enabled: true, MountPath: "/var/lib/postgresql/tls", SSLMode: v1.SSLModeDisable}},
			given: replica(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyPostgresConf, states.ConfigMapDataKeyPgHbaConf},
				[]string{states.ConfigMapDataKeyCopyPrimaryDataToReplica},
			)),
			want: replica(withVolumeMountKeys(1, ctx.BaseConfigMapVolumeName,
				[]string{states.ConfigMapDataKeyTLSPostgresConf, states.ConfigMapDataKeyPgHbaConf},
				[]string{states.ConfigMapDataKeyTLSCopyPrimaryDataToReplicaScript},
			)),
			wantModified: true,
			wantExp: "container[test-container-0] volumeMount[0] SubPath=tls_postgres.conf; " +
				"initContainer[test-init-container-0] volumeMount[0] SubPath=tls_copy_primary_data_to_replica.sh",
			wantCurr: "container[test-container-0] volumeMount[0] SubPath=postgres.conf; " +
				"initContainer[test-init-container-0] volumeMount[0] SubPath=copy_primary_data_to_replica.sh",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tlsHelper := CreateTLSConfigSpecHelper(ctx.KubegresContext{Kubegres: &v1.Kubegres{Spec: tc.spec}})

			gotExp, gotCurr, gotModified := tlsHelper.ConfigureVolumeMounts(tc.given)
			if tc.wantModified != gotModified {
				t.Errorf("Expected modified: %v, got: %v", tc.wantModified, gotModified)
			}
			if tc.wantExp != gotExp {
				t.Errorf("Expected expected string: %s, got: %s", tc.wantExp, gotExp)
			}
			if tc.wantCurr != gotCurr {
				t.Errorf("Expected current string: %s, got: %s", tc.wantCurr, gotCurr)
			}
			if diff := cmp.Diff(tc.want, tc.given, protocmp.Transform()); diff != "" {
				t.Errorf("Got unexpected StatefulSet (-want +got):\n%s", diff)
			}
		})
	}
}
