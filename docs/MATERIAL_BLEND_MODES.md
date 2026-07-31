# Material blend modes, wrapping and alpha

Decoded from the retail client (`ghidra-support` snapshot, SLUS char-creation build). Every value
below is read off a GS register encoding in raw disassembly, not inferred from behaviour — where
something *is* inferred, it says so.

Motivation: `blendMode` and `wrapMode` were both parsed by `pkg/eqoa` and read by nothing. That
made two questions unanswerable — whether a dropped second layer was visually significant, and
why some surfaces tiled when they should not — and both turned out to matter.

## Where the fields live

| Field | In the ESF | In `VIMaterial` | Written by |
|---|---|---|---|
| `blendMode` | layer stride +0x3c region | `+layerIdx*0x90 + 0x3c` | `SetLayerBlendMode__10VIMateriali16VIAlphaBlendMode` @ `0x003de628` |
| `wrapMode` | layer +8 | layer flags `+0x34`, bits 1–2 | `SetLayerWrapMode` @ `0x003de4d8` |

Note the `VIMaterial` layer stride is **0x90**, not the 68-byte ESF file stride — the in-memory
layout is not the on-disc one.

The C++ enum name survives in the mangled symbol: `VIAlphaBlendMode`. Default is `0`
(`Init__10VIMateriali` @ `0x003dde78`).

## Blend modes

`ConstructTextPass` @ `0x003deb80` and `ConstructSolidFillPass` @ `0x003defc8` switch on the value
and write GS register `0x42` (`ALPHA_1`), always with `FIX = 0x80` (= 1.0).

GS ALPHA is `A[1:0] B[3:2] C[5:4] D[7:6] FIX[39:32]`, computing `(A−B)*C >> 7 + D`, where A/B/D
select `0=Cs, 1=Cd, 2=zero` and C selects `0=As, 1=Ad, 2=FIX`.

| Mode | ALPHA | A,B,C,D | Formula | Meaning |
|---|---|---|---|---|
| **0** | `0x80_00000064` | Cs, Cd, FIX, Cd | `Cs` | **Opaque.** `PRIM.ABE` is cleared, so blending is off entirely |
| **1** | `0x80_00000048` | Cs, 0, As, Cd | `Cs*As + Cd` | **Additive**, alpha-scaled |
| **2** | `0x80_00000042` | 0, Cs, As, Cd | `Cd − Cs*As` | **Subtractive** |
| **3** | `0x80_00000044` | Cs, Cd, As, Cd | `(Cs−Cd)*As + Cd` | **Standard alpha blend** |
| **4** | `0x80_00000089` | Cd, 0, As, 0 | `Cd * As` | **Multiply framebuffer by source alpha** — a darken pass |

Values ≥ 5 fall through to the mode-0 block (`sltiu v1,a0,0x5`).

Also derived from the same field:

- `PRIM.ABE = (blendMode != 0)`
- `PRIM.FGE = fogEnabled && (blendMode != 4)` — fog is force-disabled for the darken pass only.

### Mode 4 does not use the layer's colour

`0x89` decodes to `A=Cd, B=zero, C=As, D=zero`. `Cs` never enters the equation, so **the layer's
RGB is never read by the blender** — only its alpha carries signal. This is why those layer-1
textures look like solid black images: the colour channel is unused payload.

Consequence for the exporter: dropping a mode-4 layer is **only** visually neutral where its alpha
is 255. Where alpha < 255 the pass darkens the framebuffer, and dropping it loses that darkening.
glTF has no multiply blend mode, so dropping remains the least-wrong approximation — but it is a
known lossy step, not a no-op. The layer is recorded in material extras so it is at least
addressable (see below).

## Alpha is halved for every non-zero blend mode

At `0x003def10` and `0x003df1b0`, the layer colour's alpha is scaled `a/255 * 128` (PS2 convention,
128 = 1.0) and then multiplied by `0.5` when `blendMode != 0`.

So a layer colour of `(255,255,255,128)` at blend 3 reaches the GS at an effective **0.25**, not
0.5. This explains why measured partial-alpha layer values (128 / 102 / 77 with neutral RGB) render
fainter than a naive import produces.

## Alpha testing keys off the same field

`ConstructTextPass` @ `0x003dec14` selects GS `TEST_1`:

