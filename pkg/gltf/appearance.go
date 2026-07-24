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
// the bare-skin slots; SetHair swaps the head material's texture. The EXACT
// material-palette-index → body-slot mapping is unresolved (blocked on a live PINE
// memory read — see the RE doc), so the mapping here is a PROVISIONAL HEURISTIC:
// character body materials are taken in palette order and assigned slot = idx mod 5.
// This is meant to be visually spot-checked, not trusted as exact.
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

// matExtras is the per-material extras payload written when appearance is applied.
type matExtras struct {
	MatIndex  int  `json:"eqoaMatIndex"`  // palette order index
	SlotGuess int  `json:"eqoaSlotGuess"` // provisional body slot 0..4
	HairSlot  bool `json:"eqoaHairSlot,omitempty"`
	Bare      bool `json:"eqoaBareSkin,omitempty"` // received the skin tint
}

func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}
