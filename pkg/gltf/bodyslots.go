package gltf

import (
	"math"
	"sort"

	"github.com/average-bit/eqonvert/pkg/eqoa"
)

// Body armor slot of a character material, read from the shape of the
// skeleton (track C3 of the Tunaria EQOA port; same rule as
// tools/eqoa/lib/appearance.js bodyRegions there).
//
// The old rule, slot = material palette index mod 5, is wrong: the body
// materials sit at different palette indexes on every model. Joint numbers
// do not hold either: vanilla DATA/CHAR.ESF player skeletons have 52 joints
// (torso 3, forearms 15/29, hands 16/30), the Frontiers ones 40 or 43 with
// other numbers. The rule here holds on every humanoid skeleton of both
// discs:
//   - the root (pelvis) and everything not painted below is legs;
//   - the chest is the nearest midline joint above the root with a mirrored
//     pair of off-midline children (the shoulders); it, the spine below it
//     and its subtree are chest;
//   - the neck is the chest's midline child whose subtree reaches highest,
//     when it sits above the chest; its subtree is no slot; the head joint
//     is the first joint down the neck chain with more than one child;
//   - each arm follows the outermost child from the shoulder to the hand,
//     the first joint with 3 or more children: the hand subtree is gloves,
//     the joint before it (the forearm) bracer;
//   - each leg (an off-midline root child below the root): the thigh stays
//     legs, everything below it is boots.
// Each material takes the region most of its vertices hang on (first joint
// of each vertex); each slot keeps the material with the most votes.

// Body regions, in actor gear byte order.
const (
	BodyChest  = 0
	BodyBracer = 1
	BodyGloves = 2
	BodyLegs   = 3
	BodyBoots  = 4
	BodyNone   = -1
)

const midlineEps = 0.02

// skinSlotNote is the mapping note for the skeleton rule.
const skinSlotNote = "body slot from the skeleton: each material takes the body region most of its vertices hang on (chest = spine up to the shoulders, clavicles and upper arms; bracer = forearm; gloves = hand and fingers; legs = pelvis and thighs; boots = below the knee; the head is none), one material per slot"

