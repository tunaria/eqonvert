package eqoa

// Readers for CHARCUST, the character customization file (vanilla
// /DATA/CHARCUST.ESF, Frontiers /DATA2/CHARCUST.CSF): the helm meshes and the
// armor, hair and robe sheets, read from the file itself instead of a
// hand-written id list. Port of tunaria-eqoa tools/eqoa/lib/charcust.js.
//
// Layout (both discs):
//   - 57 root 0x1000 Surfaces, then 8 root 0x2000 SimpleSprites.
//   - Surfaces 0..39: 8 groups of 5 armor sheets, sides 128, 64, 64, 128, 64.
//   - Surfaces 40..47: the 8 hair sheets, 64x64.
//   - Surface 48: 0x431B69AA, 16x16, every pixel alpha 0 (blank hair).
//   - Surfaces 49..56: the 8 robe sheets, 128x128.
//   - Sprites 57..64: the 8 helms. Each 0x2000 holds a 0x2001 header whose
//     first u32 is the sprite id, a 0x1110 palette with one surface (inside a
//     0x1001 array), a 0x1200 mesh and a 0x4200 collision buffer.
//
// Set and slot of each armor sheet: group g is armor set CharcustGroupSets[g],
// sheet p of a group is slot CharcustPosSlots[p] (docs/ARMOR_TEXTURE_MAPPING.md,
// applied to file order). Chest and legs are the 128x128 sheets. The helm-to-set
// pairing is not stored in the file (the client's $Helm table is filled at run
// time). DERIVED, not sourced: helm h goes with set CharcustGroupSets[h], the
// same file order as the armor groups (eqoa-knowledge formats/CONVERTER_FORMATS.md).

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// CharcustPaths are the CHARCUST paths on the two discs, Frontiers first.
var CharcustPaths = []string{"/DATA2/CHARCUST.CSF", "/DATA/CHARCUST.ESF"}

// CharcustGroupSets is the armor set (1..8) of each group of 5 sheets, in file order.
var CharcustGroupSets = [8]int{1, 2, 3, 4, 8, 5, 7, 6}

// CharcustPosSlots is the slot (0 chest, 1 bracer, 2 gloves, 3 legs, 4 boots)
// of each sheet in a group, in file order.
var CharcustPosSlots = [5]int{0, 4, 1, 3, 2}

// CharcustSlotSide is the sheet side of each slot: chest and legs 128, the rest 64.
var CharcustSlotSide = [5]int32{128, 64, 64, 128, 64}

// CharcustSheet is one root surface.
type CharcustSheet struct {
	ID     uint32
	Offset int64
	Width  int32
	Height int32
	Pos    int // index among the root surfaces
}

// CharcustHelm is one root helm sprite. Surface and Mesh are 0 with Has* false when absent.
type CharcustHelm struct {
	Index      int
	Set        int // DERIVED armor set (CharcustGroupSets[Index]), not read from the file
	ID         uint32
	Offset     int64
	Surface    uint32
	HasSurface bool
	Mesh       uint32
	HasMesh    bool
}

// CharcustSheets is the result of ReadCharcustSheets.
type CharcustSheets struct {
	ArmorSets map[int][5]CharcustSheet // set 1..8 -> sheets by slot
	Hair      []CharcustSheet          // 8, file order
	Blank     CharcustSheet            // 16x16 placeholder
	Robe      []CharcustSheet          // 8, file order
}

type charcustFile struct {
	r       io.ReadSeeker
	objects []*ESFObject
	order   binary.ByteOrder
}

