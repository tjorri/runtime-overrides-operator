// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	"sort"

	v1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
	"github.com/tjorri/runtime-overrides-operator/internal/merge"
)

// computePeers returns the ContributingPeers slice for a given CR — every
// *other* validated CR targeting the same tenantId. Used in status to make
// "who else is shaping this tenant's overrides?" visible without needing
// cluster-wide kubectl access.
//
// The list is sorted by (Weight ASC, Namespace ASC, Name ASC) — same key
// the merge engine uses — so users reading status see peers in the order
// their contributions are layered.
func computePeers(self merge.Override, all []merge.Override) []v1alpha1.ContributingPeer {
	var peers []v1alpha1.ContributingPeer
	for _, o := range all {
		if o.TenantId != self.TenantId {
			continue
		}
		if o.Namespace == self.Namespace && o.Name == self.Name {
			continue
		}
		peers = append(peers, v1alpha1.ContributingPeer{
			Namespace: o.Namespace,
			Name:      o.Name,
			Weight:    o.Weight,
		})
	}
	sort.SliceStable(peers, func(i, j int) bool {
		if peers[i].Weight != peers[j].Weight {
			return peers[i].Weight < peers[j].Weight
		}
		if peers[i].Namespace != peers[j].Namespace {
			return peers[i].Namespace < peers[j].Namespace
		}
		return peers[i].Name < peers[j].Name
	})
	return peers
}

// peersChanged returns true when two ContributingPeers slices differ.
// Used as part of the status diff to avoid spurious updates.
func peersChanged(a, b []v1alpha1.ContributingPeer) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}
