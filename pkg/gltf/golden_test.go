package gltf

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Golden-file regression test over exported character models.
//
// Unit tests on synthetic bytes cannot catch what actually went wrong when the material header
// was corrected: every test passed, the module built, and nine races rendered with wrong face
// colours anyway. The damage was visible only by eye, and by then the baseline exports had been
// overwritten, so there was nothing left to compare against and the regression could not be
// isolated.
//
// This records what an export *contains* -- per material: which texture it points at, its alpha
// mode, and its base colour factor -- so any change to that surface shows up as a diff instead
// of as a surprise. It deliberately checks the exported artefact rather than calling the
// converter internals, so it stays useful no matter how the pipeline is refactored, and can be
// pointed at any export directory.
//
//	# record a baseline from a known-good export
//	EQONVERT_GOLDEN_DIR=~/Development/EQOAF_OUTPUT/DATA/CHAR go test ./pkg/gltf -run Golden -update
//
//	# check a later export against it
//	EQONVERT_GOLDEN_DIR=~/Development/EQOAF_OUTPUT/DATA/CHAR go test ./pkg/gltf -run Golden
//
// Without EQONVERT_GOLDEN_DIR the test skips: the game assets are not in the repository and
// cannot be, so this must never fail merely because someone does not have them.

var updateGolden = flag.Bool("update", false, "rewrite the golden file from the export directory")

// One golden file per source disc. The base game and Frontiers are different releases with
// genuinely different art, so comparing an export of one against a baseline of the other reports
// every material as changed -- which looks exactly like a catastrophic regression and is not one.
// eqonvert stamps .eqonvert-manifest.json at the export root, so the source is recoverable rather
// than something the operator has to remember to pass.
func goldenPathFor(source string) string {
	safe := strings.TrimSuffix(source, ".iso")
	safe = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, safe)
	if safe == "" {
		safe = "unknown"
	}
	return filepath.Join("testdata", "golden_"+safe+".json")
}

