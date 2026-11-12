package eqoa

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"testing"
)

// soundObj is a 0xB000 (no dictID of its own) whose 0xB010 header carries hdrID.
func soundObj(hdrID uint32) []byte {
	hdr := le(hdrID, 0, 2, 11025, 0, 0, 0)
	data := make([]byte, 32)
	data[0], data[2], data[16], data[17] = 0x0c, 0x12, 0x0c, 0x07
	return esfObj(0xB000, 0, nil, esfObj(0xB010, 0, hdr), esfObj(0xB020, 0, data))
}

// animRec is one 44-byte 0x2910 v3 record.
func animRec(actionID, animID, soundID, sound2ID uint32) []byte {
	return le(actionID, 0, animID, float32(1), 0, soundID, float32(0), float32(0), sound2ID, float32(0), 0)
}

// csprite is a 0x2700 (empty body, so no dictID) with a 0x2710 header id and a 0x2910 v3 table.
func csprite(id uint32, recs ...[]byte) []byte {
	table := le(len(recs))
	for _, r := range recs {
		table = append(table, r...)
	}
	return esfObj(0x2700, 6, nil, esfObj(0x2710, 4, append(le(id), make([]byte, 44)...)), esfObj(0x2910, 3, table))
}

func sourceOf(t *testing.T, name string, objs ...[]byte) SpriteSoundSource {
	t.Helper()
	var all []byte
	var offs []int64
	for _, o := range objs {
		offs = append(offs, int64(len(all)))
		all = append(all, o...)
	}
	r := bytes.NewReader(all)
	src := SpriteSoundSource{File: name, R: r, Order: binary.LittleEndian}
	for _, off := range offs {
		o, err := ReadObjectAt(r, binary.LittleEndian, off)
		if err != nil {
			t.Fatalf("ReadObjectAt %d: %v", off, err)
		}
		src.Objects = append(src.Objects, o)
	}
	return src
}

func TestSoundsByAnim(t *testing.T) {
	got := SoundsByAnim([]CSpriteAnimation{{AnimID: 0x16, SoundID: 0xA1}, {AnimID: 1, SoundID: 0xA2, Sound2ID: 0xA3}, {AnimID: 0x16, SoundID: 0xA1}, {AnimID: 0x22}})
	want := map[uint32][]uint32{0x16: {0xA1}, 1: {0xA2, 0xA3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SoundsByAnim = %v, want %v", got, want)
	}
}

func TestCollectSpriteSoundsAndManifest(t *testing.T) {
	srcs := []SpriteSoundSource{sourceOf(t, "CHAR.ESF",
		csprite(0x1234, animRec(1, 0x16, 0xA1, 0), animRec(2, 0x01, 0xA2, 0xA3), animRec(3, 0x16, 0xA1, 0), animRec(4, 0x22, 0xFF, 0)),
		csprite(0x5678, animRec(1, 0x00, 0, 0)),
		soundObj(0xA1), soundObj(0xA2), soundObj(0xA3), soundObj(0xA1),
	)}
	s := CollectSpriteSounds(srcs)
	if len(s.Errors) != 0 {
		t.Fatalf("errors: %v", s.Errors)
	}
	if _, ok := s.Sprites[0x5678]; ok {
		t.Error("a sprite with no sounds is left out")
	}
	if !reflect.DeepEqual(s.Sprites[0x1234], map[uint32][]uint32{0x16: {0xA1}, 1: {0xA2, 0xA3}, 0x22: {0xFF}}) {
		t.Errorf("sprite 0x1234 = %v", s.Sprites[0x1234])
	}
	if !reflect.DeepEqual(s.Missing, []uint32{0xFF}) {
		t.Errorf("missing = %v, want [0xFF]", s.Missing)
	}
	if s.Sounds[0xA1].SampleRate != 11025 {
		t.Errorf("sample rate = %d", s.Sounds[0xA1].SampleRate)
	}
	pcm, rate, err := DecodeSpriteSound(srcs, s.Sounds[0xA2])
	if err != nil || rate != 11025 || len(pcm) != 28 {
		t.Errorf("DecodeSpriteSound = %d samples, %d Hz, %v", len(pcm), rate, err)
	}
	m := BuildSFXManifest(s, map[uint32]bool{0xA1: true, 0xA2: true, 0xA3: true})
	b, _ := json.Marshal(m)
	want := `{"version":1,"sounds":{"0x000000A1":"SFX/0x000000A1.wav","0x000000A2":"SFX/0x000000A2.wav","0x000000A3":"SFX/0x000000A3.wav"},"sprites":{"0x00001234":{"0x01":["0x000000A2","0x000000A3"],"0x16":["0x000000A1"]}}}`
	if string(b) != want {
		t.Errorf("manifest\n got %s\nwant %s", b, want)
	}
}
