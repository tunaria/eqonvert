package gltf

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/average-bit/eqonvert/pkg/eqoa"
)

// TestJointNodesCarrySourceIndex pins the node-identity contract that 0x2500
// HSpriteAttachments records depend on. A record's NodeIndex is a joint index in
// the source 0x2400 hierarchy, so the export must let a consumer get back from
// that index to a glTF node — here by all three routes it is allowed to use:
// the skin's index-aligned joint list, the Joint_<i> name, and the node's
// explicit `extras.node_index`.
func TestJointNodesCarrySourceIndex(t *testing.T) {
	const n = 5
	verts := []eqoa.Vertex{
		{Pos: [3]float32{0, 0, 0}, Joints: [4]uint8{0, 0, 0, 0}, Weights: [4]uint8{255, 0, 0, 0}},
		{Pos: [3]float32{1, 0, 0}, Joints: [4]uint8{1, 0, 0, 0}, Weights: [4]uint8{255, 0, 0, 0}},
		{Pos: [3]float32{0, 1, 0}, Joints: [4]uint8{2, 0, 0, 0}, Weights: [4]uint8{255, 0, 0, 0}},
	}
	b := exportAndAddRoot(t, skinnedAsset(minimalJoints(n), verts))

	if len(b.Doc.Skins) != 1 {
		t.Fatalf("skins = %d, want 1", len(b.Doc.Skins))
	}
	joints := b.Doc.Skins[0].Joints
	if len(joints) != n {
		t.Fatalf("skin joints = %d, want %d", len(joints), n)
	}
	for i, nodeIdx := range joints {
		node := b.Doc.Nodes[nodeIdx]
		if want := fmt.Sprintf("Joint_%d", i); node.Name != want {
			t.Errorf("skin.joints[%d] -> node %d named %q, want %q", i, nodeIdx, node.Name, want)
		}
		var extras struct {
			NodeIndex *int `json:"node_index"`
		}
		if err := json.Unmarshal(node.Extras, &extras); err != nil {
			t.Fatalf("joint %d extras %q: %v", i, node.Extras, err)
		}
		if extras.NodeIndex == nil || *extras.NodeIndex != i {
			t.Errorf("joint %d extras node_index = %v, want %d", i, extras.NodeIndex, i)
		}
	}
}
