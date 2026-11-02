package eqoa

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// Character sound effects: which sounds each animation of a CSprite plays.
//
// Every record of a CSprite's 0x2910 v3 animation table names an animation id
// and 0, 1 or 2 sounds (SoundID at +20, Sound2ID at +32). A sound id is the
// DictID in the body of a 0xB010 AdpcmHeader, not the DictID of its 0xB000
// parent: on the Frontiers disc all 242 sound ids that CSprites name resolve
// by the header id and none by the 0xB000 object id. Many 0xB000 objects
// repeat one header id (3161 objects, 273 ids on Frontiers); the first in
// file order, then preorder, is used.
//
// Same rules as the JavaScript port's lib/sfx.js (tunaria tools/eqoa), which
// writes the same SFX/sfx.json.

// SpriteSoundSource is one parsed ESF file.
type SpriteSoundSource struct {
	File    string
	R       io.ReadSeeker
	Order   binary.ByteOrder
	Objects []*ESFObject
}

// SoundRef is where a sound's 0xB000 object lives.
type SoundRef struct {
	Source     int // index into the sources slice
	Object     *ESFObject
	SampleRate uint32
	BlockCount uint32
}

// SpriteSounds is every CSprite's animation sounds and every sound found.
type SpriteSounds struct {
	// Sprites maps a sprite id to animation id -> sound ids (SoundID then
	// Sound2ID, zeros left out, no repeats). Sprites with no sounds are absent.
	Sprites map[uint32]map[uint32][]uint32
	Sounds  map[uint32]SoundRef
	// Missing lists sound ids a CSprite names that no 0xB000 carries.
	Missing []uint32
	Errors  []string
}

// SoundsByAnim groups one 0x2910 table's sounds by animation id.
func SoundsByAnim(table []CSpriteAnimation) map[uint32][]uint32 {
	out := map[uint32][]uint32{}
	for _, rec := range table {
		for _, s := range []uint32{rec.SoundID, rec.Sound2ID} {
			if s == 0 {
				continue
			}
			list := out[rec.AnimID]
			dup := false
			for _, x := range list {
				if x == s {
					dup = true
					break
				}
			}
			if !dup {
				out[rec.AnimID] = append(list, s)
			}
		}
	}
	return out
}

func walkPreorder(objs []*ESFObject, fn func(*ESFObject)) {
	for _, o := range objs {
		fn(o)
		walkPreorder(o.Children, fn)
	}
}

func childOfType(o *ESFObject, t uint16) *ESFObject {
	for _, c := range o.Children {
		if uint16(c.Header.ObjectType) == t {
			return c
		}
	}
	return nil
}

// cspriteSoundID is the CSprite's id: its DictID, else the 0x2710 header's first dword.
func cspriteSoundID(r io.ReadSeeker, o *ESFObject, order binary.ByteOrder) uint32 {
	if o.DictID != 0 {
		return o.DictID
	}
	h := childOfType(o, 0x2710)
	if h == nil {
		return 0
	}
	b, err := h.ReadRaw(r)
	if err != nil || len(b) < 4 {
		return 0
	}
	return order.Uint32(b[0:4])
}

