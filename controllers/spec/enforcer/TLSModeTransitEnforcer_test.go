package enforcer

import (
	"strings"
	"testing"

	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/ctx/log"
	"reactive-tech.io/kubegres/test/util"
)

//func TestTransitionMode(t *testing.T) {
//
//	cases := []struct {
//		name         string
//		originalSpec v1.KubegresSpec
//		desiredSpec  v1.KubegresSpec
//		currentMode  v1.SSLMode
//		wantSSLMode  v1.SSLMode
//		wantChanged  bool
//		wantOutput   string
//	}{
//		{
//			name: " < [allow] < require",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeRequire},
//			},
//			currentMode: v1.SSLModeDisable,
//			wantSSLMode: v1.SSLModeAllow,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer:  < [allow] < require\n",
//		},
//		{
//			name: " < allow < [require]",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeRequire},
//			},
//			currentMode: v1.SSLModeAllow,
//			wantSSLMode: v1.SSLModeRequire,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer:  < allow < [require]\n",
//		},
//		{
//			name: "disable < [allow] < require",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeDisable},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeRequire},
//			},
//			currentMode: v1.SSLModeDisable,
//			wantSSLMode: v1.SSLModeAllow,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer: disable < [allow] < require\n",
//		},
//		{
//			name: "disable < allow < [require]",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeDisable},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeRequire},
//			},
//			currentMode: v1.SSLModeAllow,
//			wantSSLMode: v1.SSLModeRequire,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer: disable < allow < [require]\n",
//		},
//		{
//			name: "disable < [allow] < verify-ca",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeDisable},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeVerifyCA},
//			},
//			currentMode: v1.SSLModeDisable,
//			wantSSLMode: v1.SSLModeAllow,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer: disable < [allow] < verify-ca\n",
//		},
//		{
//			name: "disable < allow < [verify-ca]",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeDisable},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeVerifyCA},
//			},
//			currentMode: v1.SSLModeAllow,
//			wantSSLMode: v1.SSLModeVerifyCA,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer: disable < allow < [verify-ca]\n",
//		},
//		{
//			name: "verify-ca > [allow] > disable",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeVerifyCA},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeDisable},
//			},
//			currentMode: v1.SSLModeVerifyCA,
//			wantSSLMode: v1.SSLModeAllow,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A lower SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer: verify-ca > [allow] > disable\n",
//		},
//		{
//			name: "verify-ca > allow > [disable]",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeVerifyCA},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeDisable},
//			},
//			currentMode: v1.SSLModeAllow,
//			wantSSLMode: v1.SSLModeDisable,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A lower SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer: verify-ca > allow > [disable]\n",
//		},
//		{
//			name: "disable < [allow]",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeDisable},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeAllow},
//			},
//			currentMode: v1.SSLModeDisable,
//			wantSSLMode: v1.SSLModeAllow,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer: disable < [allow]\n",
//		},
//		{
//			name: "verify-full > allow",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeVerifyFull},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeAllow},
//			},
//			currentMode: v1.SSLModeVerifyFull,
//			wantSSLMode: v1.SSLModeAllow,
//			wantChanged: true,
//			wantOutput: "TLSModeTransitEnforcer: A lower SSL mode is requested, running smooth transition.\n" +
//				"TLSModeTransitEnforcer: verify-full > [allow]\n",
//		},
//		{
//			name: "fist time, no current mode",
//			// first time, the original spec is previously set with the desired spec
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeVerifyCA},
//			},
//			// fist time, the current mode is previously set with the desired spec
//			currentMode: v1.SSLModeVerifyCA,
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeVerifyCA},
//			},
//			wantSSLMode: v1.SSLModeVerifyCA,
//			wantChanged: false,
//			wantOutput:  "TLSModeTransitEnforcer: Current SSL mode is the same as desired SSL mode, no transition needed.\n",
//		},
//		{
//			name: "no change, same desired and original",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeRequire},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeRequire},
//			},
//			currentMode: v1.SSLModeRequire,
//			wantSSLMode: v1.SSLModeRequire,
//			wantChanged: false,
//			wantOutput:  "TLSModeTransitEnforcer: Current SSL mode is the same as desired SSL mode, no transition needed.\n",
//		},
//		{
//			name: "change of same priority: require == verify-ca",
//			originalSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeRequire},
//			},
//			desiredSpec: v1.KubegresSpec{
//				TLS: v1.TLS{SSLMode: v1.SSLModeVerifyCA},
//			},
//			currentMode: v1.SSLModeRequire,
//			wantSSLMode: v1.SSLModeVerifyCA,
//			wantChanged: true,
//			wantOutput:  "TLSModeTransitEnforcer: Current SSL mode has the same priority as desired SSL mode, no transition needed.\n",
//		},
//	}
//
//	for _, tc := range cases {
//		t.Run(tc.name, func(t *testing.T) {
//			out := &strings.Builder{}
//			l := util.CreateMockLogger(out)
//			tls := TLSModeTransitEnforcer{
//				kubegresContext: ctx.KubegresContext{
//					Kubegres: &v1.Kubegres{Spec: tc.desiredSpec},
//					Log:      log.LogWrapper{Logger: l},
//					Status: &status.KubegresStatusWrapper{
//						Kubegres: &v1.Kubegres{
//							Status: v1.KubegresStatus{
//								TLSTransition: v1.TLSTransition{
//									OriginalState:      tc.originalSpec.TLS,
//									DesiredState:       tc.desiredSpec.TLS,
//									CurrentTransitMode: tc.currentMode,
//								},
//							},
//						},
//						Log: log.LogWrapper{Logger: l},
//					},
//				},
//			}
//
//			got, changed := tls.transitSSLMode()
//			if got != tc.wantSSLMode {
//				t.Fatalf("Expected SSL mode %s, got %s", tc.wantSSLMode, got)
//			}
//			if changed != tc.wantChanged {
//				t.Fatalf("Expected changed to be %v, got %v", tc.wantChanged, changed)
//			}
//			if out.String() != tc.wantOutput {
//				t.Fatalf("Expected output:\n%s\nGot:\n%s", tc.wantOutput, out.String())
//			}
//		})
//	}
//}