// openCharcust parses CHARCUST bytes (ESF or CESF).
func openCharcust(data []byte) (*charcustFile, error) {
	esf := data
	if len(data) >= 4 && string(data[:4]) == MagicCESF {
		rd, _, err := DecompressCSF(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if esf, err = io.ReadAll(rd); err != nil {
			return nil, err
		}
	}
	r := bytes.NewReader(esf)
	_, objs, _, order, err := ParseESF(r)
	if err != nil {
		return nil, err
	}
	return &charcustFile{r: r, objects: objs, order: order}, nil
}

func (f *charcustFile) sheet(o *ESFObject) (CharcustSheet, error) {
	b, err := o.ReadBody(f.r)
	if err != nil {
		return CharcustSheet{}, err
	}
	s, err := ParseSurface(b, f.order)
	if err != nil {
		return CharcustSheet{}, fmt.Errorf("CHARCUST surface at 0x%X: %w", o.Offset, err)
	}
	return CharcustSheet{ID: s.DictID, Offset: o.Offset, Width: s.Width, Height: s.Height}, nil
}

// ReadCharcustHelms returns the 8 helm meshes in file order. ID is the sprite
// id (first u32 of the 0x2001 header), Offset the 0x2000's offset in the
// decompressed ESF, Surface the id of its one texture and Mesh the id of its
// 0x1200.
func ReadCharcustHelms(data []byte) ([]CharcustHelm, error) {
	f, err := openCharcust(data)
	if err != nil {
		return nil, err
	}
	var out []CharcustHelm
	for _, o := range f.objects {
		if o.Header.ObjectType != 0x2000 {
			continue
		}
		var head *ESFObject
		for _, c := range o.Children {
			if c.Header.ObjectType == 0x2001 {
				head = c
				break
			}
		}
		if head == nil {
			return nil, fmt.Errorf("CHARCUST sprite at 0x%X has no 0x2001 header", o.Offset)
		}
		hb, err := head.ReadBody(f.r)
		if err != nil {
			return nil, err
		}
		if len(hb) < 4 {
			return nil, fmt.Errorf("CHARCUST sprite at 0x%X: 0x2001 header too short", o.Offset)
		}
		h := CharcustHelm{Index: len(out), ID: f.order.Uint32(hb[:4]), Offset: o.Offset}
		if h.Index < len(CharcustGroupSets) {
			h.Set = CharcustGroupSets[h.Index]
		}
		for _, c := range o.Children {
			switch c.Header.ObjectType {
			case 0x1110:
				for _, a := range c.Children {
					if a.Header.ObjectType != 0x1001 {
						continue
					}
					for _, x := range a.Children {
						if x.Header.ObjectType == 0x1000 {
							s, err := f.sheet(x)
							if err != nil {
								return nil, err
							}
							h.Surface, h.HasSurface = s.ID, true
							break
						}
					}
					break
				}
			case 0x1200:
				if h.HasMesh {
					continue
				}
				mb, err := c.ReadBody(f.r)
				if err != nil {
					return nil, err
				}
				if len(mb) >= 4 {
					h.Mesh, h.HasMesh = f.order.Uint32(mb[:4]), true
				}
			}
		}
		out = append(out, h)
	}
	return out, nil
}

// ReadCharcustSheets reads the root sheets. It returns an error when the root
// surfaces do not have the layout above (count or sheet sides), so a
// different file is never read as armor.
func ReadCharcustSheets(data []byte) (*CharcustSheets, error) {
	f, err := openCharcust(data)
	if err != nil {
		return nil, err
	}
	var surf []CharcustSheet
	for _, o := range f.objects {
		if o.Header.ObjectType != 0x1000 {
			continue
		}
		s, err := f.sheet(o)
		if err != nil {
			return nil, err
		}
		s.Pos = len(surf)
		surf = append(surf, s)
	}
	if len(surf) != 57 {
		return nil, fmt.Errorf("CHARCUST: %d root surfaces, expected 57", len(surf))
	}
	side := func(s CharcustSheet, n int32, what string) error {
		if s.Width != n || s.Height != n {
			return fmt.Errorf("CHARCUST %s at 0x%X is %dx%d, expected %dx%d", what, s.Offset, s.Width, s.Height, n, n)
		}
		return nil
	}
	out := &CharcustSheets{ArmorSets: map[int][5]CharcustSheet{}}
	for g, set := range CharcustGroupSets {
		var slots [5]CharcustSheet
		for k, slot := range CharcustPosSlots {
			s := surf[g*5+k]
			if err := side(s, CharcustSlotSide[slot], fmt.Sprintf("armor set %d slot %d", set, slot)); err != nil {
				return nil, err
			}
			slots[slot] = s
		}
		out.ArmorSets[set] = slots
	}
	out.Hair = surf[40:48]
	for i, s := range out.Hair {
		if err := side(s, 64, fmt.Sprintf("hair %d", i)); err != nil {
			return nil, err
		}
	}
	out.Blank = surf[48]
	if err := side(out.Blank, 16, "blank hair"); err != nil {
		return nil, err
	}
	out.Robe = surf[49:57]
	for i, s := range out.Robe {
		if err := side(s, 128, fmt.Sprintf("robe %d", i)); err != nil {
			return nil, err
		}
	}
	return out, nil
}
