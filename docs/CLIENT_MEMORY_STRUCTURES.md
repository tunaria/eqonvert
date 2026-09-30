# EQOA Beta3 Client — In-Memory Runtime Structures

RE'd from SLUS_280.28 (Ghidra `support_client` snapshot + CLIENT overlay, live PINE reads on PCSX2).
Complements the *static* asset docs here (FORMATS/ANIMATION/ARMOR_TEXTURE_MAPPING/DICTIDS): this is the
**live in-memory layout** — actor/scene/player/appearance/animation state — for eqoa-xr's runtime recreation
and for correlating wire data (the server ObjectUpdate) with what the client renders. Addresses are EE RAM;
VIClient app base = *(0x004B0880) = 0x1FC2C74 (deterministic across sessions). Object-channel wire parse:
see EQOAGameServer research (BETA3_OBJECT_CHANNEL_PARSER). Character *texture* identity (DictID per
appearance slot) lives in ARMOR_TEXTURE_MAPPING.md — this doc gives the runtime OFFSETS that hold those
indices; the two join at the appearance slot.

## 1. VIClient app struct (base 0x1FC2C74) — player/global handles
| off | field |
|-----|-------|
| +0x68 | client state (5 = world-entering) |
| +0x380 | VIRaster (+lights) ; +0x400 VICamera |
| +0x61e0 | world resource VILoader ; +0x61dc loader-mode flag |
| +0x68a8 | APPEARANCE MANAGER (per-actor slots @ *(mgr+0x2c) + idx*500) |
| +0x69b0 | VIWalkCamera ; +0x6ae8/+0x6af4 self-bind position targets |
| +0x6da4 | self objId (0x2756FFFE) ; +0x6da8 SELF-ID (self-bind compares actor+0xc against this) |
| +0x70b0 | ACTOR TABLE (VICharacter*[], index = object-channel client index = server channel-1; slot0=self) |
| +0x7110 | DRDP instance ; +0x7114 server connection ; +0x7140 scene/VIWorld ptr |
| +0x99d0 | player world position (vec3) ; +0x304d0 world-loaded flag ; +0x304c4 current zone id |

## 2. Actor (VICharacter) — nearby actors / entity list
| off | field |
|-----|-------|
| +0x0c | objId (self 0x2756FFFE) ; +0x13 model DictID (== server modelid) |
| +0x24 | scene SPRITE handle (-> VISprite in scene table) ; +0x30 VIScene* ; +0x34 appearance mgr* |
| +0x3c/+0x40/+0x44 | render position (x,y,z) ; +0x48 appearance/resource key (= model DictID) |
| +0x4c | vtable ; +0x70 VIWalkControl (embedded) |
| +0x210..0x23c | transform matrix (4x3; wire convert scales 21/8/63/32) |
| +0x24d | transform-apply gate (0=apply) ; +0x252 (u16) visibility gate (&0x200 = hide) |
| +0x24d..0x252 | gear bytes (chest/bracer/gloves/legs/boots/helm) ; +0x280 (u64) last update time |
Created by FUN_006c2540 (type 0x82 -> VICharacter ctor 0x6bf748); self-bind FUN_00617C60 when
actor+0xc == app+0x6da8.

## 3. VIScene — zone / world state (actor+0x30; live 0x1FC9DB0)
| off | field |
|-----|-------|
| +0x2c0 | VIActor list ; +0x2e0 SPRITE TABLE base (0xc-byte entries {objPtr,type,id}; id*0xc) |
| +0x2794 | resource DICTIONARY (VIDictionary) ; +0x2798 raster/scene-ready flag (0 = not ready) |
VISprite (actor+0x24): +0x40 visibility (bit0=hidden), +0x44/+0x48/+0x4c world pos, +0x60 primary sprite id,
+0x68/+0x6c GEOMETRY handles (-1 = unresolved mesh). World resources loaded by
VIClient_OpenWorldResourceFiles (0x618a1c): data\char.esf + ambtrack + item + itemicon + ParseWorld(scene/
tunaria/zone*.esf) into the loader at app+0x61e0.

## 4. VICSprite — character appearance region (runtime OFFSETS; texture DictIDs -> ARMOR_TEXTURE_MAPPING.md)
| off | field |
|-----|-------|
| +0x198 | RACE (VICSpriteRace 0..9) ; +0x19c SEX/face-variant ; +0x1a0 MaterialPalette id |
| +0xf38 + slot*0x18 | armor slot flags(bit0=lock) / +0xf3c surface(-1=absent) / +0xf40 layer / +0xf44 default-tex / +0xf48 armorSet / +0xf4c tint |  (6 slots; slot5 = helm) |
| +0xfe4/+0xfe8 | face surface/layer ; +0x1100 hair style ; +0x1144 helm-present (!=-1 -> hide hair) ; +0x110 name ; +0x130 name color |
Setters: SetArmorSlot 0x4023a8, SetFace 0x402100, SetHair 0x402520, GetTintColor 0x4084e8 (dye table
0x4afa08). These apply CHARCUST textures (ARMOR_TEXTURE_MAPPING.md) onto the material palette. The
appearance-manager SLOT (app+0x68a8, *(mgr+0x2c)+idx*500) holds the DATA the server ObjectUpdate fills:
name@+0x110, nameColor@+0x130, face@+0x134, hair@+0x138/+0x13c/+0x140, robe@+0x144/+0x148/+0x14c,
armorSet[6]@+0x150+n*4, armorTint[6]@+0x168+n*4, model DictID@+0x100 (resolver key), dirty@+0xfc.

