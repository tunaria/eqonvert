package eqoa

import (
	"encoding/binary"
	"fmt"
)

// Attachment resource kinds — the `Type` discriminant stored in a 0x2500 record.
// The engine maps it to a VIDictionary resource type before resolving DictID
// (ParseHSpriteAttachments__10VIESFParse @ 0x0040d470):
//
//	0 -> VIResourceType 4 = SimpleSprite   (0x2000; added by ParseSimpleSpriteObj)
//	1 -> VIResourceType 7 = SkinSubSprite  (0x2320; added by ParseSkinSubSpriteObj)
//
// Any other value aborts the parse with an error in the engine, so we do the same.
const (
	AttachTypeSimpleSprite  int32 = 0
	AttachTypeSkinSubSprite int32 = 1
)

// AttachNodeSkin is the NodeIndex sentinel meaning "skin this resource onto the
// whole hierarchy" (engine calls AttachSkin__9VIHSprite) rather than parenting it
// to a single node (Attach__9VIHSprite).
const AttachNodeSkin int32 = -1

// HSpriteAttachment is one record of an HSprite's 0x2500 HSpriteAttachments
// array. Each record says which resource hangs off which node of the sprite's
// 0x2400 hierarchy — this is the only placement information a held item gets:
// the engine applies NO extra rotation or offset, the item simply inherits the
// node's transform (VICSprite::AttachItem @ 0x00401a28 →
// Attach__9VIHSpriteiiR7VIScene @ 0x0041b328, which stores just {sprite, node}).
//
// Layout recovered from ParseHSpriteAttachments__10VIESFParse (@ 0x0040d470),
// object type 0x2500:
//
//	int32 count
//	repeat count times (12 bytes each):
//	  [0:4]  int32  Type       (0 or 1, see AttachType* — any other value is an error)
//	  [4:8]  uint32 DictID     (the attached resource; 0 means "no attachment", skipped)
//	  [8:12] int32  NodeIndex  (-1 = AttachSkin, otherwise a 0x2400 joint index)
//
// NodeIndex indexes the hierarchy's joint array directly: ParseHSpriteHierarchy
// (@ 0x0040d168) allocates the VIHSprite node vector with the 0x2400 joint count
// and AddRoot/Add hand out indices in file order, so node i == Hierarchy.Joints[i].
type HSpriteAttachment struct {
	Type      int32
	DictID    uint32
	NodeIndex int32
}

// IsSkin reports whether this record skins the resource onto the whole hierarchy
// (NodeIndex == -1) instead of parenting it to a single node.
func (a HSpriteAttachment) IsSkin() bool { return a.NodeIndex == AttachNodeSkin }

// ResourceName returns the ESF object type the record's Type discriminant
// resolves to, for human-readable output. Unknown values never reach here —
// ParseHSpriteAttachments rejects them — but the fallback keeps callers total.
func (a HSpriteAttachment) ResourceName() string {
	switch a.Type {
	case AttachTypeSimpleSprite:
		return "SimpleSprite"
	case AttachTypeSkinSubSprite:
		return "SkinSubSprite"
	}
	return fmt.Sprintf("Unknown(%d)", a.Type)
}

// attachmentRecordSize is the on-disc size of one 0x2500 record.
const attachmentRecordSize = 12

