# EQOA Armor / Character Texture Mapping — RE status & resume plan

**Status (2026-07-13):** architecture *fully confirmed* from the client's own C++
symbols; concrete per-texture labels are **blocked on a live memory read** because
the lookup tables are runtime-populated. This doc is the pick-up-cold plan.

## The question

CHARCUST.ESF (`data2\charcust.csf`) = **8 SimpleSprites** (helmet/head geometry — the
8 GLBs) + **137 standalone Surfaces** (body-armor SKIN textures, mostly 64×64). Body
armor in EQOA is **not geometry** — it is a *texture swap* onto the shared body mesh's
material slots. Items map textures onto item geometry the same way. We extract the 137
skins today as anonymous PNGs; the goal is to **label each** (which armor set / body
slot / race) so we can name them and optionally emit armored-character GLBs.

## Confirmed mechanism — class `VICSpriteCust`

The client has an SN-Systems runtime symbol block (mangled C++ names). `VICSpriteCust`
is the body texture-swap engine. Body = one `VISkinSprite` with **5 material slots**
(`VICSpriteTextSlot`, proven by the applier loop `iVar8 = 0..4`).

Selection methods (addresses are from the **`slus_280.28_snapshot_data.elf` symbol
table** — see skew warning below):

| Method | Addr (symtab) | Role |
|---|---|---|
| `GetArmorSetTexture(VICSpriteRace, VICSpriteArmorSet, VICSpriteTextSlot)` | `0x004084e8` | body-armor skin per slot |
| `GetRobeTexture(slot)` | `0x00408458` | robe overlay |
| `GetHairTexture(i)` | `0x00408410` | hair |
| `GetFaceTexture(i,i,race)` | `0x00428840` | face (from separate CHARFACE) |
| `GetHelm(VICSpriteArmorSet)` | `0x004bdf50` | → the 8 SimpleSprite helmet meshes |
| `GetTintColor(VICSpriteTint)` | `0x00408480` | RGBA tint (not a texture) |
| `SetResources(VIRaster, VIDictionary)` | `0x004afa08` | **builds the tables from the loaded charcust dictionary** |
| `SetMaterialPal(VISkinSprite,i,VIRaster,VIDictionary)` | `0x00458458` | binds a material palette to the body |

Applier `FUN_004021b0` (Ghidra) loops the 5 slots; per slot it fetches a robe texture
and the armor-set texture and binds them via `FUN_003de380` / `FUN_003de428`.

Static data tables (symtab addrs): `$Race`@`0x4c6c60` (184 B), `$Hair`@`0x4c6d18`
(352 B), `$Robe`@`0x4c6e78` (32 B), `$Helm`@`0x4c6e98` (32 B), `$Tints` (14 × RGBA @
`0x4afa08` region), plus a table-init ctor (symtab-named `$Armor`) @ `0x4072b8` that
**zeroes** `$Race/$Hair/$Robe` at startup.

DictID hash (for reference) = polynomial `h = h*0x83 + c` over the asset name
(`FUN_003ceb10`; verified: `EruditeMale` → `0x320C0B47`). **Charcust textures are
addressed by dictionary index, not name-hash**, so hashing item names finds nothing —
confirmed (0 matches) and a raw-DictID scan of the ELFs is also 0.

## Why static analysis stops here

1. **Tables are runtime-populated.** In *both* snapshot ELFs the `$Race/$Hair/$Robe/
   $Helm` tables are all-zero and `$Tints`/armor colors are the default `0xFF000000`.
   `SetResources` fills them only when a character's charcust data loads.
