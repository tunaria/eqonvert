# EQOA file formats

This document specifies the container and object formats parsed by
`pkg/eqoa/`. Everything here was recovered by reverse engineering the PS2
client (Ghidra + Emotion Engine loader) and validated against real disc data.
Engine function references (`FUN_00xxxxxx`) are addresses in the Frontiers
beta client executable and mark where each claim was verified.

## Authoring provenance

The formats below are the shipped form of assets authored in the standard DCC tools of the era:

| Asset class | Authored in |
|---|---|
| Textures and 2D art | Photoshop |
| Character animation | Maya |
| Environment props | 3ds Max |

This is not trivia. Several things in these formats read as export-pipeline artefacts rather than
engine design, and knowing the source tool is often what makes a field make sense:

- **Texture conventions follow the paint tool, not the GS.** Palette alpha is a full 0–255 range
  with a mode of 255 — measured at 93.7% of `CHAR*` CLUT entries above 128 — not the PS2's 0–128
  convention. That is what an 8-bit RGBA export produces, and the client compares it against
  `AREF=128` at draw time. Assuming the hardware convention here led directly to a wrong
  `alphaCutoff`; see `MATERIAL_BLEND_MODES.md`.
- **Skeletons and animation carry Maya's structure.** The 0x2400 hierarchy stores world-space bind
  TRS and the loader converts to parent-relative at load — a DCC-side export choice, not something
  the runtime needs. Joint ordering and the separate upper/lower animation layers follow from how
  the clips were authored.
- **Props carry Max's.** GroupSprite member transforms are `T·R·S` with Euler angles rather than
  quaternions, and zone actors carry full Euler triples where only yaw is usually non-zero.

Where a field looks arbitrary, the exporting tool is often the explanation.

## CSF — compressed container

Most disc files ship as CSF: a zlib block container around a raw ESF stream.

```
Offset  Size  Field
0       4     Magic "CESF"
4       4     NumberOfBlocks        (int32 LE)
8       8     TotalCompressedSize   (int64 LE)
16      8     TotalDecompressedSize (int64 LE)
24      8     FirstBlockOffset      (int64 LE, typically 40)
32      4     MaxCompressedBlock    (int32 LE)
36      4     Unknown               (often 0x77F534AC)
```

After the header, `NumberOfBlocks` blocks follow, each prefixed by an 8-byte
info record `{int32 compressedSize, int32 decompressedSize}` and containing a
standard zlib stream. Concatenating the inflated blocks yields the ESF file.
Implementation: `pkg/eqoa/csf.go`.

## ESF — object tree ("VIObjFile")

An ESF file is a tree of typed objects. The 32-byte file header:

```
Offset  Size  Field
0       4     Magic: "OBJF" (big endian file) or "FJBO" (little endian)
4       4     NumberOfObjects (top-level)
8       4     FileType
12      4     Unknown1
16      8     Offset
24      8     Unknown2
```

**Endianness rule:** the magic tells you the byte order — `FJBO` = little
endian, `OBJF` = big endian — and all subsequent fields follow it.  In
practice every disc examined (Frontiers beta, Beta 3, retail SLUS-207.44)
is `FJBO`/little-endian, as expected for the all-LE PS2 + Windows toolchain;
the big-endian path exists in the parser but no big-endian disc has been
seen.  Do not assume retail differs from beta here.

Every object starts with a 12-byte header:

```
Offset  Size  Field
0       2     ObjectType         (see table below)
2       2     ObjectVersion      (format revision — parsers branch on this)
4       4     ObjectSize         (body size in bytes, excluding this header)
8       4     NumberOfSubObjects (children parsed recursively from the body)
```

Most objects begin their body with a `uint32 DictID` — a 32-bit identifier
used for cross-references (textures by ID, sprite identity, etc.). DictIDs
are stable across game builds: the same model has the same ID in beta and
retail, and — critically — **the server-side "modelid" is the same value**
(see [MODEL_NAMES.md](MODEL_NAMES.md)).

### Object type registry

The full table lives in `pkg/eqoa/esf.go` (`ObjTypeNames`). The important
families:

