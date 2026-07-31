package eqoa

import (
	"encoding/binary"
	"testing"
)

// buildMaterial assembles a material body in the layout the client's ParseMaterial reads:
// numLayers, then flags (version > 1), then emissive RGBA (version > 2), then fixed-size layers.
func buildMaterial(version int16, layers [][4]byte, texIDs []uint32) []byte {
	var b []byte
	u32 := func(v uint32) {
		var tmp [4]byte
		binary.LittleEndian.PutUint32(tmp[:], v)
		b = append(b, tmp[:]...)
	}
	u32(uint32(len(layers)))
	if version > 1 {
		u32(0) // flags
	}
	if version > 2 {
		u32(0) // emissive
	}
	for i, rgba := range layers {
		u32(0)         // layer flags
		u32(texIDs[i]) // texture DictID
		u32(1)         // wrap mode
		u32(2)         // blend mode
		b = append(b, rgba[:]...)
		b = append(b, make([]byte, 36)...) // UV matrix
		u32(0)                             // LOD bias
		b = append(b, make([]byte, 8)...)  // UV scroll rate
	}
	return b
}

// A two-layer v=3 material is what characters actually use. The old header layout read a DictID
// first, which shifted everything by four bytes, produced numLayers == 0, and dropped the second
// layer along with every colour.
func TestParseMaterialTwoLayerV3(t *testing.T) {
	body := buildMaterial(3,
		[][4]byte{{109, 83, 77, 255}, {255, 255, 255, 255}},
		[]uint32{0xd88fea71, 0x16c969f6})

	if len(body) != 12+2*layerStride {
		t.Fatalf("test fixture is %d bytes, expected %d", len(body), 12+2*layerStride)
	}

	m, err := ParseMaterialBody(body, 3, binary.LittleEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.Layers) != 2 {
		t.Fatalf("got %d layers, want 2 -- the second layer is being discarded", len(m.Layers))
	}
	if m.Layers[0].TexID != 0xd88fea71 || m.Layers[1].TexID != 0x16c969f6 {
		t.Errorf("texture IDs: got %08x, %08x", m.Layers[0].TexID, m.Layers[1].TexID)
	}
	// The Erudite skin modulate. This is the value whose loss produced a dark face on a pale
	// body, so it is asserted exactly rather than approximately.
	want := float32(109) / 255
	if got := m.Layers[0].Color[0]; got != want {
		t.Errorf("layer 0 red: got %v, want %v", got, want)
	}
	if m.Layers[0].Color[3] != 1 {
		t.Errorf("layer 0 alpha: got %v, want 1", m.Layers[0].Color[3])
	}
	if m.Layers[0].WrapMode != 1 || m.Layers[0].BlendMode != 2 {
		t.Errorf("wrap/blend not decoded: %d/%d", m.Layers[0].WrapMode, m.Layers[0].BlendMode)
	}
}

// A neutral single-layer material, which is what every unmodulated race wears.
func TestParseMaterialSingleLayerNeutral(t *testing.T) {
	body := buildMaterial(3, [][4]byte{{255, 255, 255, 255}}, []uint32{0x75bf0eb0})
	m, err := ParseMaterialBody(body, 3, binary.LittleEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.Layers) != 1 {
		t.Fatalf("got %d layers, want 1", len(m.Layers))
	}
	for i, c := range m.Layers[0].Color {
		if c != 1 {
			t.Errorf("component %d: got %v, want 1 (neutral white)", i, c)
		}
	}
}

// Zone materials reach the same function. A body that does not fit the character layout must
// fall through to the legacy path instead of being read as a huge layer count -- that fallback
// is the only thing keeping zone conversion working while the header is corrected.
func TestParseMaterialRejectsImplausibleLayerCount(t *testing.T) {
	body := make([]byte, 32)
	binary.LittleEndian.PutUint32(body[0:4], 0x7fffffff) // absurd count
	binary.LittleEndian.PutUint32(body[16:20], 0xdeadbeef)

	m, err := ParseMaterialBody(body, 3, binary.LittleEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.Layers) > 1 {
		t.Fatalf("absurd layer count was trusted: got %d layers", len(m.Layers))
	}
}

// A truncated body must not panic or over-read.
func TestParseMaterialTruncated(t *testing.T) {
	full := buildMaterial(3, [][4]byte{{1, 2, 3, 4}}, []uint32{0xabcdef01})
	for _, n := range []int{0, 1, 4, 8, 12, 20, len(full) - 1} {
		if _, err := ParseMaterialBody(full[:n], 3, binary.LittleEndian); err != nil {
			t.Errorf("length %d returned an error: %v", n, err)
		}
	}
}