2. **3-way address skew.** Ghidra snapshot ≠ `_data.elf` symtab ≠ retail. E.g. the
   `$Race` writer is `~0x40842c` in the Ghidra image but the ctor is `0x4072b8` in the
   symtab; `SetResources` "`0x4afa08`" from the symtab is *data* in Ghidra ("No function
   at 0x004afa08"). So a static `SetResources` replay is fragile AND yields no values.

## Resume plan — live PINE read (ground truth)

The `pine_*` MCP tools are available. Procedure:

1. `pine_ping` / `pine_get_status` — confirm PCSX2 is connected and identify the build
   (retail vs beta) so table addresses are correct. **Re-derive the live addresses for
   that build** — do NOT assume the symtab addresses above; they are for the snapshot,
   and skew is proven. Locate `VICSpriteCust::SetResources` / the `$Race`,`$Hair`,
   `$Robe`,`$Helm` tables in the *running* image (find them via the same symbol block in
   the live binary, or by xrefs to the accessor cluster).
2. Get to **character-select or in-game** (charcust loaded) — needs a savestate or live
   session at that screen.
3. `pine_read_range` the `$Race/$Hair/$Robe/$Helm` tables now that they're populated;
   entries are **dictionary indices** into the loaded charcust dictionary.
4. Map dictionary index → **charcust.csf Surface order** (the 137, in file order from
   `eqonvert inspect --json CHARCUST.CSF`).
5. Join `VICSpriteArmorSet`/`VICSpriteTextSlot`/`VICSpriteRace` → armor-set / slot /
   race **names** via the server DB: `itempattern.patternfam` (armor set),
   `equipslot` (slot). See `EQOAGameServer/EQOA_Master.sql`.
6. Validate: pick a known in-game armor set, confirm the surface it maps to is the
   texture we'd expect.

Deliverable when unblocked: a table `surfaceIndex(0..136) → {race, armorSet, slot}` →
human name, feeding the `eqonvert` extractor to name the 137 PNGs and (optionally) emit
armored-character GLBs by binding a skin set to a body's 5 material slots.

## Fallback (no emulator)

Structural/heuristic labeling from charcust.csf surface order + dimensions + the DB
enumeration of armor sets/slots. Usable but **not exact** — prior heuristic passes on
this codebase have been wrong before, so treat as provisional only.

## RESOLVED OFFLINE (2026-07-23) — the "blocked on live read" conclusion was wrong

The tables *are* runtime-populated with dictionary **indices**, but that was the wrong
thing to chase. `SetResources__13VICSpriteCust` @`0x004072b8` calls `FindTyped` ~140
times with the texture **DictIDs hardcoded as immediates**, and eqonvert names exported
textures **by DictID** — so the DictID→texture identity is fully recoverable from the
binary, no emulator needed. Extracted + **100% resolved** to exported files:

- **Face** table `0x4c6d18` (race×0x20 = 4 faces×2 genders): 10 races, **80/80** → Human/Elf
  in `DATA/CHAR/`, others in `DATA2/CHARFACE/` (Erudite's are the dark ones).
- **Hair** table `0x4c6e78` (8): **8/8** in `DATA/CHAR/`. Default per race = `hair[raceTable+0x40]`.
- **Robe** `0x4c6e98` (8): 8/8. **ArmorSet body** `0x4c6c60` (8 sets × 5 slots): 40/40.
- **Tint palette** `0x4afa08` (15 RGBA, hardcoded in `SetResources`): tint3 `(150,100,50)`
  brown & tint11 `(220,190,150)` tan are the skin tones.

Full spec: `eqoa-xr/tools/ui-loop/appearance_tables.json` (extractor
`scratchpad/extract_appearance.py`). Frontiers race table (per-race default face/hair
indices) read live from `slus_290.63_frontiers_beta_ee.bin` @`0x4EF4A0` (see CHAR_CREATION.md §1.8).

**Still open:** `race → default-armorSet` — NOT in `SetResources` (the preview build
`FUN_00653d00` sets only `SetHair`+`SetFace`, no skin-tint/armor). Trace the in-game default
appearance path (`VICSprite::Init` / char-record apply) or read the applied values from the dump.

`race → skin-tint index` was also open here; it is **answered below** — there is no such index,
because there is no race→skin-tint table at all.

## RESOLVED (2026-07-30): per-race skin colour is a baked material-layer modulate

Answers the `race → skin-tint index` item left open by the 2026-07-23 section above. Established by
static analysis of the retail SLUS char-creation snapshot (Ghidra `ghidra-support`) plus raw table
reads from `slus_280.28_snapshot_data.elf`. Supersedes the "race → skin tint TODO" in
`appearance_tables.json`.

**There is no race→skin-texture lookup and no race→skin-tint table.** Each race's CSprite material
palette in `char.esf` carries a per-layer RGBA modulate, applied by `ParseMaterial__10VIESFParse`
(`0x0040c6d8`) at parse time via `SetLayerColor`. That colour is the skin tone.

| race | modulate on the four shared flesh surfaces |
|---|---|
| Human, Elf, Halfling | `(255,255,255,255)` — neutral |
| **Erudite M/F** | **`(109,83,77,255)`** |
| Barbarian M | `(237,219,188,255)` |
| Barbarian F | `(238,220,189,255)` |
| DarkElf, Gnome, Dwarf, Troll | n/a — own texture sets, no shared flesh |

Shared flesh surfaces: `d88fea71`, `b0379a45`, `75bf0eb0`, `058d23d3`.

The Erudite ratio is `0.427 / 0.325 / 0.302`, against `0.45 / 0.36 / 0.19` measured independently
from exported GLB texture means. R and G agree closely; B differs because the measurement came from
*face* textures rather than the body modulate.

### Why it is not the tint table

`GetArmorSetTexture` (`0x00408410`) takes race in `a0` and **overwrites it before ever reading it**
(`_li a0,0x14` on the `armorSet == 0` path), so CHARCUST body skins are indexed
`[armorSet 1..8][slot 0..4]` only. Race is used in exactly one place: `SetFace` → `GetFaceTexture`
(`0x00408458`), `faces[race][face][sex]` at `0x004c6d18`.

The 15 tints at `DAT_004afa08` are dyes applied per armour slot, index 0 being white = no dye. There
is no second tint table.

Relevant to character creation specifically: the preview spawner `FUN_00653d00` calls only `SetHair`
and `SetFace`, never `SetArmorSlot` — so on the create and customise screens the baked layer colour
is exactly what the player sees. In world, `SetArmorSlot` overwrites the layer colour with the dye
tint on body slots 0–4, so the baked skin tone survives only on slots armour never touches.

### Material header layout (corrected)

The client reads:

```
u32   numLayers
u32   flags           (version > 1)
RGBA  emissive        (version > 2)
layer[numLayers]      68 bytes each
```

Layer: `flags u32 | textureDictID u32 | wrapMode u32 | blendMode u32 | RGBA | UV matrix 9f |
lodBias f | UV rate 2f`.

`ParseMaterialBody` previously read a DictID first whenever `version > 1`, shifting everything by
four bytes on the v=3 materials characters use. `numLayers` then decoded as 0, the layer loop never
ran, and the `numLayers == 0` fallback picked up what happened to be layer 0's texture ID — so
single-texture lookup worked by accident while **every second layer, every layer colour, and all
wrap/blend modes were silently discarded**. Fixed, with `pkg/eqoa/material_test.go` covering the
two-layer v=3 case, neutral single-layer, an implausible layer count, and truncation.

### RESOLVED (2026-08-01): the modulate is baked into a texture copy

**The problem was the colour space, not the value.** `baseColorFactor` multiplies in **linear**
space; the console multiplies texel by modulate in **gamma** space. Two attempts at emitting the
modulate as `baseColorFactor` failed on that mismatch — the first needed a `c^2.2` correction and
still read wrong, and both turned Gnome `(0.216, 0, 0)` and Ogre `(0.051, 0.041, 0.041)`
near-black. A non-white `material.color` also interacts with scene lighting, which showed as a
plastic sheen over the skin.

The exporter now multiplies the modulate into a **copy of the texture** (`bakeSkinTint`, on by
default, `--bake-skin-tint`). A per-texel multiply in image space is exactly what the hardware
does. `material.color` stays white, so nothing touches lighting and there is no sheen.

Keyed by `(surface, tone)`, so races sharing a texture at the same tone share one baked copy and a
race using it neutral keeps the original. Frontiers Erudite gains six images; Elf gains none.

The premise was verified by hashing texture content across the ten male models rather than trusting
the note above. Three body textures are shared by exactly Human, Elf, Erudite and Barbarian:

| race | modulate on the shared flesh textures |
|---|---|
| Human, Elf | `(255,255,255)` — neutral, uses the art as-is |
| Erudite | `(109,83,77)` |
| Barbarian | `(237,219,188)` |

**The suspected `2×` bias does not exist.** Neither `/255` nor `/128` looked right when tested
interactively, because the divisor was never the variable — the colour space was. Human's
`(128,128,128)` is simply a half-brightness modulate, not a unity value under a biased convention.

**The extreme modulates were never skin.** Gnome's `(36,36,36)` and `(128,0,1)` land on its cap and
beard; Dwarf's `(0,0,0)` lands on a non-skin material. Baking them is correct and visually verified
— those races render normally. They only blackened under `baseColorFactor` because the linear-space
multiply was wrong, not because the values were.

`SetSkinModulate` / `--skin-modulate` remains as the old `baseColorFactor` path, still off, kept
only for comparison.

Measured effect on the golden baselines: 55 character materials changed texture on Frontiers, 82 on
the base game, across 14 models — Barbarian, Dwarf, Erudite and Gnome — with **no** change to
`alphaMode`, `alphaCutoff` or `baseColorFactor`. That is the signature of the bake and nothing else.

The interactive tool used to settle this is `eqoa-xr/charcreate.html?tint=1`: an eyedropper that
samples the rendered face and applies a normalised tint to the body, reporting the effective
multiplier. It is what showed that no divisor of the stored value would work.

### Second-layer blend modes — decoded

No longer open. All five blend modes and the wrap bitmask were read off their GS register encodings;
see **`MATERIAL_BLEND_MODES.md`**. Briefly: mode 4 is a multiply pass that never reads the layer's
RGB, mode 0 is an alpha test at `AREF=128`, and blend/wrap/dropped-layer data is now preserved per
material in glTF `extras`.

## Related

**Runtime in-memory layout: `CLIENT_MEMORY_STRUCTURES.md`** (2026-09-10) — the live VICSprite appearance-region
offsets (armor slots @ +0xf38+slot*0x18, face +0xfe4, hair +0x1100, matPal +0x1a0, race +0x198), the
appearance-manager slot the server ObjectUpdate fills, plus actor/scene/player/animation structures. It joins
to THIS doc at the appearance slot: the runtime slot holds the armorSet/tint/face/hair indices; this doc
resolves those to texture DictIDs. Independent 2026-09-10 re-derivation of the getters
(GetArmorSetTexture 0x408410 race-dead, GetFaceTexture 0x408458, GetHairTexture 0x408440, GetRobeTexture
0x408480, GetHelm 0x4084d0, GetTintColor 0x4084e8, SetResources 0x4072b8; NOTE the "Confirmed mechanism"
symtab table above is the OLD skewed set — these Ghidra addrs are correct) + a 145-entry hash decoder
(elfconv/tools/charcust_texture_map.py) CONFIRMED this doc's map (armor 8x5, face 10x4x2, hair 8+default,
robe 4x2, helm 8).

Memory: `project_armor_texture_swap`, `project_model_naming`, `project_client_game_logic`,
`project_char_garment_alpha`, `project_char_dup_blank`, `project_eqoa_ui_tools`.
