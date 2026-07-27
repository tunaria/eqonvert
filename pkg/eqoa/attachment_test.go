package eqoa

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"testing"
)

// buildAttachBody encodes a 0x2500 HSpriteAttachments body for testing.
func buildAttachBody(recs ...[3]int32) []byte {
	buf := new(bytes.Buffer)
	w := func(v int32) { binary.Write(buf, binary.LittleEndian, v) }
	w(int32(len(recs)))
	for _, r := range recs {
		w(r[0]) // type
		w(r[1]) // dictID
		w(r[2]) // nodeIndex
	}
	return buf.Bytes()
}

func TestParseHSpriteAttachments(t *testing.T) {
	body := buildAttachBody(
		[3]int32{0, 0x11223344, 7},
		[3]int32{1, 0x55667788, -1},
	)

	got, err := ParseHSpriteAttachments(body, binary.LittleEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []HSpriteAttachment{
		{Type: AttachTypeSimpleSprite, DictID: 0x11223344, NodeIndex: 7},
		{Type: AttachTypeSkinSubSprite, DictID: 0x55667788, NodeIndex: -1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[0].IsSkin() {
		t.Error("record 0 (node 7) reported as skin attach")
	}
	if !got[1].IsSkin() {
		t.Error("record 1 (node -1) not reported as skin attach")
	}
	if got[0].ResourceName() != "SimpleSprite" || got[1].ResourceName() != "SkinSubSprite" {
		t.Errorf("resource names = %q, %q", got[0].ResourceName(), got[1].ResourceName())
	}
}

// A DictID of 0 means "nothing attached": the engine skips the dictionary
// lookup and the attach entirely, so the record carries no placement.
func TestParseHSpriteAttachmentsSkipsZeroDictID(t *testing.T) {
	body := buildAttachBody(
		[3]int32{0, 0, 3},
		[3]int32{0, 0x0BADF00D, 4},
	)
	got, err := ParseHSpriteAttachments(body, binary.LittleEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[0].DictID != 0x0BADF00D {
		t.Fatalf("got %+v, want only the non-zero DictID record", got)
	}
}

// The engine aborts the whole parse on a type outside {0,1}; so do we, rather
// than silently guessing a resource namespace.
func TestParseHSpriteAttachmentsRejectsUnknownType(t *testing.T) {
	body := buildAttachBody([3]int32{2, 0x1234, 0})
	if _, err := ParseHSpriteAttachments(body, binary.LittleEndian); err == nil {
		t.Fatal("expected an error for type 2")
	}
}

func TestParseHSpriteAttachmentsRejectsTruncated(t *testing.T) {
	body := buildAttachBody([3]int32{0, 0x1234, 0})
	for _, n := range []int{0, 3, len(body) - 1} {
		if _, err := ParseHSpriteAttachments(body[:n], binary.LittleEndian); err == nil {
			t.Errorf("expected an error for a %d-byte body", n)
		}
	}
	// A negative count must not reach the allocation.
	neg := make([]byte, 4)
	binary.LittleEndian.PutUint32(neg, uint32(0xFFFFFFFF))
	if _, err := ParseHSpriteAttachments(neg, binary.LittleEndian); err == nil {
		t.Error("expected an error for a negative count")
	}
}

func TestParseHSpriteAttachmentsBigEndian(t *testing.T) {
	buf := new(bytes.Buffer)
	w := func(v int32) { binary.Write(buf, binary.BigEndian, v) }
	w(1)
	w(0)
	w(0x0A0B0C0D)
	w(2)
	got, err := ParseHSpriteAttachments(buf.Bytes(), binary.BigEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[0].DictID != 0x0A0B0C0D || got[0].NodeIndex != 2 {
		t.Fatalf("got %+v", got)
	}
}

// buildSlotBody encodes a 0x2920 CSpriteASlotList body for testing.
func buildSlotBody(recs ...[2]int32) []byte {
	buf := new(bytes.Buffer)
	w := func(v int32) { binary.Write(buf, binary.LittleEndian, v) }
	w(int32(len(recs)))
	for _, r := range recs {
		w(r[0]) // slot
		w(r[1]) // nodeIndex
	}
	return buf.Bytes()
}

func TestParseCSpriteAttachSlots(t *testing.T) {
	body := buildSlotBody(
		[2]int32{0, 18},
		[2]int32{1, 29},
		[2]int32{2, 30},
	)

	got, err := ParseCSpriteAttachSlots(body, binary.LittleEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []CSpriteAttachSlot{{Slot: 0, NodeIndex: 18}, {Slot: 1, NodeIndex: 29}, {Slot: 2, NodeIndex: 30}}
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	for i, s := range got {
		if !s.HasNode() {
			t.Errorf("record %d (node %d) reported as having no attach point", i, s.NodeIndex)
		}
	}
}

// A count of 0 is normal — five of the 568 retail CSprites carry an empty list —
// so it must parse to no records rather than fail.
func TestParseCSpriteAttachSlotsEmptyIsValid(t *testing.T) {
	got, err := ParseCSpriteAttachSlots(buildSlotBody(), binary.LittleEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want no records", got)
	}
}

// -1 is the engine's "this slot has no attach point" value (AttachItem skips the
// attach). It must survive parsing but not claim a joint.
func TestParseCSpriteAttachSlotsKeepsNoNodeSentinel(t *testing.T) {
	got, err := ParseCSpriteAttachSlots(buildSlotBody([2]int32{1, -1}), binary.LittleEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[0].NodeIndex != CSpriteAttachSlotNoNode {
		t.Fatalf("got %+v", got)
	}
	if got[0].HasNode() {
		t.Error("node index -1 reported as an attach point")
	}
}

// The engine writes each record straight into a fixed 3-entry table with no
// bounds check, so a slot outside 0..2 is corruption, not data — reject it
// rather than emit a binding that cannot exist.
func TestParseCSpriteAttachSlotsRejectsSlotOutOfRange(t *testing.T) {
	for _, slot := range []int32{-1, CSpriteAttachSlotCount, 99} {
		if _, err := ParseCSpriteAttachSlots(buildSlotBody([2]int32{slot, 0}), binary.LittleEndian); err == nil {
			t.Errorf("expected an error for slot %d", slot)
		}
	}
}

func TestParseCSpriteAttachSlotsRejectsTruncated(t *testing.T) {
	body := buildSlotBody([2]int32{0, 4})
	for _, n := range []int{0, 3, len(body) - 1} {
		if _, err := ParseCSpriteAttachSlots(body[:n], binary.LittleEndian); err == nil {
			t.Errorf("expected an error for a %d-byte body", n)
		}
	}
	// A negative count must not reach the allocation.
	neg := make([]byte, 4)
	binary.LittleEndian.PutUint32(neg, uint32(0xFFFFFFFF))
	if _, err := ParseCSpriteAttachSlots(neg, binary.LittleEndian); err == nil {
		t.Error("expected an error for a negative count")
	}
}

func TestParseCSpriteAttachSlotsBigEndian(t *testing.T) {
	buf := new(bytes.Buffer)
	w := func(v int32) { binary.Write(buf, binary.BigEndian, v) }
	w(1)
	w(2)
	w(0x0A0B)
	got, err := ParseCSpriteAttachSlots(buf.Bytes(), binary.BigEndian)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[0].Slot != 2 || got[0].NodeIndex != 0x0A0B {
		t.Fatalf("got %+v", got)
	}
}

// itemESFPath is a real beta-disc ESF whose sprites are HSprites (0x2200), the
// only sprite kind that carries a 0x2500 array. The test is skipped when it is
// not present so CI stays green without game assets.
const itemESFPath = "/Users/justinjanes/Development/elfconv/EQOABETADISC/DATA/ITEM.ESF"

// TestHSpriteAttachmentsRealITEM parses every 0x2500 in the real ITEM.ESF and
// asserts each body is exactly consumed, every record is a known type, and every
// non-skin NodeIndex addresses a real joint of the owning 0x2400 hierarchy.
func TestHSpriteAttachmentsRealITEM(t *testing.T) {
	data, err := os.ReadFile(itemESFPath)
	if err != nil {
		t.Skipf("real item asset not available: %v", err)
	}

	r := io.ReadSeeker(bytes.NewReader(data))
	_, objects, _, order, err := ParseESF(r)
	if err != nil {
		t.Fatalf("ParseESF: %v", err)
	}

	var attachObjs []*ESFObject
	var sprites []*ESFObject
	var walk func(o *ESFObject)
	walk = func(o *ESFObject) {
		if uint16(o.Header.ObjectType) == 0x2500 {
			attachObjs = append(attachObjs, o)
		}
		if IsSprite(uint16(o.Header.ObjectType)) {
			sprites = append(sprites, o)
		}
		for _, c := range o.Children {
			walk(c)
		}
	}
	for _, o := range objects {
		walk(o)
	}
	if len(attachObjs) == 0 {
		t.Fatal("no 0x2500 objects found in ITEM.ESF")
	}

	// Every body must be exactly 4 + 12*count bytes — no trailing slack, which
	// would mean the record stride is wrong.
	for _, o := range attachObjs {
		body, err := o.ReadBody(r)
		if err != nil {
			t.Fatalf("0x%X: ReadBody: %v", o.Offset, err)
		}
		recs, err := ParseHSpriteAttachments(body, order)
		if err != nil {
			t.Fatalf("0x%X: %v", o.Offset, err)
		}
		count := int32(order.Uint32(body[0:4]))
		if want := 4 + int(count)*attachmentRecordSize; len(body) != want {
			t.Errorf("0x%X: body is %d bytes, count %d implies %d", o.Offset, len(body), count, want)
		}
		if len(recs) > int(count) {
			t.Errorf("0x%X: %d records from a count of %d", o.Offset, len(recs), count)
		}
	}

	// LoadAsset must attribute each 0x2500 to its own hierarchy, and every
	// non-skin NodeIndex must be a valid joint of that hierarchy.
	withAttachments := 0
	for _, o := range sprites {
		asset, err := LoadAsset(r, o, order)
		if err != nil {
			t.Fatalf("LoadAsset 0x%X: %v", o.DictID, err)
		}
		if asset.AttachmentsErr != nil {
			t.Errorf("sprite 0x%X: attachments: %v", asset.ID, asset.AttachmentsErr)
		}
		if len(asset.Attachments) == 0 {
			continue
		}
		withAttachments++
		if asset.Hierarchy == nil {
			t.Errorf("sprite 0x%X has attachments but no hierarchy", asset.ID)
			continue
		}
		for _, a := range asset.Attachments {
			if a.IsSkin() {
				continue
			}
			if a.NodeIndex < 0 || int(a.NodeIndex) >= len(asset.Hierarchy.Joints) {
				t.Errorf("sprite 0x%X: NodeIndex %d outside 0..%d",
					asset.ID, a.NodeIndex, len(asset.Hierarchy.Joints)-1)
			}
		}
	}
	if withAttachments == 0 {
		t.Fatal("no sprite in ITEM.ESF surfaced attachments through LoadAsset")
	}
}

// charESFPath is a real beta-disc ESF whose sprites are CSprites (0x2700), the
// only sprite kind that carries a 0x2920 attach-slot list. Skipped when absent
// so CI stays green without game assets.
const charESFPath = "/Users/justinjanes/Development/elfconv/EQOABETADISC/DATA/CHAR.ESF"

// TestCSpriteAttachSlotsRealCHAR parses every 0x2920 in the real CHAR.ESF and
// asserts each body is exactly consumed, every slot is in range, and every
// NodeIndex addresses a real joint of the owning 0x2400 hierarchy. It also
// pins the two facts that motivated 0x2920 in the first place: a CSprite never
// carries a 0x2500, and an empty slot list is normal.
func TestCSpriteAttachSlotsRealCHAR(t *testing.T) {
	data, err := os.ReadFile(charESFPath)
	if err != nil {
		t.Skipf("real char asset not available: %v", err)
	}

	r := io.ReadSeeker(bytes.NewReader(data))
	_, objects, _, order, err := ParseESF(r)
	if err != nil {
		t.Fatalf("ParseESF: %v", err)
	}

	var slotObjs []*ESFObject
	var sprites []*ESFObject
	attachObjs := 0
	var walk func(o *ESFObject)
	walk = func(o *ESFObject) {
		switch uint16(o.Header.ObjectType) {
		case 0x2920:
			slotObjs = append(slotObjs, o)
		case 0x2500:
			attachObjs++
		}
		if IsSprite(uint16(o.Header.ObjectType)) {
			sprites = append(sprites, o)
		}
		for _, c := range o.Children {
			walk(c)
		}
	}
	for _, o := range objects {
		walk(o)
	}
	if len(slotObjs) == 0 {
		t.Fatal("no 0x2920 objects found in CHAR.ESF")
	}
	// Characters place held items through 0x2920 exclusively; if a 0x2500 ever
	// showed up here the two would need disambiguating.
	if attachObjs != 0 {
		t.Errorf("CHAR.ESF has %d 0x2500 objects, expected none", attachObjs)
	}

	// Every body must be exactly 4 + 8*count bytes — no trailing slack, which
	// would mean the record stride is wrong.
	empty := 0
	for _, o := range slotObjs {
		body, err := o.ReadBody(r)
		if err != nil {
			t.Fatalf("0x%X: ReadBody: %v", o.Offset, err)
		}
		recs, err := ParseCSpriteAttachSlots(body, order)
		if err != nil {
			t.Fatalf("0x%X: %v", o.Offset, err)
		}
		count := int32(order.Uint32(body[0:4]))
		if want := 4 + int(count)*attachSlotRecordSize; len(body) != want {
			t.Errorf("0x%X: body is %d bytes, count %d implies %d", o.Offset, len(body), count, want)
		}
		if len(recs) != int(count) {
			t.Errorf("0x%X: %d records from a count of %d", o.Offset, len(recs), count)
		}
		if count == 0 {
			empty++
		}
	}
	if empty == 0 {
		t.Error("expected some CSprites to carry an empty slot list")
	}

	// LoadAsset must attribute each 0x2920 to its own hierarchy, and every
	// NodeIndex must be a valid joint of that hierarchy.
	withSlots := 0
	for _, o := range sprites {
		asset, err := LoadAsset(r, o, order)
		if err != nil {
			t.Fatalf("LoadAsset 0x%X: %v", o.DictID, err)
		}
		if asset.AttachSlotsErr != nil {
			t.Errorf("sprite 0x%X: attach slots: %v", asset.ID, asset.AttachSlotsErr)
		}
		if len(asset.AttachSlots) == 0 {
			continue
		}
		withSlots++
		if asset.Hierarchy == nil {
			t.Errorf("sprite 0x%X has attach slots but no hierarchy", asset.ID)
			continue
		}
		for _, s := range asset.AttachSlots {
			if !s.HasNode() {
				continue
			}
			if s.NodeIndex < 0 || int(s.NodeIndex) >= len(asset.Hierarchy.Joints) {
				t.Errorf("sprite 0x%X slot %d: NodeIndex %d outside 0..%d",
					asset.ID, s.Slot, s.NodeIndex, len(asset.Hierarchy.Joints)-1)
			}
		}
	}
	if withSlots == 0 {
		t.Fatal("no sprite in CHAR.ESF surfaced attach slots through LoadAsset")
	}
}