// BodyRegions returns the region of every joint plus the chest and head
// joint indexes (-1 when not found). parents[i] = -1 marks the root; pos is
// the model-space bind position (y up).
func BodyRegions(parents []int, pos [][3]float32) (regions []int, chest, head int) {
	n := len(parents)
	kids := make([][]int, n)
	root := -1
	for i, p := range parents {
		if p < 0 {
			if root < 0 {
				root = i
			}
		} else if p < n {
			kids[p] = append(kids[p], i)
		}
	}
	regions = make([]int, n)
	for i := range regions {
		regions[i] = BodyNone
	}
	chest, head = -1, -1
	if root < 0 {
		return
	}
	x := func(j int) float64 {
		if j < len(pos) {
			return float64(pos[j][0])
		}
		return 0
	}
	y := func(j int) float64 {
		if j < len(pos) {
			return float64(pos[j][1])
		}
		return 0
	}
	lateral := func(j int, up bool) []int {
		var out []int
		for _, c := range kids[j] {
			if math.Abs(x(c)) > midlineEps && ((up && y(c) >= y(root)) || (!up && y(c) < y(root))) {
				out = append(out, c)
			}
		}
		return out
	}
	mirrored := func(l []int) bool {
		neg, posv := false, false
		for _, c := range l {
			if x(c) < 0 {
				neg = true
			}
			if x(c) > 0 {
				posv = true
			}
		}
		return neg && posv
	}
	paint := func(j, r int) {
		st := []int{j}
		for len(st) > 0 {
			k := st[len(st)-1]
			st = st[:len(st)-1]
			regions[k] = r
			st = append(st, kids[k]...)
		}
	}
	paint(root, BodyLegs)
	queue := append([]int(nil), kids[root]...)
	for len(queue) > 0 {
		j := queue[0]
		queue = queue[1:]
		if math.Abs(x(j)) > midlineEps {
			continue
		}
		if mirrored(lateral(j, true)) {
			chest = j
			break
		}
		queue = append(queue, kids[j]...)
	}
	if chest >= 0 {
		for j, guard := chest, 0; j != root && j >= 0 && guard < n; j, guard = parents[j], guard+1 {
			regions[j] = BodyChest
		}
		for _, c := range kids[chest] {
			paint(c, BodyChest)
		}
		top := func(j int) float64 {
			m := y(j)
			st := append([]int(nil), kids[j]...)
			for len(st) > 0 {
				k := st[len(st)-1]
				st = st[:len(st)-1]
				m = math.Max(m, y(k))
				st = append(st, kids[k]...)
			}
			return m
		}
		for _, c := range kids[chest] {
			if math.Abs(x(c)) <= midlineEps && (head < 0 || top(c) > top(head)) {
				head = c
			}
		}
		if head >= 0 && y(head) > y(chest) {
			paint(head, BodyNone)
			for guard := 0; guard < n && len(kids[head]) == 1; guard++ {
				head = kids[head][0]
			}
		} else {
			head = -1
		}
		for _, a := range lateral(chest, true) {
			j, prev := a, -1
			for guard := 0; guard < 16 && j >= 0; guard++ {
				if len(kids[j]) >= 3 {
					if prev >= 0 {
						paint(prev, BodyBracer)
					}
					paint(j, BodyGloves)
					break
				}
				prev = j
				next := -1
				for _, c := range kids[j] {
					if next < 0 || math.Abs(x(c)) > math.Abs(x(next)) {
						next = c
					}
				}
				j = next
			}
		}
	}
	for _, t := range lateral(root, false) {
		for _, c := range kids[t] {
			paint(c, BodyBoots)
		}
	}
	return
}

// SlotsFromVotes maps material key -> slot from per-key votes
// [chest, bracer, gloves, legs, boots, none], keys taken in ascending order.
func SlotsFromVotes(votes map[int][6]int) map[int]int {
	keys := make([]int, 0, len(votes))
	for k := range votes {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	type pick struct{ key, n int }
	best := map[int]pick{}
	for _, key := range keys {
		v := votes[key]
		r := 0
		for k := 1; k < 6; k++ {
			if v[k] > v[r] {
				r = k
			}
		}
		if r == 5 || v[r] == 0 {
			continue
		}
		if cur, ok := best[r]; !ok || v[r] > cur.n {
			best[r] = pick{key, v[r]}
		}
	}
	out := map[int]int{}
	for slot, p := range best {
		out[p.key] = slot
	}
	return out
}

// BodySlotByMaterial maps material palette index -> body slot (0..4) for a
// character's skinned meshes (type 5). Empty without a skeleton.
func BodySlotByMaterial(meshes []*eqoa.Mesh, h *eqoa.HSpriteHierarchy) map[int]int {
	if h == nil || len(h.Joints) == 0 {
		return map[int]int{}
	}
	parents := make([]int, len(h.Joints))
	pos := make([][3]float32, len(h.Joints))
	for i, j := range h.Joints {
		parents[i] = int(j.ParentIndex)
		pos[i] = j.Position
	}
	regions, _, _ := BodyRegions(parents, pos)
	votes := map[int][6]int{}
	for _, m := range meshes {
		if m == nil || m.Type != 5 {
			continue
		}
		for _, g := range m.FaceGroups {
			v := votes[int(g.MaterialIndex)]
			for _, vert := range g.Vertices {
				j := int(vert.Joints[0])
				if vert.Weights[0] == 0 {
					j = 0
				}
				r := BodyNone
				if j < len(regions) {
					r = regions[j]
				}
				if r < 0 {
					v[5]++
				} else {
					v[r]++
				}
			}
			votes[int(g.MaterialIndex)] = v
		}
	}
	return SlotsFromVotes(votes)
}
