// Package tracker provides status tracking functionality for ModelValidation resources
package tracker

import (
	"k8s.io/apimachinery/pkg/types"
)

// PodMapping manages bidirectional mapping between pod names and UIDs
type PodMapping struct {
	nameToUID map[types.NamespacedName]types.UID
	uidToName map[types.UID]types.NamespacedName
}

// NewPodMapping creates a new bidirectional pod mapping
func NewPodMapping() *PodMapping {
	return &PodMapping{
		nameToUID: make(map[types.NamespacedName]types.UID),
		uidToName: make(map[types.UID]types.NamespacedName),
	}
}

// AddPod adds a pod to the mapping
func (pm *PodMapping) AddPod(name types.NamespacedName, uid types.UID) {
	// Remove any existing mappings for this name or UID
	pm.removePod(name, uid)

	pm.nameToUID[name] = uid
	pm.uidToName[uid] = name
}

// GetUIDByName returns the UID for a given pod name
func (pm *PodMapping) GetUIDByName(name types.NamespacedName) (types.UID, bool) {
	uid, exists := pm.nameToUID[name]
	return uid, exists
}

// GetNameByUID returns the name for a given pod UID
func (pm *PodMapping) GetNameByUID(uid types.UID) (types.NamespacedName, bool) {
	name, exists := pm.uidToName[uid]
	return name, exists
}

// RemovePodsByName removes multiple pod mappings by name
func (pm *PodMapping) RemovePodsByName(names ...types.NamespacedName) {
	if len(names) == 0 {
		return
	}

	for _, name := range names {
		if uid, exists := pm.nameToUID[name]; exists {
			pm.removePod(name, uid)
		}
	}
}

// RemovePodByUID removes a pod mapping by UID
func (pm *PodMapping) RemovePodByUID(uid types.UID) bool {
	name, exists := pm.uidToName[uid]
	if exists {
		pm.removePod(name, uid)
	}
	return exists
}

// removePod removes mappings
func (pm *PodMapping) removePod(name types.NamespacedName, uid types.UID) {
	delete(pm.nameToUID, name)
	delete(pm.uidToName, uid)
}

// GetAllPodNames returns all tracked pod names
func (pm *PodMapping) GetAllPodNames() []types.NamespacedName {
	names := make([]types.NamespacedName, 0, len(pm.nameToUID))
	for name := range pm.nameToUID {
		names = append(names, name)
	}
	return names
}

// Clear removes all mappings
func (pm *PodMapping) Clear() {
	pm.nameToUID = make(map[types.NamespacedName]types.UID)
	pm.uidToName = make(map[types.UID]types.NamespacedName)
}