## 5. Runtime animation / locomotion (VICSprite) — TRACK-based
+0x194 = skel mode: **1 = player (DUAL-track: track0 lower body, track1 upper body)**, else single. (Runtime
source of the upper/lower composite behind the glTF upper/lower layer split.) Per track (stride 0x10):
current animID@+0x118c, play handle@+0x1190, playback type@+0x1194, prev anim@+0x116c, blend handle@+0x1170.
Per-anim table (stride 0x34 @ +0x1a8): HSprite ref@+0x1b0, playback type@+0x1b4, priority@+0x1b8, per-track
resource id@+0x1a8+id*0x34+t*4. Idle-fidget timer@+0x11b0. AnimIDs: 0x0 default, 0xE death, 0xF idle, 0x11
idle-variant, 0x16..0x36 actions, 0x2C combat. Setters SetAnimation(0x402bf0, priority-arbitrated, 0.1s
cross-fade), SetLocomotion(0x401928), SetAction(0x4015f8); VIWalkControl (actor+0x70) -> UpdateSprite
(0x454b68) picks locomotion per-frame from walk flags@+0xf8 + anim@+0x188; BlendToCenterTransform composites
tracks over 400 ms. Static channel format (0x2600 channel-major + 0x5000 RefMap->joint) is in ANIMATION.md;
this is the LIVE runtime state (which anim is playing + blend) that eqoa-xr needs to drive actors.

## 6. Item / inventory (in-memory)
Inventory list @ app+0x9ac8, bank list @ app+0x110f4. List format (FUN_00623de8): [varint count][u32 count]
[items...]. Each item = 0x2f4 (756)-byte record (parser FUN_006b83f0; matches server DumpItem in Item.cs):
| off | field | | off | field |
|-----|-------|-|-----|-------|
| +0x00 | StackLeft | | +0x38 | IsNoRent |
| +0x04 | RemainingHP | | +0x3c | Unk4 |
| +0x08 | Charges | | +0x40 | Attacktype |
| +0x0c | EquipLocation | | +0x44 | Weapondamage |
| +0x10 | Location | | +0x48 | Unk5 |
| +0x14 | key/serverKey (u32) | | +0x4c | Levelreq |
| +0x18 | ItemID (DictID) | | +0x50 | Maxstack |
| +0x1c | ItemCost | | +0x54 | Maxhp |
| +0x20 | Unk1 | | +0x58 | Duration |
| +0x24 | ItemIcon | | +0x5c | Classuse |
| +0x28 | Unk2 | | +0x60 | Raceuse |
| +0x2c | itemSlot | | +0x64 | Procanim |
| +0x30 | Unk3 | | +0x68 | IsLore (Beta3 last varint; retail adds Unk6/IsCraft) |
| +0x34 | IsNoTrade | | +0x6c | ItemName (UTF-16, cap 0x80) |
| +0xec | ItemDesc (UTF-16, cap 0x200) | | +0x2ec | stats sub-block |
Stats sub-block (FUN_006b8930): [varint statCount][statCount x {varint statIndex, varint statValue}]. All
scalars are zigzag varints on the wire; +0x14 key is a raw u32. ItemID (+0x18) is the item DictID.

## 7. VIWalkControl physics (actor+0x70; movement / locomotion)
Tunable params (InitParms 0x456548): +0x08=0.5, +0x0c/+0x20=3.0, +0x10 RUN speed 8.83, +0x14 WALK speed 4.42,
+0x18 max-pitch pi/2, +0x28 max-speed clamp 0.6, +0x40/+0x44/+0x48 accel-decay (fwd/back/strafe),
+0x4c angular decay, +0x60 grounded/gravity enable, +0x64 scene-actor handle, +0x68/+0x6c pitch min/max
(-1.56/+1.56 rad), +0x70 ~0.52 (30deg).
Runtime state (integrated by UpdatePhysics 0x454e50 / Move 0x4540e0):
| off | field |
|-----|-------|
| +0xb0/+0xb4/+0xb8 | POSITION x,y,z |
| +0xbc/+0xc0/+0xc4 | ORIENTATION heading(yaw, fmod 2pi) / pitch(clamped) / roll |
| +0xc8/+0xcc/+0xd0 | linear VELOCITY x,y,z |
| +0xd4/+0xd8/+0xdc | angular velocity |
| +0xe0/+0xe4/+0xe8 | force/accel input (+gravity DAT_0x4b07e8/0x4b07f8 when grounded) |
| +0xec/+0xf0/+0xf4 | angular accel/target |
| +0x104 | current forward speed (from +0x18c target) ; +0x108 target heading |
| +0xf8 | movement dir flags (bit0 fwd/back, &0xfc strafe) ; +0x180 movement mode (0x54 ground, 0xa8 swim/fly) |
| +0x148 | landscape pitch clamp |
Drives the CSprite each frame (UpdateSprite 0x454b68 -> SetLocomotion). eqoa-xr: use +0xb0 position + +0xbc
heading (or actor+0x3c pos + transform +0x210) to place/orient actors; movement mode + speed pick the anim.

## Provenance / validation
Offsets from Ghidra decompiles of the named functions + live PINE reads (SLUS_280.28 in-world). The dye
table @0x4afa08 was validated live (FF FF FF / 5A 5A 5A / 00 B4 FF / ...). The CHARCUST getter formulas and
DictID map here match ARMOR_TEXTURE_MAPPING.md's independent extraction (cross-validated). Full source copy:
elfconv/re_analysis/CLIENT_DATA_STRUCTURES.md; CHARCUST hash decoder: elfconv/tools/charcust_texture_map.py.