| Range | Family |
|---|---|
| 0x1000–0x11xx | Surfaces (textures), materials, material palettes |
| 0x1200/0x1210 | Mesh data (static / skinned) |
| 0x2000–0x2Fxx | Sprites: renderable entities of all kinds |
| 0x2400 | Skeleton (see [ANIMATION.md](ANIMATION.md)) |
| 0x2500 | Attachments — what hangs off which skeleton node (see [Attachments](#attachments--hspriteattachments-0x2500)) |
| 0x2920 | CSprite attach slots — where a character holds an item (see [Attach slots](#attach-slots--cspriteaslotlist-0x2920)) |
| 0x2600 | Animation set (see [ANIMATION.md](ANIMATION.md)) |
| 0x5000 | RefMap — generic int32→int32 dictionary (bone maps, sound refs) |
| 0x3000–0x32xx | Zone structures: rooms, terrain tables, actors |
| 0x4200 | Collision mesh |
| 0xB000–0xB1xx | Audio (PS2 ADPCM, XM modules) |
| 0xC000–0xC3xx | Particle / effect emitters (see [Particle emitters](#particle-emitters)) |

### Sprite containers

A "sprite" is any renderable entity. Character models are usually a
`CSprite` (0x2700) containing, in rough order:

```
0x2700 CSprite
├── 0x2710 CSpriteHeader        (dictID of the sprite)
├── 0x1110 MaterialPalette      (surfaces + materials)
├── 0x5000 RefMap               (sound/effect references — NOT the bone map)
├── 0xB070 (audio container)    (footsteps, vocalizations)
├── (mesh container)            (LOD tiers of 0x2320 SkinSubSprite → 0x1210)
├── 0x2610 (animation list)     (N × 0x2600 HSpriteAnim)
├── 0x2400 HSpriteHierarchy     (skeleton)
├── 0x5000 RefMap               (bone map — immediately AFTER the hierarchy)
├── 0x2450 HSpriteTriggers, misc
└── 0x2920 CSpriteASlotList     (attach slots — where held items go)
```

⚠️ Sprites carry **multiple 0x5000 RefMaps** with different meanings. The
bone map is specifically the one following the 0x2400 hierarchy in the same
child list (the engine's HSprite parser `FUN_0040cdb0` reads them in that
order). Grabbing "the first RefMap in the tree" silently yields the sound-
reference map and breaks all animations. See `findSiblingAfter` in
`pkg/eqoa/asset.go`, which resolves the bone map, the 0x2500 attachment array
and the 0x2920 attach-slot list by the same "sibling after the hierarchy" rule.

## Attachments — HSpriteAttachments (0x2500)

An `HSprite` (0x2200) carries exactly one 0x2500 array, immediately after its
0x2400 hierarchy / 0x5000 bone map / optional 0x2450 triggers. It records what
hangs off which skeleton node:

```
int32 count
repeat count times (12 bytes):
  int32  Type       0 → SimpleSprite (0x2000), 1 → SkinSubSprite (0x2320)
  uint32 DictID     the attached resource (0 = nothing attached, skipped)
  int32  NodeIndex  -1 = skin onto the whole hierarchy, else a 0x2400 joint index
```

`Type` selects which resource namespace `DictID` is resolved in — the engine
maps 0 → VIResourceType 4 and 1 → VIResourceType 7, and errors on any other
value (`ParseHSpriteAttachments__10VIESFParse` @ 0x0040d470). `NodeIndex`
indexes the hierarchy's joint array directly: the engine allocates its node
vector with the 0x2400 joint count and hands out indices in file order.

**The node index is the whole placement.** The engine applies no rotation and
no offset when attaching — `Attach__9VIHSprite` stores just `{sprite, node}`
and the item inherits that node's transform, which *is* the grip.

⚠️ **`CSprite` (0x2700) has no 0x2500.** Character models place held items
through 0x2920 instead — see below. Measured: retail `char.esf` has 568
CSprites, 568 × 0x2920 and **zero** 0x2500 (beta `CHAR.ESF`: 390/390/0);
`item.esf` and `SCENE.ESF` have one 0x2500 per HSprite and no 0x2920. The two
never appear on the same sprite.

## Attach slots — CSpriteASlotList (0x2920)

A `CSprite` (0x2700) carries exactly one 0x2920 list, in the same place relative
to its hierarchy that an HSprite's 0x2500 occupies: `ParseCSpriteObj`
(@ 0x0040e5a8) reads 0x2400, the 0x5000 bone map, the optional 0x2450 triggers,
the skin list, the play list, the optional node-ID list, and then 0x2920 — only
when the CSprite's `ObjectVersion != 0`. It binds each of the sprite's item-
attach slots to a joint:

```
int32 count
repeat count times (8 bytes):
  int32 Slot       the VICSpriteAttachSlot index, 0..2
  int32 NodeIndex  a 0x2400 joint index (-1 = slot has no attach point)
```

`ParseCSpriteASlotList__10VIESFParse` (@ 0x0040eff8) writes each pair straight
into the sprite's slot table at `slot*0xc + cSprite + 0x110c`, which is exactly
where `VICSprite::AttachItem` (@ 0x00401a28) reads it back. **The node index is
the whole placement**, as with 0x2500: no rotation, no offset — the item
inherits the joint's transform, which *is* the grip.

There are exactly **three** slots. The table is a fixed inline array of 12-byte
entries and the next member (the per-slot attach handles) starts at `+0x1130`,
so it spans `0x1130-0x110c = 3 × 0xc`; both callers of `AttachItem` loop
`slot < 3`. The parser writes without a bounds check, so an out-of-range slot
would corrupt adjacent state — `ParseCSpriteAttachSlots` rejects it.

A `count` of 0 is normal, not an error (5 of 568 retail CSprites, 9 of 390 beta).
Most carry two or three slots.

⚠️ **The `VICSpriteAttachSlot` enumerator names are not recoverable** from the
shipped binary — every call site loops over the slots generically — so eqonvert
emits the raw index rather than inventing labels. What *is* evidenced:

* Slots **0 and 1 are the weapon-capable ones**. `SetAttackAction__9VICSprite`
  (@ 0x004016a8) rejects `slot >= 2`, and its caller only calls it `if (slot < 2)`.
* Slots 0 and 1 bind to **opposite sides of the body**: across the 563 retail
  CSprites with slots, slot 0's joint has model-space X < 0 in 495 cases and
  slot 1's has X >= 0 in 489.
* Slot 2 sits on the **same side as slot 1 but inboard of it** — e.g. X=+0.560
  against slot 1's +0.705 on sprite 0xDC3B344D — i.e. further up the same arm,
  and it cannot hold a weapon.

### Export representation

Sprites with either kind of record get a `PREFIX_…_attach.json` sidecar beside
their `.glb`, carrying a `joints` table that maps every source `node_index` to
the glTF node holding it, plus whichever record set applies:

* `attachments` (0x2500) — each record's resolved `resource` name, `dict_id`
  and `attach` mode.
* `attach_slots` (0x2920) — each `slot` with its `node_index`, `node_name` and
  `gltf_node`. Parent the item to `gltf_node`; a slot whose `node_index` is -1
  resolves to no node.

The same node identity is in the GLB itself three ways: the joint's
`Joint_<node_index>` name, the skin's `joints` array (index-aligned with the
0x2400 hierarchy, so `skin.joints[node_index]` is the node), and each joint
node's `extras.node_index`.

## Meshes — PrimBuffer (0x1200) / SkinPrimBuffer (0x1210)

Header (after optional dictID when ObjectVersion > 1):

```
int32 pbType      4 = zone terrain, 5 = skinned character mesh, others static
int32 numMaterials
int32 numGroups
int32 totalVerts
int32 p1, p2, p3  quantization exponents
```

Positions are dequantized by `1.0 / 2^p1`, UVs by `1.0 / 2^p2`. Vertices are
stored per face group; the skinned layout (pbType 5) is **21 bytes** per
vertex with no padding:

```
int16[3]  x, y, z      × 1/2^p1
int16[2]  u, v         × 1/2^p2
int8[3]   nx, ny, nz   × 1/127
uint8[4]  joint indices
uint8[4]  joint weights (÷255; renormalize — quantization drift is common)
```

A widely tempting mistake is assuming a padding byte after the normals
(22-byte stride). It parses without error and produces garbage positions —
verify with known-shape models when porting this.

Zone terrain (pbType 4) vertices carry a `uint16 VGroup` that indexes the
`ZonePreTranslations` (0x3250) array **per vertex**; applying translations
per-sprite instead of per-vertex causes seams between terrain sub-blocks.

## Materials and textures

- `Surface` (0x1000): a texture. PS2 formats including 8-bit paletted
  (PSMT8) with the GS's 32×32 block swizzle — `pkg/eqoa/surface.go`
  implements the unswizzle. Multiple mip levels may be present.
- `Material` (0x1100): layer list; each layer references a Surface by
  `TexID` (a dictID).
- `MaterialPalette` (0x1110): groups a `SurfaceArray` and `MaterialArray`
  for a sprite subtree; the nearest palette up the tree wins.

Surfaces referenced by no material (AO/detail maps) are skipped during export
to avoid phantom near-black textures in the GLB.

**Fullscreen UI backdrops are split across textures.** The GS has no
1024-wide texture format and only 4MB of VRAM, so hi-res screens (title,
status) are stored as consecutive 512×512 slices — the border art and title
lettering run continuously across the seam.  `convert` stitches runs of
consecutive standalone 512×512 surfaces into `PREFIX_screenN.png` composites
(rows 448–511 of each slice are padding); individual slices are always
written too.  The stitch order is file order: exact draw coordinates live in
client code, and texture dictIDs are hashed from names at runtime so they
never appear as searchable constants.  Animated UI uses a different
mechanism entirely — cells within one atlas addressed by UV offsets — never
file splitting.

## Audio

`Adpcm` (0xB000) wraps PS2 VAG-format ADPCM: `AdpcmHeader` (0xB010) carries
sample rate and block counts; `AdpcmSampleData` (0xB020) is the raw stream.
`convert` decodes it directly to 16-bit PCM `.wav` (`DecodeADPCM` in
`pkg/eqoa/audio.go` — the standard SPU2 codec: 16-byte blocks of
predictor/shift + 28 nibbles, five fixed filter pairs; block flag 0x07 ends
the stream).

`Xm` (0xB030) holds music converted from FastTracker II modules into a
runtime binary format: all text stripped, structures padded into fixed
arrays (256 pattern slots × 12 B directory, 128 instrument slots × 224 B,
128 sample headers × 24 B — unused slots filled with MSVC `0xCD`), pattern
data kept in standard XM packing verbatim, and samples re-encoded as PS2 SPU
ADPCM (sample lengths are in 16-byte ADPCM blocks; ×16 equals the 0xB060
blob size exactly).  `RebuildXM` (`pkg/eqoa/xm.go`) reverses the conversion
back to playable `.xm` files during `convert`: it decodes the ADPCM,
delta-encodes the PCM, and synthesizes the stripped 60-byte text header.
All 21 modules on the beta disc validate in libopenmpt.

Sound effects export directly as FLAC (pure-Go encoder, `mewkiz/flac`) —
verified bit-identical to the decoded PCM.  The `.xm` files are the archival
masters for music; render them to audio with any libopenmpt-based player
(`openmpt123 --render`) if a fixed waveform is preferred.

## Fullscreen images (.16 files)

`LOADING*.16` / `ERROR*.16` on disc are headerless 640×448 16-bpp images in
GS PSMCT16 order: little-endian `uint16`, red in bits 0–4, green 5–9, blue
10–14, alpha bit 15 (exactly 573,440 bytes). `convert` turns them into PNG.

## Zones

Zone ESFs (`TUNARIA.ESF` etc.) hold terrain in world-space coordinates, an
AABB tree of `ZoneRoom`s linking to sprites, `ZonePreTranslations` (LOD
centers / terrain sub-block offsets), `CollBuffer` collision meshes, and
`ZoneActors` placement records. `reader convert-zone` groups geometry per
Zone object and re-centers it; `reader scene` dumps actor placements to JSON.

## Practical notes for implementers

1. **Trust the magic, not the file extension** — some `.ESF` files on disc
   are CSF-compressed, some are raw.
2. **ObjectVersion changes layouts.** Parsers must branch on it (see the
   0x2600 and 0x2400 parsers for examples of version-gated fields).
3. **Bind-pose rendering proves nothing about your skeleton parse.** At bind
   pose, skinning matrices are identity regardless of how wrong your joint
   transforms are. Validate skeletons with *animated* poses. This masked a
   major bug in this tool for weeks — see
   [ANIMATION.md](ANIMATION.md#trap-2-the-skeleton-stores-world-space-transforms).

## Particle emitters

Fires, cascading water, smoke and spell effects are runtime particle systems,
not geometry. On disc:

```
0xC100 ParticleSprite
  0xC101 header            u32 definition dictID
  0xC000 ParticleDefinition (NumberOfSubObjects is a format flag, not a count)
    0xC010                 u32 texture dictID
    0x1000 Surface         the particle sprite image
    0xC020                 flat parameter block:
                           u32 textureDictID, i32 blendMode, i32 zWrite,
                           i32 zTest, i32 textureConfig, i32 motifCount-1,
                           then per motif (name[32] for motif>0):
                             13× f32 friction, birthrate(+var), lifespan(+var),
                                 velocity(+var), startSize(+var), endSize(+var),
                                 inheritVelocity, deltaSpawn
                             2× RGBA startColorVar, endColorVar
                             32× RGBA gradient (colour over life)
                             f32 gradientRepeat
                             6× vec3 inner/outer offset+hpr, nozzle axis+hpr
                             i32 gravityOn (ObjectVersion ≥ 1 only)
```

Emitters nested in a `GroupSprite` (0x2C00) — e.g. a wall-torch flame — are
positioned by the group's 0x2C30 member array: per member
`[u32 dictID, vec3 rot, f32 scale, vec3 pos]`, index-aligned with the 0x2C20
sprite array; local matrix `T(pos)·R_euler(rot)·S(scale)`.

### Export representation (glTF)

glTF has no particle system, so eqonvert emits each emitter as an `Emitter_<id>`
node — a small octahedron marker tinted with the effect's start colour — at the
emitter's world position, with the full recipe on the node `extras`:

```jsonc
{
  "effect": "particle",
  "dict_id": "0x19E16B19",
  "texture": 120,            // glTF texture index of the particle sprite
  "sprite": { "def_ref": 0, "definition": { /* full ParticleDefinition */ } }
}
```

A consuming runtime recreates the live effect from `extras` + the referenced
texture; a plain glTF viewer just shows the marker.