| Condition | TEST_1 | Meaning |
|---|---|---|
| `blend == 0` | `0x3080b` / `0x5080b` | `ATE=1, ATST=GEQUAL, AREF=128, AFAIL=KEEP` |
| `blend != 0` | `0x33001` / `0x53001` | `ATE=1, ATST=NEVER, AREF=0, AFAIL=RGB_ONLY` |

**Blend 0 is therefore exactly glTF `MASK` with `alphaCutoff = 128/255`** — provided the alpha
being compared is the same number in both. That turned out to need checking, and the answer has a
caveat; see "What AREF=128 is compared against" below.

Modes 1–4 are all BLEND-class passes with Z-write suppressed.

The `0x5…` variants are selected when material flags bit0 (ZTest) is set, giving `ZTST=GEQUAL`;
flags bit1 is ZWrite → `ZBUF.ZMSK`.

### What AREF=128 is compared against

The fragment alpha the test sees is `Av = (texAlpha × RGBAQ.A) >> 7` (TFX is MODULATE — see
"Texture function" below), where `RGBAQ.A` is the material layer colour's alpha rescaled `a/255 ×
128` by `ConstructTextPass` @ `0x003dee94`. Measured across all `CHAR*` content: **2,736 of 2,739
blend-0 materials carry layer colour alpha 255**, i.e. `RGBAQ.A = 128`, i.e. `Av = texAlpha`
unmodified. Partial layer alphas (26 / 64 / 89 / 102 / 128 / 153 / 191 / 204) occur almost
exclusively on blend 1 and 3 materials — the modes where alpha actually blends. So for the
alpha-tested path the compare is directly against the texture's own alpha byte.

That byte is a **full 0–255 range, not the PS2 0–128 convention.** Read straight out of the ESF
CLUT bytes:

| Content | CLUT entries | alpha > 128 | alpha == 128 | mode |
|---|---|---|---|---|
| `CHAR*` (827 surfaces) | 211,712 | 93.71% | 0.10% | 255 (×183,701) |
| `ITEM*` (423 surfaces) | 108,288 | 97.91% | 0.01% | 255 (×104,559) |
| `ARENA` (126 surfaces) | 32,256 | 97.01% | 0.02% | 255 (×30,037) |

Nothing clusters at 0x80. So `AREF = 128, GEQUAL` on 0–255 data is a cutoff at exactly **50%**, and
the doc's original reading — blend 0 ≡ `MASK` at `alphaCutoff` 0.5 — is right about the hardware.

It is *not* what the exporter currently emits, because `pkg/eqoa/surface.go` (~line 48) rescales
palette alpha as `a >= 128 ? 255 : a*2` on the stated premise that "PS2 palette alpha is 0-128".
The measurements above do not support that premise. The rescale is monotonic, so it does not
reorder anything, but it moves the effective cutoff: after `a*2`, a raw alpha of 64 exports as 128
and passes `alphaCutoff 0.5`. **The exported MASK cutoff sits at raw 64 where the hardware's sits
at raw 128** — roughly twice as permissive — and every raw value in 128–254 is flattened to fully
opaque. Confidence: high on the measurement, high that the premise is wrong for this content;
whether the rescale should change is a separate question with its own regression surface (the
zone/foliage soft-key behaviour was tuned against it) and is not decided here.

## Wrap mode is a bitmask, not an enum

`SetLayerWrapMode` packs it into layer-flag bits 1–2; `ConstructTextPass` @ `0x003ded10` unpacks
bit 1 into `CLAMP.WMS` and bit 2 into `CLAMP.WMT` (GS reg `0x08`; 0 = REPEAT, 1 = CLAMP).

| `wrapMode` | U / S | V / T |
|---|---|---|
| 0 | REPEAT | REPEAT |
| 1 | CLAMP | REPEAT |
| 2 | REPEAT | CLAMP |
| 3 | CLAMP | CLAMP |

Values > 3 are rejected (return −1). `REGION_CLAMP` / `REGION_REPEAT` are never emitted.

Measured distribution on the Frontiers disc:

| Content | Result |
|---|---|
| `CHAR.ESF` + `ITEM.ESF` (3,094 materials) | `wrapMode` 0 throughout |
| `ARENA` / `LAVASTM` / `PLANESKY` / `FX` | 33 materials at `wrapMode 3` |

So this matters for zones and not for characters, which is why it went unnoticed while character
export was the focus.

## Corroboration

Independent call sites that set a blend mode by constant, consistent with the table:

- `Init__12VIAtmosphere` @ `0x003f8050` — clouds → 3; sun / moon / star-glow → 1 with ZWrite and
  ZTest off and LOD bias 1.0 (textbook additive glow).
- `VITrailFxInit` @ `0x00450228`, `VILightningFxInit` @ `0x00422dc8` — both → 1, ZWrite off.
- `VIParticleDefinition::SetBlendMode` @ `0x0042b360` passes 0–4 through unchanged from
  `VIParticleDefinitionEx::BLENDMODE`, so particles share this enum space.

`ConstructWirePass` @ `0x003df268` ignores `blendMode` and hardcodes `0x64`.

## Texture function: what feeds `As`

Previously open; now closed via the caller chain, no GS dump needed.

`VIMaterialPass` is not a separate object. `AddMaterial__8VIRasteriPP10_sceDmaTag` @ `0x003e5588`
calls `Cache__13VIEESurfCache…R14VIMaterialPass…` @ `0x003efe68` (call site `0x003e5620`) passing
`material + layerIdx*0x90 + 0x30` as the `VIMaterialPass&`. **`VIMaterialPass` *is* the material's
per-layer struct, based at layer offset `+0x30`.** Seven independent offset agreements confirm it:

| VIMaterialPass | Layer field | Written by `Set__13VIEESurfCache` as |
|---|---|---|
| `+0x00` | `+0x30` texture index | (read by AddMaterial to skip untextured layers) |
| `+0x04` | `+0x34` layer flags | read for TFX and mag-filter |
| `+0x34` | `+0x64` LOD bias (default 0.8) | `TEX1.K` |
| `+0x50` | `+0x80` | `TEX0` (GS reg `0x06`) |
| `+0x60` | `+0x90` | `TEX1` (reg `0x14`) |
| `+0x70` | `+0xa0` | `MIPTBP1` (reg `0x34`) |
| `+0x80` | `+0xb0` | `MIPTBP2` (reg `0x36`) |

So the TFX bit is layer-flags bit0 — the bit `SetLayerModulate` @ `0x003de480` writes, as suspected.
At `0x003f0604`–`0x003f0620`:

- `TEX0.TCC` (bit 34) is set **unconditionally**. Texture alpha is always used; there is no
  RGB-only path.
- `TEX0.TFX` (bits 35–36) gets bit0 set when layer-flags bit0 is **clear** → `TFX = 1 = DECAL`.
  When layer-flags bit0 is **set** → `TFX = 0 = MODULATE`.

`Init__10VIMateriali` @ `0x003dde78` writes `1` to every layer's flags word, so **modulate is on by
default**. `ParseMaterial` never calls `SetLayerModulate`; the two flag writers it does call —
`SetLayerWrapMode` (masks `~0x6`, bits 1–2) and `SetLayerFillType` (bit 5) — both preserve bit0. All
nine `SetLayerModulate` call sites are in atmosphere / lightning / trail / sky / particle / HUD code
and all pass `1`. Nothing in the engine ever turns modulate off.

**Therefore, for every ESF material: `TFX = MODULATE`, and `As = (texAlpha × RGBAQ.A) >> 7`**, with
`RGBAQ.A` the layer colour alpha rescaled to 0–128 and halved for blend ≠ 0. Confidence: high —
register encodings read in raw disassembly, with the struct identification corroborated by seven
offset agreements including two constants (`0.8` LOD bias, `-1` cache tag) set by `Init`.

Not established: whether per-vertex lighting (`SetVertexLighting`) modulates alpha as well as RGB.
The VU1 side was not traced. It does not affect the alpha-tested path, where the measured layer
alpha is 255 on 2,736 of 2,739 blend-0 character materials.

## Confidence

High for all five blend modes and all four wrap values: these are direct register encodings read
in raw disassembly against the documented GS formats, with three independent corroborating call
sites. Modes 2 and 4 have no in-engine call site setting them by constant (only the particle
passthrough), so their *artistic intent* is inferred — their *arithmetic* is not.

## What the exporter does with this

`wrapMode` is applied: layers with a non-zero mask get a glTF sampler with the corresponding
`CLAMP_TO_EDGE` axes. Because wrapping is a per-layer property while textures are keyed by surface
DictID, a texture used at two different wrap settings produces two glTF `Texture` objects sharing
one `Source`. `wrapMode 0` emits no sampler at all, so the common case is unchanged.

`blendMode` is **not** mapped onto `alphaMode` by default, and this is the interesting result of
the whole exercise: being faithful to the hardware made the export worse.

Forcing `alphaMode = BLEND` wherever `blendMode != 0` changes 149 of 5,652 character materials
(2.6%) and 73 of 1,591 on ARENA (4.6%), always in the direction of more blending. Visually it is a
regression. On PS2 those passes draw in a fixed, authored order with Z-write suppressed; glTF has
no way to express draw order, so a BLEND material lands in the viewer's transparent pass and gets
ordered by camera distance instead. Layered geometry then drops out — on
`CHAR_phantom_animated_0x5B1D6121` the chains hanging over the robe vanish when viewed head-on,
which is much more objectionable than the chains being too opaque.

So the disassembly is right about the hardware and wrong about the export. It is available behind
`--blend-from-material` for inspection, and the blend mode is recorded in extras regardless — which
is the better home for it. A renderer that controls its own draw order can act on it; a generic
glTF viewer should not be handed an ordering problem it cannot solve.

Note the contrast with `wrapMode`, decoded in the same pass: that one translates cleanly, because
glTF samplers express exactly what the GS CLAMP register does. Whether a hardware fact survives
translation depends on whether the target format can represent it, not on how well it was decoded.

## The blend-0 / BLEND-upgrade conflict: resolved

Taken literally, `blend == 0` means MASK — and applying that flipped ~1,173 character materials
from BLEND to MASK, apparently undoing the visually-validated garment fix. That looked like the
disassembly and the rendered result disagreeing. They do not. The conflict was internal to the
exporter's own heuristic.

**The client does not override the blend mode at runtime.** `SetLayerBlendMode` @ `0x003de628` has
exactly 17 call sites in the whole binary:

| Caller | Mode set |
|---|---|
| `ParseMaterial__10VIESFParse` @ `0x0040c8e8` | whatever the ESF says |
| `Init__12VIAtmosphere` ×4, `ProcessTransitions__12VIAtmosphere` | 3 / 1 |
| `VILightningFxInit`, `VITrailFxInit`, `SetSky__7VIScene` | 1 |
| `VIParticleDefinition::SetBlendMode` ×2 | passthrough |
| `CreateDefaultResources__8VIRaster`, `CreateResources__7VIRFont` ×2 | fixed |
| `FUN_0060ac48` (additive shimmer over an attached CSprite slot-4 sprite) | 1 |
| `FUN_0060b6e8` (fullscreen fade quad), `FUN_0066b638` (2D HUD icon) | 3 / 0 |

None of them is character, equipment or appearance code. Checked directly: the entire `VICSprite`
dressing API — `SetHair` @ `0x00402520`, `SetHelm` @ `0x004028d0`, `SetRobe` @ `0x004021b0`,
`SetArmorSet`, `SetArmorSlot`, `SetFace`, `AttachItem` — only ever calls `SetLayerTexture` and
`SetLayerColor`. `SetHelm` copies the material palette and tints it; it never touches blend state.
**The ESF blend field is the effective value.** Confidence: high (complete xref enumeration; the
setter is a bare `sw a2,0x3c(a1)`, so only a direct struct write could bypass it, and none was
found).

**There is also no separate pass for worn equipment.** `ConstructDMAPacket__10VIMaterial` @
`0x003dea00` is the sole dispatcher, reached from `AddMaterial__8VIRaster` for every material:
layer-flags bit5 → `ConstructWirePass`; texture index ≠ −1 → `ConstructTextPass`; otherwise
`ConstructSolidFillPass`. `AREF` is a literal immediate inside `ConstructTextPass`; there is no
per-object or per-pass override. Confidence: high.

### What the conflicted materials actually are

They are **hair and head-slot geometry**, not garments. Three lines of evidence agree:

- `SetHair__9VICSprite` textures 9–11 material slots (three loops of three, plus two conditional
  slots) from an 8-entry hair-texture table (`GetHairTexture__13VICSpriteCust`, index clamped to
  0–7).
- `CHAR_Gnome_Female_animated_0x4C4084FC.glb` has exactly 9 BLEND materials out of 16; 8 of them
  reference hair sheet `0x221E7298` and one references `0xD499494B`.
- CHARCUST contains exactly 8 64×64 surfaces sharing one distinctive alpha signature; they are the
  hair sheets.

Across all `CHAR* / CHARSEL* / CHARCUST / CHARFACE` content, only **22 distinct surfaces** trigger
the blend-0 MASK→BLEND upgrade (590 material instances). Fourteen of the 22 are those hair sheets
and their duplicates.

### Why they trip the upgrade, and what the hardware does

Texel-weighted raw alpha for a hair sheet (`0x221E7298`, and the other seven are within a point):

| raw alpha | 0 | 1–63 | 64–127 | 128–254 | 255 |
|---|---|---|---|---|---|
| share of texels | 4.00% | 11.72% | 4.49% | 79.27% | 0.51% |

Its `TranslucentFraction` is **0.052** — barely over the exporter's `>= 0.05` upgrade bar. It is not
sheer cloth; it is a mostly-dense sheet with a thin feathered rim. What each path draws:

| | drawn |
|---|---|
| Hardware (blend 0, `AREF=128 GEQUAL` on raw alpha) | **79.79%** |
| glTF `MASK` @ 0.5 after eqonvert's `a*2` rescale (raw ≥ 64) | 84.28% |
| glTF `BLEND` | ~96%, feathered |

MASK lands within 4.5 points of the hardware; BLEND is the outlier by 16 points *and* moves hair
into the viewer's distance-sorted transparent pass — the same ordering failure already documented
for `--blend-from-material`. A sheet that is 80% dense cannot render as "edges only" under MASK, so
the symptom that motivated the upgrade is not what is happening on these particular materials.

The ESF field and the texture content in fact **agree** wherever it matters. Of the 22 upgraded
blend-0 surfaces, 20 have `TranslucentFraction` between 0.050 and 0.082. The genuinely translucent
character textures — `0xFB7915` (0.241), `0xCA0C6F4C` (0.240), `0x1E182697` (0.206) — are used by
blend 1 and blend 3 materials, the modes that actually blend. The 0.05 bar is simply loose enough
to sweep up feathered cutouts alongside real translucency.

### Consequence for the exporter

1. **APPLIED.** The `tf >= 0.05` bar in `alphaModeFor` was the thing miscalibrated, not the blend
   mapping. The bar is now **0.10** (`gradientBlendBar`, override with `--gradient-blend-bar`),
   which returns the hair sheets to MASK (within 4.5 points of hardware) while leaving the ≥0.20
   genuinely-sheer surfaces on BLEND. 1,187 materials across 368 files change BLEND → MASK; nothing
   else changes.

   The numbers alone did not justify this — they said MASK was faithful, not that it looked
   better — so it went through the A/B harness (`wrapaudit.html?case=hair`) and was confirmed
   visually on the Gnome Female hair-and-hat combination.

   Worth recording what the visual check added that the measurements missed: the clearest
   improvement is not on hair at all, it is on **headwear**. Under BLEND the hat brim rendered
   washed-out and semi-transparent with hair showing through a hollow-looking crown; under MASK it
   is a solid felt cone. The hair difference is real but subtle by comparison.
2. Do **not** drive `alphaMode` from `blendMode` even now that the field is trustworthy. glTF
   `alphaMode` is per-material, but eqonvert caches the decision per surface DictID
   (`surfaceAlphaMode`), and at least 13 non-opaque CHAR surfaces are referenced at more than one
   blend mode — `0xFB7915` and `0x91C013E3` appear at 0, 1 *and* 3. Counted only over the 173
   non-opaque paletted surfaces; fully-opaque ones were not checked, so 13 is a floor.
   A per-material rule cannot be expressed through a per-surface
   cache without restructuring, and the draw-order objection above still stands.
3. The palette rescale (see "What AREF=128 is compared against") is the one place where the exporter
   and the hardware genuinely disagree about a number. That is worth its own investigation; it is
   upstream of every alpha decision in this document.

Everything the exporter cannot express is written to material `extras` (see `matExtras` in
`pkg/gltf/appearance.go`) so it is preserved and auditable rather than silently dropped:

```json
{
  "eqoaBlendMode": 0,
  "eqoaWrapMode": 0,
  "eqoaLayerCount": 2,
  "eqoaExtraLayers": [
    { "texId": "0xCF45A209", "blendMode": 4, "wrapMode": 0, "color": [1, 1, 1, 1] }
  ]
}
```

On ARENA that preserves 62 layers across 1,591 materials which previously existed in the GLB only
as unreferenced image bytes.