// ParseHSpriteAttachments decodes a 0x2500 HSpriteAttachments body. Records with
// DictID 0 are dropped: the engine skips the dictionary lookup and the attach
// entirely for those, so they carry no placement.
func ParseHSpriteAttachments(body []byte, order binary.ByteOrder) ([]HSpriteAttachment, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("HSpriteAttachments: body too short (%d bytes)", len(body))
	}
	count := int32(order.Uint32(body[0:4]))
	if count < 0 || len(body) < 4+int(count)*attachmentRecordSize {
		return nil, fmt.Errorf("HSpriteAttachments: count %d exceeds body size %d", count, len(body))
	}
	out := make([]HSpriteAttachment, 0, count)
	for i := 0; i < int(count); i++ {
		off := 4 + i*attachmentRecordSize
		a := HSpriteAttachment{
			Type:      int32(order.Uint32(body[off : off+4])),
			DictID:    order.Uint32(body[off+4 : off+8]),
			NodeIndex: int32(order.Uint32(body[off+8 : off+12])),
		}
		if a.Type != AttachTypeSimpleSprite && a.Type != AttachTypeSkinSubSprite {
			return nil, fmt.Errorf("HSpriteAttachments: record %d has unknown type %d", i, a.Type)
		}
		if a.DictID == 0 {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// CSpriteAttachSlotCount is how many item-attach slots a VICSprite has.
//
// The slot table is a fixed inline array of 12-byte entries at cSprite+0x110c;
// the next member (the per-slot attach handle array read by AttachItem) starts
// at +0x1130, so the table spans 0x1130-0x110c = 0x24 bytes = 3 entries. Both
// call sites of AttachItem__9VICSprite (@ 0x006bc9f8 and @ 0x006498c8) loop
// `slot < 3` to fill it, and the retail/beta data never uses a higher index.
//
// The engine's own parser performs an UNCHECKED write at
// slot*0xc + cSprite + 0x110c, so a slot index outside this range would corrupt
// adjacent sprite state rather than be rejected — ParseCSpriteAttachSlots
// rejects it instead.
const CSpriteAttachSlotCount = 3

// CSpriteAttachSlotNoNode is the NodeIndex value meaning "this slot has no
// attach point". VICSprite::AttachItem (@ 0x00401a28) skips the attach entirely
// when the stored node is -1. No 0x2920 record in retail or beta uses it — the
// engine writes it as the SetDefaultEntries__9VICSprite initial value — but it
// is a legal stored value, so the parser keeps such records rather than
// inventing a joint for them.
const CSpriteAttachSlotNoNode int32 = -1

// CSpriteAttachSlot is one record of a CSprite's 0x2920 CSpriteASlotList: the
// binding of one of the sprite's item-attach slots to a joint of its 0x2400
// hierarchy. This is the whole placement for a weapon or shield held by a
// CHARACTER — the CSprite equivalent of HSpriteAttachment's NodeIndex.
//
// Characters never carry a 0x2500 HSpriteAttachments array (ParseCSpriteObj
// @ 0x0040e5a8 never calls ParseHSpriteAttachments); they carry 0x2920 instead,
// and the two are mutually exclusive in practice.
//
// Layout recovered from ParseCSpriteASlotList__10VIESFParse (@ 0x0040eff8),
// object type 0x2920:
//
//	int32 count
//	repeat count times (8 bytes each):
//	  [0:4] int32 Slot       the attach slot, 0..CSpriteAttachSlotCount-1
//	  [4:8] int32 NodeIndex  a 0x2400 joint index (-1 = no attach point)
//
// Each pair is stored at slot*0xc + cSprite + 0x110c, which is exactly where
// VICSprite::AttachItem (@ 0x00401a28) reads it back:
//
//	Attach__9VIHSprite(this, sprite, *(int *)(base + 0x110c + slot*0xc), scene)
//
// As with 0x2500, NO rotation and no offset are applied anywhere: the item
// inherits the joint's transform, and that transform is the grip.
//
// Slot semantics that are evidenced (but deliberately NOT turned into names
// here, since the VICSpriteAttachSlot enumerator names are not recoverable from
// the stripped binary):
//   - Slots 0 and 1 are the weapon-capable ones.
//     SetAttackAction__9VICSprite (@ 0x004016a8) rejects any slot >= 2 outright,
//     and its caller only calls it `if (slot < 2)`.
//   - Slots 0 and 1 bind to opposite sides of the body. Across all 563 retail
//     char.esf CSprites that carry slots, slot 0's joint has model-space X < 0
//     in 495 cases and slot 1's has X >= 0 in 489 cases.
//   - Slot 2 sits on the same side as slot 1 but inboard of it (e.g. X=+0.560
//     against slot 1's +0.705 on sprite 0xDC3B344D) — i.e. further up the same
//     arm — and is not weapon-capable.
type CSpriteAttachSlot struct {
	Slot      int32
	NodeIndex int32
}

// HasNode reports whether this slot names a joint at all, as opposed to the
// engine's "no attach point" sentinel.
func (s CSpriteAttachSlot) HasNode() bool { return s.NodeIndex != CSpriteAttachSlotNoNode }

// attachSlotRecordSize is the on-disc size of one 0x2920 record.
const attachSlotRecordSize = 8

// ParseCSpriteAttachSlots decodes a 0x2920 CSpriteASlotList body.
//
// A count of 0 is normal, not an error: five of the 568 retail char.esf
// CSprites carry an empty list (body size 4), and the engine parses those the
// same way. Records are returned in file order; the engine stores them by
// writing each into a fixed slot table, so a repeated Slot would be last-wins,
// but no shipped file repeats one.
func ParseCSpriteAttachSlots(body []byte, order binary.ByteOrder) ([]CSpriteAttachSlot, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("CSpriteAttachSlots: body too short (%d bytes)", len(body))
	}
	count := int32(order.Uint32(body[0:4]))
	if count < 0 || len(body) < 4+int(count)*attachSlotRecordSize {
		return nil, fmt.Errorf("CSpriteAttachSlots: count %d exceeds body size %d", count, len(body))
	}
	out := make([]CSpriteAttachSlot, 0, count)
	for i := 0; i < int(count); i++ {
		off := 4 + i*attachSlotRecordSize
		s := CSpriteAttachSlot{
			Slot:      int32(order.Uint32(body[off : off+4])),
			NodeIndex: int32(order.Uint32(body[off+4 : off+8])),
		}
		if s.Slot < 0 || s.Slot >= CSpriteAttachSlotCount {
			return nil, fmt.Errorf("CSpriteAttachSlots: record %d has slot %d outside 0..%d",
				i, s.Slot, CSpriteAttachSlotCount-1)
		}
		out = append(out, s)
	}
	return out, nil
}
