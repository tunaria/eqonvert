package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/average-bit/eqonvert/pkg/eqoa"
	"github.com/average-bit/eqonvert/pkg/gltf"
)

// appearanceAssetRoot is the root of the already-extracted textures (the
// EQOAF_OUTPUT tree). appearance_tables.json entries carry a `file` path relative
// to it, so we can load the exact texture PNGs directly — independent of which CSF
// the input model came from. Overridable via --appearance-assets.
var appearanceAssetRoot = "/Users/justinjanes/Development/EQOAF_OUTPUT"

// loadAppearanceTexture resolves a texture: first from the pre-extracted PNG (the
// `file` field, joined with appearanceAssetRoot), then falling back to a Surface
// decoded from the CHARCUST source registry.
func loadAppearanceTexture(file string, reg *eqoa.SurfaceRegistry, id uint32) (image.Image, error) {
	if file != "" && appearanceAssetRoot != "" {
		if f, err := os.Open(filepath.Join(appearanceAssetRoot, file)); err == nil {
			defer f.Close()
			if img, _, err := image.Decode(f); err == nil {
				return img, nil
			}
		}
	}
	if surf, ok := reg.Get(id); ok {
		return surf.ToImage(0)
	}
	return nil, fmt.Errorf("not in extracted assets (%s) or CHARCUST sources", file)
}

// appearance_tables.json is copied from eqoa-xr/tools/ui-loop and embedded so the
// converter is self-contained. It maps race → faces, and provides the shared hair
// (8), robe (8), armorSets (8×5 slots) and tints (15 RGBA) tables. See the
// --apply-appearance flag and docs/ARMOR_TEXTURE_MAPPING.md.
//
//go:embed appearance_tables.json
var appearanceTablesJSON []byte

// --- CLI flag state (registered in convert.go init) ---
var (
	applyAppearanceRace string // "" = disabled; else a race name (e.g. "erudite")
	appearanceArmorSet  int
	appearanceHair      int
	appearanceTint      int
)

// dictEntry is one {dictID, file} record in the appearance tables.
type dictEntry struct {
	DictID string `json:"dictID"`
	File   string `json:"file"`
}

// appearanceTables mirrors appearance_tables.json (only the fields we consume).
type appearanceTables struct {
	Tints [][4]int `json:"tints"`
	Hair  map[string]dictEntry
	// armorSets: "1".."8" → slot "0".."4" → entry. Set 0 (bare) is implied absent.
	ArmorSets map[string]map[string]dictEntry `json:"armorSets"`
	// faces: race → face → gender → entry (unused for now; race presence validates
	// the --apply-appearance <race> argument).
	Faces map[string]map[string]map[string]dictEntry `json:"faces"`
}

func loadAppearanceTables() (*appearanceTables, error) {
	var t appearanceTables
	if err := json.Unmarshal(appearanceTablesJSON, &t); err != nil {
		return nil, fmt.Errorf("parsing embedded appearance_tables.json: %w", err)
	}
	return &t, nil
}

