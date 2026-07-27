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