// sourceOf walks up from a CHAR directory looking for the export manifest.
func sourceOf(dir string) string {
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for i := 0; i < 4; i++ {
		blob, err := os.ReadFile(filepath.Join(d, ".eqonvert-manifest.json"))
		if err == nil {
			var m struct {
				SourceName string `json:"source_name"`
			}
			if json.Unmarshal(blob, &m) == nil && m.SourceName != "" {
				return m.SourceName
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return ""
}

// materialSig is one material's externally visible surface. Texture is a content hash rather
// than an index because indices shift when the number of embedded images changes -- which
// happened the moment second layers started parsing, and would otherwise have shown up as every
// material differing.
type materialSig struct {
	Texture   string    `json:"texture"`
	AlphaMode string    `json:"alphaMode,omitempty"`
	Factor    []float32 `json:"factor,omitempty"`
	Cutoff    *float32  `json:"cutoff,omitempty"`
}

// sameCutoff compares two optional alpha cutoffs by value. Both nil (a non-MASK material) counts
// as equal; one nil and one set is a real change, because the material gained or lost its cutout.
func sameCutoff(a, b *float32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

type modelSig struct {
	Materials []materialSig `json:"materials"`
	Images    int           `json:"images"`
	Meshes    int           `json:"meshes"`
}

// readGLB pulls the JSON and BIN chunks out of a .glb.
func readGLB(path string) (map[string]any, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "glTF" {
		return nil, nil, fmt.Errorf("%s: not a glb", filepath.Base(path))
	}
	var doc map[string]any
	var bin []byte
	off := 12
	for off+8 <= len(raw) {
		clen := int(binary.LittleEndian.Uint32(raw[off : off+4]))
		ctype := binary.LittleEndian.Uint32(raw[off+4 : off+8])
		off += 8
		if off+clen > len(raw) {
			break
		}
		chunk := raw[off : off+clen]
		off += clen
		switch ctype {
		case 0x4E4F534A: // JSON
			if err := json.Unmarshal(chunk, &doc); err != nil {
				return nil, nil, err
			}
		case 0x004E4942: // BIN
			bin = chunk
		}
	}
	return doc, bin, nil
}

func idx(v any) (int, bool) {
	f, ok := v.(float64)
	return int(f), ok
}

// signature reduces a model to the things a viewer actually renders.
func signature(path string) (modelSig, error) {
	doc, bin, err := readGLB(path)
	if err != nil {
		return modelSig{}, err
	}
	images, _ := doc["images"].([]any)
	textures, _ := doc["textures"].([]any)
	meshes, _ := doc["meshes"].([]any)
	materials, _ := doc["materials"].([]any)
	views, _ := doc["bufferViews"].([]any)

	texHash := func(ti int) string {
		if ti < 0 || ti >= len(textures) {
			return "?"
		}
		t, _ := textures[ti].(map[string]any)
		si, ok := idx(t["source"])
		if !ok || si >= len(images) {
			return "?"
		}
		im, _ := images[si].(map[string]any)
		bvi, ok := idx(im["bufferView"])
		if !ok || bvi >= len(views) {
			return "?"
		}
		bv, _ := views[bvi].(map[string]any)
		start, _ := idx(bv["byteOffset"])
		length, _ := idx(bv["byteLength"])
		if start+length > len(bin) {
			return "?"
		}
		sum := sha256.Sum256(bin[start : start+length])
		return fmt.Sprintf("%x", sum[:6])
	}

	sig := modelSig{Images: len(images), Meshes: len(meshes)}
	for _, mv := range materials {
		m, _ := mv.(map[string]any)
		ms := materialSig{Texture: "-"}
		if am, ok := m["alphaMode"].(string); ok {
			ms.AlphaMode = am
		}
		if c, ok := m["alphaCutoff"].(float64); ok {
			f := float32(c)
			ms.Cutoff = &f
		}
		if pbr, ok := m["pbrMetallicRoughness"].(map[string]any); ok {
			if bct, ok := pbr["baseColorTexture"].(map[string]any); ok {
				if ti, ok := idx(bct["index"]); ok {
					ms.Texture = texHash(ti)
				}
			}
			if bcf, ok := pbr["baseColorFactor"].([]any); ok {
				for _, v := range bcf {
					if f, ok := v.(float64); ok {
						ms.Factor = append(ms.Factor, float32(f))
					}
				}
			}
		}
		sig.Materials = append(sig.Materials, ms)
	}
	return sig, nil
}

// collect signs the player-race character models. Those are the ones with shared flesh textures
// and per-race faces, so they are where material changes show up first -- and they are few
// enough that the golden file stays readable in a diff.
func collect(dir string) (map[string]modelSig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]modelSig{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".glb") || !strings.Contains(n, "_animated_0x") {
			continue
		}
		if !strings.HasPrefix(n, "CHAR_") ||
			(!strings.Contains(n, "_Male_") && !strings.Contains(n, "_Female_")) {
			continue
		}
		sig, err := signature(filepath.Join(dir, n))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		out[n] = sig
	}
	return out, nil
}

func TestGoldenCharacterMaterials(t *testing.T) {
	dir := os.Getenv("EQONVERT_GOLDEN_DIR")
	if dir == "" {
		t.Skip("set EQONVERT_GOLDEN_DIR to an exported CHAR directory to run this")
	}
	source := sourceOf(dir)
	if source == "" {
		t.Skipf("no .eqonvert-manifest.json above %s -- cannot tell which disc this came from, "+
			"and comparing across discs is meaningless", dir)
	}
	goldenPath := goldenPathFor(source)
	t.Logf("source: %s -> %s", source, goldenPath)

	got, err := collect(dir)
	if err != nil {
		t.Fatalf("reading exports: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("no character models found in %s", dir)
	}

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		blob, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(blob, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s from %d models in %s", goldenPath, len(got), dir)
		return
	}

	blob, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Skipf("no golden file yet -- record one with -update (%v)", err)
	}
	var want map[string]modelSig
	if err := json.Unmarshal(blob, &want); err != nil {
		t.Fatalf("parsing %s: %v", goldenPath, err)
	}

	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)

	problems := 0
	for _, n := range names {
		w := want[n]
		g, ok := got[n]
		if !ok {
			t.Errorf("%s: missing from the export", n)
			problems++
			continue
		}
		if len(w.Materials) != len(g.Materials) {
			t.Errorf("%s: material count %d -> %d", n, len(w.Materials), len(g.Materials))
			problems++
			continue
		}
		// Reported per material and per field: "nine races changed" is not actionable, but
		// "material 7 swapped texture" points straight at the cause.
		for i := range w.Materials {
			a, b := w.Materials[i], g.Materials[i]
			if a.Texture != b.Texture {
				t.Errorf("%s mat %d: TEXTURE %s -> %s", n, i, a.Texture, b.Texture)
				problems++
			}
			if a.AlphaMode != b.AlphaMode {
				t.Errorf("%s mat %d: alphaMode %q -> %q", n, i, a.AlphaMode, b.AlphaMode)
				problems++
			}
			if fmt.Sprint(a.Factor) != fmt.Sprint(b.Factor) {
				t.Errorf("%s mat %d: baseColorFactor %v -> %v", n, i, a.Factor, b.Factor)
				problems++
			}
			// Cutoff was recorded in the signature but never asserted, so a change to the MASK
			// threshold -- which decides whether a texel is drawn at all -- passed silently.
			// It is the one field here that changes what renders without changing the texture,
			// the alpha mode or the colour.
			// Compare the pointed-to values, not the pointers. fmt.Sprint on a *float32 prints
			// the address, so every material would differ from itself.
			if !sameCutoff(a.Cutoff, b.Cutoff) {
				show := func(f *float32) string {
					if f == nil {
						return "none"
					}
					return fmt.Sprintf("%.3f", *f)
				}
				t.Errorf("%s mat %d: alphaCutoff %s -> %s", n, i, show(a.Cutoff), show(b.Cutoff))
				problems++
			}
		}
		if w.Images != g.Images {
			// Not a failure on its own: embedding more images changes file size but not
			// what renders. Logged because it is a strong hint that layer parsing moved.
			t.Logf("%s: embedded images %d -> %d (not a rendering change)", n, w.Images, g.Images)
		}
	}
	for n := range got {
		if _, ok := want[n]; !ok {
			t.Logf("%s: new model, not in the golden file", n)
		}
	}
	if problems == 0 {
		t.Logf("%d models match the golden file", len(want))
	}
}
