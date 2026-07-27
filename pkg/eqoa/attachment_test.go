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
