package kube

import (
	"reflect"
	"testing"
)

func TestKubeadmJoinArgsIncludesFullSelectedParameters(t *testing.T) {
	opts := JoinOptions{
		Runtime:                  "docker",
		Master:                   "172.25.138.69:6443",
		Token:                    "abcdef.0123456789abcdef",
		DiscoveryTokenCACertHash: "sha256:0123456789abcdef",
	}
	want := []string{
		"join", "172.25.138.69:6443",
		"--token=abcdef.0123456789abcdef",
		"--discovery-token-ca-cert-hash=sha256:0123456789abcdef",
		"--cri-socket=" + criDockerdSocket,
	}
	if got := kubeadmJoinArgs(opts); !reflect.DeepEqual(got, want) {
		t.Errorf("kubeadmJoinArgs() = %v, want %v", got, want)
	}
}