// CollectSpriteSounds walks every 0xB000 and every CSprite of the sources.
func CollectSpriteSounds(srcs []SpriteSoundSource) *SpriteSounds {
	out := &SpriteSounds{Sprites: map[uint32]map[uint32][]uint32{}, Sounds: map[uint32]SoundRef{}}
	for si, src := range srcs {
		walkPreorder(src.Objects, func(o *ESFObject) {
			if uint16(o.Header.ObjectType) != 0xB000 {
				return
			}
			h := childOfType(o, 0xB010)
			if h == nil {
				return
			}
			b, err := h.ReadRaw(src.R)
			if err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s @%d: %v", src.File, o.Offset, err))
				return
			}
			hdr, err := ParseAdpcmHeader(b, src.Order)
			if err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s @%d: %v", src.File, o.Offset, err))
				return
			}
			if _, seen := out.Sounds[hdr.DictID]; hdr.DictID != 0 && !seen {
				out.Sounds[hdr.DictID] = SoundRef{Source: si, Object: o, SampleRate: hdr.SampleRate, BlockCount: hdr.BlockCount}
			}
		})
		walkPreorder(src.Objects, func(o *ESFObject) {
			if uint16(o.Header.ObjectType) != 0x2700 {
				return
			}
			t := childOfType(o, 0x2910)
			if t == nil || t.Header.ObjectVersion != 3 {
				return
			}
			b, err := t.ReadRaw(src.R)
			if err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s @%d: %v", src.File, o.Offset, err))
				return
			}
			table, err := ParseCSpriteAnimations(b, 3, src.Order)
			if err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s @%d: %v", src.File, o.Offset, err))
				return
			}
			byAnim := SoundsByAnim(table)
			id := cspriteSoundID(src.R, o, src.Order)
			if _, seen := out.Sprites[id]; len(byAnim) > 0 && id != 0 && !seen {
				out.Sprites[id] = byAnim
			}
		})
	}
	missing := map[uint32]bool{}
	for _, byAnim := range out.Sprites {
		for _, list := range byAnim {
			for _, s := range list {
				if _, ok := out.Sounds[s]; !ok && !missing[s] {
					missing[s] = true
					out.Missing = append(out.Missing, s)
				}
			}
		}
	}
	sort.Slice(out.Missing, func(i, j int) bool { return out.Missing[i] < out.Missing[j] })
	return out
}

// DecodeSpriteSound returns the 16-bit mono PCM and sample rate of a sound.
func DecodeSpriteSound(srcs []SpriteSoundSource, ref SoundRef) ([]int16, uint32, error) {
	src := srcs[ref.Source]
	var data []byte
	if d := childOfType(ref.Object, 0xB020); d != nil {
		b, err := d.ReadRaw(src.R)
		if err != nil {
			return nil, 0, err
		}
		data = b
	}
	if data == nil {
		return nil, 0, fmt.Errorf("sound has no 0xB020 data")
	}
	return DecodeADPCM(data), ref.SampleRate, nil
}

// SFXDir is the output folder; SFXManifestName the table inside it.
const (
	SFXDir             = "SFX"
	SFXManifestName    = "sfx.json"
	SFXManifestVersion = 1
)

// SFXIDKey is the fixed-width key every sound and sprite id uses: "0x0000ABCD".
func SFXIDKey(n uint32) string { return fmt.Sprintf("0x%08X", n) }

// SFXAnimKey is the animation id key as clip names write it: "0x16".
func SFXAnimKey(n uint32) string { return fmt.Sprintf("0x%02X", n) }

// SFXSoundPath is where a decoded sound goes: SFX/0x0000ABCD.wav.
func SFXSoundPath(id uint32) string { return SFXDir + "/" + SFXIDKey(id) + ".wav" }

// SFXManifest is SFX/sfx.json:
//
//	{ "version": 1, "sounds": { "0x0000ABCD": "SFX/0x0000ABCD.wav" },
//	  "sprites": { "0x00001234": { "0x16": ["0x0000ABCD"] } } }
//
// encoding/json writes map keys sorted, as the JavaScript port does.
type SFXManifest struct {
	Version int                            `json:"version"`
	Sounds  map[string]string              `json:"sounds"`
	Sprites map[string]map[string][]string `json:"sprites"`
}

// BuildSFXManifest makes the manifest. written (nil = all) limits sounds to the
// files that were written; an animation or sprite left with none is dropped.
func BuildSFXManifest(s *SpriteSounds, written map[uint32]bool) SFXManifest {
	m := SFXManifest{Version: SFXManifestVersion, Sounds: map[string]string{}, Sprites: map[string]map[string][]string{}}
	for id, byAnim := range s.Sprites {
		anims := map[string][]string{}
		for a, list := range byAnim {
			var keep []string
			for _, snd := range list {
				if written != nil && !written[snd] {
					continue
				}
				keep = append(keep, SFXIDKey(snd))
				m.Sounds[SFXIDKey(snd)] = SFXSoundPath(snd)
			}
			if len(keep) > 0 {
				anims[SFXAnimKey(a)] = keep
			}
		}
		if len(anims) > 0 {
			m.Sprites[SFXIDKey(id)] = anims
		}
	}
	return m
}
