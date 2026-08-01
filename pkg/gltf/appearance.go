package gltf

import (
	"encoding/json"
	"fmt"
	"image"
)

// AppearanceSpec carries a resolved CHARCUST appearance to bake onto a character
// model at export time. It is OPT-IN: ExportAssetToBuilder only consults it when
// a non-nil spec is passed (via --apply-appearance), so the default export path
// is byte-for-byte unchanged.
//
// Slot model (from docs/ARMOR_TEXTURE_MAPPING.md + CHAR_CREATION.md §1): the body
// is a single VISkinSprite with FIVE VICSpriteTextSlot material slots (the applier
// loops iVar8 = 0..4). SetArmorSet swaps each slot's texture; a tint RGBA multiplies
// the bare-skin slots; SetHair swaps the head material's texture.
//
// EXPERIMENTAL — THE SLOT MAPPING BELOW IS KNOWN TO BE WRONG. Materials are taken in
// palette order and assigned slot = idx mod 5. That cannot be right: material indices
// are not stable between races (the legs are material 0 on one race and 5 or 6 on
// others), so no ordinal rule holds, and the body resolves into more regions than five
// — head, chest, legs, feet, hand and bracer, with left and right addressed separately.
//
// This was written believing the true mapping was blocked on a live memory read. It is
// not: it can be derived from the exported geometry, by measuring vertical placement
// against the shoulder line and finding the arms as a mirrored pair with no midline
// geometry. Rebuilding this on that derivation is the fix; until then the flags are
// hidden and nothing should depend on where these textures land.
type AppearanceSpec struct {
	Race     string // e.g. "erudite" (echoed into extras; not used for slot math)
	ArmorSet int    // 0 = bare (no armor textures applied); 1..8 = a CHARCUST set
	HairIdx  int    // 0..7 hair texture index
	TintIdx  int    // 0..14 tint palette index

	// ArmorTextures[slot] is the decoded CHARCUST image for body slot 0..4 of the
	// chosen ArmorSet, or nil when ArmorSet==0 / the surface was missing.
	ArmorTextures [5]image.Image
	// HairTexture is the decoded CHARCUST/CHAR hair image, or nil if missing.
	HairTexture image.Image
	// Tint is the RGBA (0..255) tint applied as BaseColorFactor on bare-skin slots.
	Tint [4]uint8

	// DictIDs recorded for the sidecar / root-extras appearance mapping.
	ArmorDictIDs [5]uint32
	HairDictID   uint32
}

// AppearanceMapping is the JSON structure emitted into the root glTF extras (and
// an optional sidecar) so a downstream viewer can re-apply CHARCUST at runtime.
type AppearanceMapping struct {
	Race         string    `json:"race"`
	ArmorSet     int       `json:"armorSet"`
	HairIdx      int       `json:"hairIdx"`
	TintIdx      int       `json:"tintIdx"`
	Tint         [4]uint8  `json:"tint"`
	HairDictID   string    `json:"hairDictID"`
	ArmorDictIDs [5]string `json:"armorDictIDs"`
	// SlotHeuristic documents the provisional material→slot mapping so consumers
	// know it is best-effort, not ground truth.
	SlotHeuristic string `json:"slotHeuristic"`
}

const slotHeuristicNote = "provisional: character body materials taken in palette order, slot = matIndex mod 5 (exact mapping blocked on live memory read; see docs/ARMOR_TEXTURE_MAPPING.md)"

// Mapping builds the AppearanceMapping for this spec.
func (a *AppearanceSpec) Mapping() AppearanceMapping {
	m := AppearanceMapping{
		Race:          a.Race,
		ArmorSet:      a.ArmorSet,
		HairIdx:       a.HairIdx,
		TintIdx:       a.TintIdx,
		Tint:          a.Tint,
		HairDictID:    fmt.Sprintf("0x%X", a.HairDictID),
		SlotHeuristic: slotHeuristicNote,
	}
	for i := 0; i < 5; i++ {
		m.ArmorDictIDs[i] = fmt.Sprintf("0x%X", a.ArmorDictIDs[i])
	}
	return m
}

// matExtras is the per-material extras payload.
//
// It exists because glTF cannot express everything an EQOA material says, and the parts it
// cannot express were previously just lost. A material carries a blend mode drawn from a
// five-value enum, a wrap mask, and up to sixteen texture layers; glTF has alphaMode, a
// sampler, and one base-colour texture. Rather than silently discard the remainder, every
// material records what the source actually stated, so a downstream consumer can reproduce
// effects this exporter cannot -- and so the loss is auditable instead of invisible.
//
// See docs/MATERIAL_BLEND_MODES.md for the decoded meaning of each value.
type matExtras struct {
	// Source facts, written for every material.
	BlendMode  int `json:"eqoaBlendMode"`
	WrapMode   int `json:"eqoaWrapMode"`
	LayerCount int `json:"eqoaLayerCount"`
	// LayerColor is layer 0's RGBA modulate, 0-255. This is the per-race skin tone: the flesh
	// textures are shared, and the palette carries the colour (Erudite 109,83,77; Barbarian
	// 237,219,188; Human neutral). It is recorded but NOT applied as baseColorFactor -- doing so
	// regressed Gnome and Ogre, see emitSkinModulate -- so a downstream consumer that wants to
	// experiment with it needs the number, and previously had nowhere to get it.
	LayerColor  [4]float32   `json:"eqoaLayerColor"`
	ExtraLayers []extraLayer `json:"eqoaExtraLayers,omitempty"`

	// Appearance, written only under --apply-appearance. Pointers because slot 0 and material
	// index 0 are both meaningful, so omitempty on a plain int would erase a real value.
	MatIndex  *int `json:"eqoaMatIndex,omitempty"`  // palette order index
	SlotGuess *int `json:"eqoaSlotGuess,omitempty"` // provisional body slot 0..4
	HairSlot  bool `json:"eqoaHairSlot,omitempty"`
	Bare      bool `json:"eqoaBareSkin,omitempty"` // received the skin tint
}

// extraLayer records a layer beyond the first -- the ones glTF has nowhere to put. Their
// textures are already embedded in the GLB (they are referenced by the surface table), so this
// costs only the descriptor and makes otherwise-unreachable artwork addressable.
type extraLayer struct {
	TexID     string     `json:"texId"`
	BlendMode int        `json:"blendMode"`
	WrapMode  int        `json:"wrapMode"`
	Color     [4]float32 `json:"color"`
}

func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}
