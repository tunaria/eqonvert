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

// writeAttachmentSidecar emits PREFIX_..._attach.json beside the model GLB,
// carrying the asset's 0x2500 HSpriteAttachments records plus the node-index →
// glTF-node mapping needed to resolve them. The records are the only placement
// information a held item gets: the engine applies no extra rotation or offset,
// the item simply inherits the named node's transform.
//
// glbPath is the .glb just written; it names the sidecar and is echoed in the
// payload. No file is written when the asset has no attachments.
func writeAttachmentSidecar(asset *eqoa.Asset, b *gltf.Builder, glbPath string) error {
	if len(asset.Attachments) == 0 {
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

	payload := map[string]any{
		"dict_id":     fmt.Sprintf("0x%08X", asset.ID),
		"model":       filepath.Base(glbPath),
		"note":        "0x2500 HSpriteAttachments. attach=node: parent the resource to gltf_node and inherit its transform — the engine applies no extra rotation or offset. attach=skin: the resource is skinned to the whole hierarchy (node_index -1).",
		"joints":      joints,
		"attachments": records,
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
