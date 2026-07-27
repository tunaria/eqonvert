package cmd

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/average-bit/eqonvert/pkg/eqoa"
	"github.com/average-bit/eqonvert/pkg/gltf"
)

// attachTestAsset builds a 4-joint skinned asset carrying one node attachment
// and one skin attachment.
func attachTestAsset() *eqoa.Asset {
	joints := make([]eqoa.Joint, 4)
	for i := range joints {
		joints[i] = eqoa.Joint{ParentIndex: -1, Rotation: [4]float32{0, 0, 0, 1}, Scale: 1}
	}
	return &eqoa.Asset{
		ID:        0xABCD1234,
		Hierarchy: &eqoa.HSpriteHierarchy{Joints: joints},
		Meshes: []*eqoa.Mesh{{
			Type: 5,
			FaceGroups: []eqoa.FaceGroup{{
				Vertices: []eqoa.Vertex{
					{Pos: [3]float32{0, 0, 0}, Weights: [4]uint8{255}},
					{Pos: [3]float32{1, 0, 0}, Weights: [4]uint8{255}},
					{Pos: [3]float32{0, 1, 0}, Weights: [4]uint8{255}},
				},
				Indices: []uint32{0, 1, 2},
			}},
		}},
		Attachments: []eqoa.HSpriteAttachment{
			{Type: 0, DictID: 0x0BADF00D, NodeIndex: 2},
			{Type: 1, DictID: 0x0000BEEF, NodeIndex: -1},
		},
	}
}

func TestWriteAttachmentSidecar(t *testing.T) {
	asset := attachTestAsset()
	b := gltf.NewBuilder()
	rootIdx, err := gltf.ExportAssetToBuilder(b, bytes.NewReader(nil), asset, binary.LittleEndian, nil, true)
	if err != nil {
		t.Fatalf("ExportAssetToBuilder: %v", err)
	}
	b.AddSceneNode(rootIdx)

	glbPath := filepath.Join(t.TempDir(), "ITEM_0xABCD1234.glb")
	if err := writeAttachmentSidecar(asset, b, glbPath); err != nil {
		t.Fatalf("writeAttachmentSidecar: %v", err)
	}

	sidecar := filepath.Join(filepath.Dir(glbPath), "ITEM_0xABCD1234"+attachSidecarSuffix)
	data, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	var got struct {
		DictID string `json:"dict_id"`
		Model  string `json:"model"`
		Joints []struct {
			NodeIndex int    `json:"node_index"`
			Name      string `json:"name"`
			GLTFNode  *int   `json:"gltf_node"`
		} `json:"joints"`
		Attachments []struct {
			Type      int32  `json:"type"`
			Resource  string `json:"resource"`
			DictID    string `json:"dict_id"`
			NodeIndex int32  `json:"node_index"`
			Attach    string `json:"attach"`
			NodeName  string `json:"node_name"`
			GLTFNode  *int   `json:"gltf_node"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("sidecar is not valid JSON: %v", err)
	}

	if got.DictID != "0xABCD1234" {
		t.Errorf("dict_id = %q", got.DictID)
	}
	if got.Model != "ITEM_0xABCD1234.glb" {
		t.Errorf("model = %q", got.Model)
	}
	if len(got.Joints) != 4 {
		t.Fatalf("joints = %d, want 4", len(got.Joints))
	}

	// Every joint entry must point at the glTF node actually named for it.
	for i, j := range got.Joints {
		if j.NodeIndex != i {
			t.Errorf("joints[%d].node_index = %d", i, j.NodeIndex)
		}
		if j.GLTFNode == nil {
			t.Fatalf("joints[%d].gltf_node missing", i)
		}
		if name := b.Doc.Nodes[*j.GLTFNode].Name; name != j.Name {
			t.Errorf("joints[%d] points at node %d named %q, sidecar says %q", i, *j.GLTFNode, name, j.Name)
		}
	}

	if len(got.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(got.Attachments))
	}

	node := got.Attachments[0]
	if node.Attach != "node" || node.NodeIndex != 2 || node.NodeName != "Joint_2" {
		t.Errorf("node attachment = %+v", node)
	}
	if node.Resource != "SimpleSprite" || node.DictID != "0x0BADF00D" {
		t.Errorf("node attachment resource/dict = %q/%q", node.Resource, node.DictID)
	}
	if node.GLTFNode == nil || *node.GLTFNode != *got.Joints[2].GLTFNode {
		t.Errorf("node attachment gltf_node = %v, want joint 2's node %v", node.GLTFNode, got.Joints[2].GLTFNode)
	}

	skin := got.Attachments[1]
	if skin.Attach != "skin" || skin.NodeIndex != -1 {
		t.Errorf("skin attachment = %+v", skin)
	}
	if skin.Resource != "SkinSubSprite" || skin.DictID != "0x0000BEEF" {
		t.Errorf("skin attachment resource/dict = %q/%q", skin.Resource, skin.DictID)
	}
	if skin.GLTFNode != nil || skin.NodeName != "" {
		t.Errorf("skin attachment should not name a node, got %+v", skin)
	}
}

// An asset with no attachments must not leave an empty sidecar behind.
func TestWriteAttachmentSidecarSkipsWhenEmpty(t *testing.T) {
	asset := attachTestAsset()
	asset.Attachments = nil

	b := gltf.NewBuilder()
	if _, err := gltf.ExportAssetToBuilder(b, bytes.NewReader(nil), asset, binary.LittleEndian, nil, true); err != nil {
		t.Fatalf("ExportAssetToBuilder: %v", err)
	}

	dir := t.TempDir()
	glbPath := filepath.Join(dir, "ITEM_0xABCD1234.glb")
	if err := writeAttachmentSidecar(asset, b, glbPath); err != nil {
		t.Fatalf("writeAttachmentSidecar: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no files, got %v", entries)
	}
}
