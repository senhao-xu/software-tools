// Package cri centralizes Kubernetes CRI runtime socket selection.
package cri

const (
	ContainerdSocket = "unix:///var/run/containerd/containerd.sock"
	DockerdSocket    = "unix:///var/run/cri-dockerd.sock"
)

// Socket maps the runtime kind to its kubeadm CRI socket. Unknown values fall
// back to containerd so a stray flag value doesn't break install cleanup.
func Socket(runtime string) string {
	switch runtime {
	case "docker":
		return DockerdSocket
	default:
		return ContainerdSocket
	}
}
