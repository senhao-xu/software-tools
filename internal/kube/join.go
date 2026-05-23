// Package kube — PR8: kubeadm join for worker nodes.
//
// Join runs the kubeadm-join pipeline:
//
//  1. validate the master endpoint, bootstrap token, and discovery CA hash —
//     these three are mandatory; we surface a clear error instead of letting
//     kubeadm fail with a less obvious message.
//  2. `kubeadm join <master> --token=... --discovery-token-ca-cert-hash=...
//     --cri-socket=<sock>`, where the cri-socket is selected by the same
//     criSocket helper as init.go so worker and master stay on the same
//     runtime.
//
// ResetJoin is the inverse for the rollback chain: best-effort `kubeadm reset`
// using the matching cri-socket. As with ResetInit it deliberately does *not*
// touch /etc/kubernetes — detect.Cleanup owns that.

package kube

import (
	"context"
	"errors"
	"fmt"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// JoinOptions controls kubeadm join behaviour for a worker node.
type JoinOptions struct {
	// Runtime is "containerd" or "docker"; selects the cri-socket via the
	// same criSocket() helper Init uses.
	Runtime string

	// Master is the control-plane endpoint passed to kubeadm join, e.g.
	// "10.0.0.10:6443" or "master.example.com:6443". Format is not pre-
	// validated; kubeadm itself will report a clearer error if malformed.
	Master string

	// Token is the kubeadm bootstrap token (the value after `--token=`
	// from the master's `kubeadm token create --print-join-command`).
	Token string

	// DiscoveryTokenCACertHash is the `--discovery-token-ca-cert-hash`
	// value (full `sha256:...` form) from the master's join command.
	DiscoveryTokenCACertHash string
}

// Join runs the kubeadm-join pipeline described in the package comment.
// Failure bubbles up so the caller can chain ResetJoin followed by the rest of
// the rollback chain (kube.Rollback -> runtime.Rollback -> sysprep.Rollback).
func Join(_ context.Context, opts JoinOptions) error {
	log.Info("kubejoin: starting kubeadm join")

	if err := validateJoinOptions(opts); err != nil {
		return err
	}

	if err := runKubeadmJoin(opts); err != nil {
		return err
	}

	log.Info("kubejoin: worker joined cluster")
	log.Info("kubejoin: verify on master with 'kubectl get nodes'")
	return nil
}

// ResetJoin best-effort reverses Join. As in ResetInit, we tolerate the
// "nothing-to-reset" case because rollback runs after partial-success too.
// The cri-socket must match the runtime so kubeadm reset knows which CRI
// endpoint to talk to.
func ResetJoin(_ context.Context, opts JoinOptions) error {
	log.Info("kubejoin: rollback")

	sock := criSocket(opts.Runtime)
	if err := xexec.Run("kubeadm", "reset", "-f", "--cri-socket="+sock); err != nil {
		log.Warn("kubeadm reset: %v", err)
	}

	log.Info("kubejoin: rollback done")
	return nil
}

// validateJoinOptions enforces the three required fields. We intentionally do
// not parse the Master endpoint format (host:port vs IP:port) — kubeadm's own
// error message is sufficient and we don't want to reject valid edge cases
// (IPv6 literals, DNS names with custom ports, etc.).
func validateJoinOptions(opts JoinOptions) error {
	if opts.Master == "" {
		return errors.New("master endpoint required")
	}
	if opts.Token == "" {
		return errors.New("token required")
	}
	if opts.DiscoveryTokenCACertHash == "" {
		return errors.New("discovery-token-ca-cert-hash required")
	}
	return nil
}

// runKubeadmJoin assembles and executes the kubeadm join command. cri-socket
// is sourced from the same criSocket() helper init.go uses so master and
// worker land on the same runtime.
func runKubeadmJoin(opts JoinOptions) error {
	sock := criSocket(opts.Runtime)
	args := []string{
		"join", opts.Master,
		"--token=" + opts.Token,
		"--discovery-token-ca-cert-hash=" + opts.DiscoveryTokenCACertHash,
		"--cri-socket=" + sock,
	}
	if err := xexec.Run("kubeadm", args...); err != nil {
		return fmt.Errorf("kubeadm join: %w", err)
	}
	return nil
}