// parseDictID parses a "0x...." hex DictID string.
func parseDictID(s string) (uint32, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

// raceKey returns the tables' race key matching the user's --apply-appearance
// argument, case-insensitively (e.g. "erudite" → "Erudite"). Empty if unknown.
func (t *appearanceTables) raceKey(name string) string {
	for k := range t.Faces {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	return ""
}

// buildAppearanceRegistry populates a SurfaceRegistry from the CHARCUST source
// files (CHARCUST.CSF, CHARFACE.CSF, and CHAR.ESF/CHAR.CSF for hair) found as
// siblings of the input path. It is intentionally separate from the main
// conversion registry so appearance textures are resolvable even when the input
// itself is a single CHARSEL body file. Missing sources are skipped (the caller
// reports which DictIDs failed to resolve).
func buildAppearanceRegistry(inputPath string) *eqoa.SurfaceRegistry {
	reg := eqoa.NewSurfaceRegistry()
	dir := inputPath
	if fi, err := os.Stat(inputPath); err == nil && !fi.IsDir() {
		dir = filepath.Dir(inputPath)
	}
	// Candidate source basenames (case-insensitive match against the dir listing).
	wanted := map[string]bool{
		"CHARCUST.CSF": true, "CHARCUST.ESF": true,
		"CHARFACE.CSF": true, "CHARFACE.ESF": true,
		"CHAR.CSF": true, "CHAR.ESF": true,
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return reg
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !wanted[strings.ToUpper(e.Name())] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		_ = reg.PopulateFromESFData(data)
	}
	return reg
}

// resolveAppearanceSpec builds a gltf.AppearanceSpec from the CLI flags, loading
// the actual textures from the appearance registry. It returns (nil, nil) when
// --apply-appearance was not requested. Unresolved textures leave that slot nil
// (the exporter falls back to tint / bare skin) and are reported to stderr.
func resolveAppearanceSpec(inputPath string) (*gltf.AppearanceSpec, error) {
	if applyAppearanceRace == "" {
		return nil, nil
	}
	tables, err := loadAppearanceTables()
	if err != nil {
		return nil, err
	}
	rk := tables.raceKey(applyAppearanceRace)
	if rk == "" {
		valid := make([]string, 0, len(tables.Faces))
		for k := range tables.Faces {
			valid = append(valid, k)
		}
		return nil, fmt.Errorf("unknown race %q for --apply-appearance; known races: %s",
			applyAppearanceRace, strings.Join(valid, ", "))
	}
	if appearanceArmorSet < 0 || appearanceArmorSet > 8 {
		return nil, fmt.Errorf("--armor-set must be 0..8 (0 = bare), got %d", appearanceArmorSet)
	}
	if appearanceHair < 0 || appearanceHair > 7 {
		return nil, fmt.Errorf("--hair must be 0..7, got %d", appearanceHair)
	}
	if appearanceTint < 0 || appearanceTint >= len(tables.Tints) {
		return nil, fmt.Errorf("--tint must be 0..%d, got %d", len(tables.Tints)-1, appearanceTint)
	}

	reg := buildAppearanceRegistry(inputPath)
	spec := &gltf.AppearanceSpec{
		Race:     rk,
		ArmorSet: appearanceArmorSet,
		HairIdx:  appearanceHair,
		TintIdx:  appearanceTint,
	}
	tc := tables.Tints[appearanceTint]
	spec.Tint = [4]uint8{uint8(tc[0]), uint8(tc[1]), uint8(tc[2]), uint8(tc[3])}

	var unresolved []string

	// Hair.
	if he, ok := tables.Hair[strconv.Itoa(appearanceHair)]; ok {
		if id, err := parseDictID(he.DictID); err == nil {
			spec.HairDictID = id
			if img, err := loadAppearanceTexture(he.File, reg, id); err == nil {
				spec.HairTexture = img
			} else {
				unresolved = append(unresolved, fmt.Sprintf("hair 0x%X (%v)", id, err))
			}
		}
	}

	// Armor set slots (0 = bare, nothing to load).
	if appearanceArmorSet != 0 {
		set, ok := tables.ArmorSets[strconv.Itoa(appearanceArmorSet)]
		if !ok {
			return nil, fmt.Errorf("armor set %d not present in appearance tables", appearanceArmorSet)
		}
		for slot := 0; slot < 5; slot++ {
			se, ok := set[strconv.Itoa(slot)]
			if !ok {
				continue
			}
			id, err := parseDictID(se.DictID)
			if err != nil {
				continue
			}
			spec.ArmorDictIDs[slot] = id
			if img, err := loadAppearanceTexture(se.File, reg, id); err == nil {
				spec.ArmorTextures[slot] = img
			} else {
				unresolved = append(unresolved, fmt.Sprintf("armor slot %d 0x%X (%v)", slot, id, err))
			}
		}
	}

	if len(unresolved) > 0 {
		fmt.Fprintf(os.Stderr, "warning: --apply-appearance could not resolve %d texture(s):\n  %s\n",
			len(unresolved), strings.Join(unresolved, "\n  "))
	}
	return spec, nil
}

// writeAppearanceSidecar writes <glbPath>.appearance.json next to an exported GLB
// so a downstream viewer can re-apply CHARCUST at runtime. Best-effort.
func writeAppearanceSidecar(glbPath string, spec *gltf.AppearanceSpec) {
	if spec == nil {
		return
	}
	b, err := json.MarshalIndent(spec.Mapping(), "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(glbPath+".appearance.json", b, 0644)
}