func TestTransitionMode(t *testing.T) {

	cases := []struct {
		name         string
		given        v1.KubegresSpec
		secure       v1.TLS
		insecure     v1.TLS
		currentMode  v1.SSLMode
		transitState v1.TLSTransitState
		wantSSLMode  v1.SSLMode
		wantTransit  v1.TLSTransitState
		wantChanged  bool
		wantOutput   string
	}{
		{
			name:        " < [allow] < require",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeRequire}},
			secure:      v1.TLS{SSLMode: v1.SSLModeRequire},
			currentMode: v1.SSLModeDisable,
			wantSSLMode: v1.SSLModeAllow,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to secure mode.\n" +
				"TLSModeTransitEnforcer: disable < allow\n",
		},
		{
			name:        " < allow < [require]",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeRequire}},
			secure:      v1.TLS{SSLMode: v1.SSLModeRequire},
			currentMode: v1.SSLModeAllow,
			wantSSLMode: v1.SSLModeRequire,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to secure mode.\n" +
				"TLSModeTransitEnforcer: allow < require\n",
		},
		{
			name:        "disable < [allow] < require",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeRequire}},
			secure:      v1.TLS{SSLMode: v1.SSLModeRequire},
			insecure:    v1.TLS{SSLMode: v1.SSLModeDisable},
			currentMode: v1.SSLModeDisable,
			wantSSLMode: v1.SSLModeAllow,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to secure mode.\n" +
				"TLSModeTransitEnforcer: disable < allow\n",
		},
		{
			name:        "disable < allow < [require]",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeRequire}},
			secure:      v1.TLS{SSLMode: v1.SSLModeRequire},
			insecure:    v1.TLS{SSLMode: v1.SSLModeDisable},
			currentMode: v1.SSLModeAllow,
			wantSSLMode: v1.SSLModeRequire,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to secure mode.\n" +
				"TLSModeTransitEnforcer: allow < require\n",
		},
		{
			name:        "disable < [allow] < verify-ca",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeVerifyCA}},
			secure:      v1.TLS{SSLMode: v1.SSLModeVerifyCA},
			insecure:    v1.TLS{SSLMode: v1.SSLModeDisable},
			currentMode: v1.SSLModeDisable,
			wantSSLMode: v1.SSLModeAllow,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to secure mode.\n" +
				"TLSModeTransitEnforcer: disable < allow\n",
		},
		{
			name:        "disable < allow < [verify-ca]",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeVerifyCA}},
			secure:      v1.TLS{SSLMode: v1.SSLModeVerifyCA},
			insecure:    v1.TLS{SSLMode: v1.SSLModeDisable},
			currentMode: v1.SSLModeAllow,
			wantSSLMode: v1.SSLModeVerifyCA,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to secure mode.\n" +
				"TLSModeTransitEnforcer: allow < verify-ca\n",
		},
		{
			name:        "verify-ca > [allow] > disable",
			given:       v1.KubegresSpec{},
			secure:      v1.TLS{SSLMode: v1.SSLModeVerifyCA},
			insecure:    v1.TLS{SSLMode: v1.SSLModeDisable},
			currentMode: v1.SSLModeVerifyCA,
			wantSSLMode: v1.SSLModeAllow,
			wantTransit: v1.TLSTransitStateToInsecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A lower SSL mode is requested, running smooth transition to insecure mode.\n" +
				"TLSModeTransitEnforcer: verify-ca > allow\n",
		},
		{
			name:        "verify-ca > allow > [disable]",
			given:       v1.KubegresSpec{},
			secure:      v1.TLS{SSLMode: v1.SSLModeVerifyCA},
			insecure:    v1.TLS{SSLMode: v1.SSLModeDisable},
			currentMode: v1.SSLModeAllow,
			wantSSLMode: v1.SSLModeDisable,
			wantTransit: v1.TLSTransitStateToInsecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A lower SSL mode is requested, running smooth transition to insecure mode.\n" +
				"TLSModeTransitEnforcer: allow > disable\n",
		},
		{
			name:        "disable < [allow]",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeAllow}},
			secure:      v1.TLS{SSLMode: v1.SSLModeAllow},
			insecure:    v1.TLS{SSLMode: v1.SSLModeDisable},
			currentMode: v1.SSLModeDisable,
			wantSSLMode: v1.SSLModeAllow,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A higher SSL mode is requested, running smooth transition to secure mode.\n" +
				"TLSModeTransitEnforcer: disable < allow\n",
		},
		{
			name:        "verify-full > allow",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeAllow}},
			secure:      v1.TLS{SSLMode: v1.SSLModeAllow},
			currentMode: v1.SSLModeVerifyFull,
			wantSSLMode: v1.SSLModeAllow,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput: "TLSModeTransitEnforcer: A lower SSL mode is requested, running smooth transition to secure mode.\n" +
				"TLSModeTransitEnforcer: verify-full > allow\n",
		},
		{
			name:        "no change, same ssl mode. secure",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeRequire}},
			secure:      v1.TLS{SSLMode: v1.SSLModeRequire},
			currentMode: v1.SSLModeRequire,
			wantSSLMode: v1.SSLModeRequire,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: false,
			wantOutput:  "TLSModeTransitEnforcer: Current SSL mode is the same as secure SSL mode, no transition needed.\n",
		},
		{
			name:        "no change, same ssl mode. insecure",
			given:       v1.KubegresSpec{TLS: v1.TLS{SSLMode: v1.SSLModeDisable}},
			insecure:    v1.TLS{SSLMode: v1.SSLModeDisable},
			currentMode: v1.SSLModeDisable,
			wantSSLMode: v1.SSLModeDisable,
			wantTransit: v1.TLSTransitStateToInsecure,
			wantChanged: false,
			wantOutput:  "TLSModeTransitEnforcer: Current SSL mode is the same as insecure SSL mode, no transition needed.\n",
		},
		{
			name:        "change of same priority: require == verify-ca",
			given:       v1.KubegresSpec{TLS: v1.TLS{Enabled: true, SSLMode: v1.SSLModeVerifyCA}},
			secure:      v1.TLS{SSLMode: v1.SSLModeVerifyCA},
			currentMode: v1.SSLModeRequire,
			wantSSLMode: v1.SSLModeVerifyCA,
			wantTransit: v1.TLSTransitStateToSecure,
			wantChanged: true,
			wantOutput:  "TLSModeTransitEnforcer: Current SSL mode has the same priority as secure SSL mode, no transition needed.\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &strings.Builder{}
			l := util.CreateMockLogger(out)
			tls := TLSModeTransitEnforcer{
				kubegresContext: ctx.KubegresContext{
					Kubegres: &v1.Kubegres{Spec: tc.given},
					Log:      log.LogWrapper{Logger: l},
				},
			}

			got, gotTransit, gotChanged := tls.transitSSLMode(v1.TLSTransition{
				CurrentTransitMode: tc.currentMode,
				SecureSpec:         tc.secure,
				InsecureSpec:       tc.insecure,
				TransitState:       tc.transitState,
			})
			if got != tc.wantSSLMode {
				t.Fatalf("Expected SSL mode %s, got %s", tc.wantSSLMode, got)
			}
			if gotTransit != tc.wantTransit {
				t.Fatalf("Expected transit state %s, got %s", tc.wantTransit, gotTransit)
			}
			if gotChanged != tc.wantChanged {
				t.Fatalf("Expected changed to be %v, got %v", tc.wantChanged, gotChanged)
			}
			if out.String() != tc.wantOutput {
				t.Fatalf("Expected output:\n%s\nGot:\n%s", tc.wantOutput, out.String())
			}
		})
	}
}
