package kube

import (
	"reflect"
	"testing"
)

func TestCRISocket(t *testing.T) {
	tests := []struct {
		name    string
		runtime string
		want    string
	}{
		{"docker", "docker", criDockerdSocket},
		{"containerd", "containerd", containerdSocket},
		{"empty defaults to containerd", "", containerdSocket},
		{"unknown defaults to containerd", "podman", containerdSocket},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := criSocket(tc.runtime); got != tc.want {
				t.Errorf("criSocket(%q) = %q, want %q", tc.runtime, got, tc.want)
			}
		})
	}
}

func TestKubeadmImagePullArgsIncludesSelectedCRISocket(t *testing.T) {
	tests := []struct {
		name string
		opts InitOptions
		want []string
	}{
		{
			name: "docker uses cri-dockerd socket",
			opts: InitOptions{Runtime: "docker", Version: "v1.35.0"},
			want: []string{
				"config",
				"images",
				"pull",
				"--kubernetes-version=v1.35.0",
				"--cri-socket=" + criDockerdSocket,
			},
		},
		{
			name: "containerd cn mirror keeps image repository",
			opts: InitOptions{Runtime: "containerd", Version: "v1.35.0", Mirror: "cn"},
			want: []string{
				"config",
				"images",
				"pull",
				"--kubernetes-version=v1.35.0",
				"--cri-socket=" + containerdSocket,
				"--image-repository=" + cnImageRepository,
			},
		},
		{
			name: "empty runtime defaults to containerd socket",
			opts: InitOptions{Version: "v1.35.0"},
			want: []string{
				"config",
				"images",
				"pull",
				"--kubernetes-version=v1.35.0",
				"--cri-socket=" + containerdSocket,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := kubeadmImagePullArgs(tc.opts); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("kubeadmImagePullArgs() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKubeadmInitArgsIncludesFullSelectedParameters(t *testing.T) {
	opts := InitOptions{
		Runtime:     "docker",
		Version:     "v1.35.0",
		ServiceCIDR: "10.96.0.0/12",
		PodCIDR:     "10.244.0.0/16",
		Mirror:      "cn",
	}
	want := []string{
		"init",
		"--kubernetes-version=v1.35.0",
		"--service-cidr=10.96.0.0/12",
		"--pod-network-cidr=10.244.0.0/16",
		"--cri-socket=" + criDockerdSocket,
		"--image-repository=" + cnImageRepository,
		"--apiserver-advertise-address=172.25.138.69",
	}
	if got := kubeadmInitArgs(opts, "172.25.138.69"); !reflect.DeepEqual(got, want) {
		t.Errorf("kubeadmInitArgs() = %v, want %v", got, want)
	}
}
