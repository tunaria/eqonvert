package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/average-bit/eqonvert/pkg/eqoa"
	"github.com/average-bit/eqonvert/pkg/gltf"
)

// attachSidecarSuffix replaces the ".glb" of the exported model to name its
// attachment sidecar, so the two files sort next to each other and share a stem.
const attachSidecarSuffix = "_attach.json"

// attachJointJSON maps one source hierarchy node to the glTF node that carries it.
type attachJointJSON struct {
	NodeIndex int    `json:"node_index"`
	Name      string `json:"name"`
	GLTFNode  *int   `json:"gltf_node,omitempty"`
}

// attachRecordJSON is one 0x2500 record, resolved against the exported skeleton.
type attachRecordJSON struct {
	Type      int32  `json:"type"`
	Resource  string `json:"resource"`
	DictID    string `json:"dict_id"`
	NodeIndex int32  `json:"node_index"`
	Attach    string `json:"attach"` // "node" or "skin"
	NodeName  string `json:"node_name,omitempty"`
	GLTFNode  *int   `json:"gltf_node,omitempty"`
}

// attachSlotJSON is one 0x2920 CSpriteASlotList record — the slot → joint
// binding that places a held weapon or shield on a character — resolved against
// the exported skeleton.
type attachSlotJSON struct {
	Slot      int32  `json:"slot"`
	NodeIndex int32  `json:"node_index"`
	NodeName  string `json:"node_name,omitempty"`
	GLTFNode  *int   `json:"gltf_node,omitempty"`
}

// writeAttachmentSidecar emits PREFIX_..._attach.json beside the model GLB,
// carrying the asset's attach records plus the node-index → glTF-node mapping
// needed to resolve them. Those records are the only placement information a
// held item gets: the engine applies no extra rotation or offset, the item
// simply inherits the named node's transform.
//
// Two disjoint kinds appear, one per sprite kind: an HSprite's 0x2500
// HSpriteAttachments (which resource hangs off which joint) and a CSprite's
// 0x2920 CSpriteASlotList (which joint each item-attach slot binds to). They
// share the joints table, so both go in the one sidecar.
//
// glbPath is the .glb just written; it names the sidecar and is echoed in the
// payload. No file is written when the asset has neither kind of record.
func writeAttachmentSidecar(asset *eqoa.Asset, b *gltf.Builder, glbPath string) error {
	if len(asset.Attachments) == 0 && len(asset.AttachSlots) == 0 {
		return nil
	}

	// The Skin's joint list is index-aligned with the 0x2400 hierarchy (see
	// ExportAssetToBuilder), so jointNodes[nodeIndex] is the glTF node index.
	// It is absent only when the hierarchy failed to parse and no skin was
	// emitted — the records are still worth writing, just unresolved.
	var jointNodes []int
	if len(b.Doc.Skins) > 0 {
		jointNodes = b.Doc.Skins[0].Joints
	}
	gltfNode := func(nodeIndex int32) *int {
		if nodeIndex < 0 || int(nodeIndex) >= len(jointNodes) {
			return nil
		}
		n := jointNodes[nodeIndex]
		return &n
	}

	joints := make([]attachJointJSON, 0, len(jointNodes))
	for i := range jointNodes {
		joints = append(joints, attachJointJSON{
			NodeIndex: i,
			Name:      gltf.JointNodeName(i),
			GLTFNode:  gltfNode(int32(i)),
		})
	}

	records := make([]attachRecordJSON, 0, len(asset.Attachments))
	for _, a := range asset.Attachments {
		rec := attachRecordJSON{
			Type:      a.Type,
			Resource:  a.ResourceName(),
			DictID:    fmt.Sprintf("0x%08X", a.DictID),
			NodeIndex: a.NodeIndex,
			Attach:    "node",
		}
		if a.IsSkin() {
			rec.Attach = "skin"
		} else {
			rec.NodeName = gltf.JointNodeName(int(a.NodeIndex))
			rec.GLTFNode = gltfNode(a.NodeIndex)
		}
		records = append(records, rec)
	}

	slots := make([]attachSlotJSON, 0, len(asset.AttachSlots))
	for _, s := range asset.AttachSlots {
		rec := attachSlotJSON{Slot: s.Slot, NodeIndex: s.NodeIndex}
		if s.HasNode() {
			rec.NodeName = gltf.JointNodeName(int(s.NodeIndex))
			rec.GLTFNode = gltfNode(s.NodeIndex)
		}
		slots = append(slots, rec)
	}

	payload := map[string]any{
		"dict_id": fmt.Sprintf("0x%08X", asset.ID),
		"model":   filepath.Base(glbPath),
		"joints":  joints,
	}
	if len(records) > 0 {
		payload["note"] = "0x2500 HSpriteAttachments. attach=node: parent the resource to gltf_node and inherit its transform — the engine applies no extra rotation or offset. attach=skin: the resource is skinned to the whole hierarchy (node_index -1)."
		payload["attachments"] = records
	}
	if len(slots) > 0 {
		payload["slots_note"] = "0x2920 CSpriteASlotList — how a character holds an item. Parent the item to gltf_node and inherit its transform; the engine applies no extra rotation or offset. slot is the raw VICSpriteAttachSlot index (0..2); the enumerator names are not recoverable from the shipped binary, so none are invented here. Slots 0 and 1 are the weapon-capable ones (SetAttackAction rejects slot >= 2) and bind to opposite sides of the body; slot 2 sits inboard on the same side as slot 1. node_index -1 means the slot has no attach point."
		payload["attach_slots"] = slots
	}

	path := strings.TrimSuffix(glbPath, ".glb") + attachSidecarSuffix
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", " ")
	err = enc.Encode(payload)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
