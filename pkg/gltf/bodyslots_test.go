package gltf

import (
	"testing"

	"github.com/average-bit/eqonvert/pkg/eqoa"
)

// A small humanoid: pelvis 0; spine 1; chest 2 with shoulders 3/7 and neck 11;
// each arm shoulder -> upper arm -> forearm -> hand with three fingers;
// head 12 under the neck with jaw and face; legs: thigh -> shin -> foot.
func testSkeleton() ([]int, [][3]float32) {
	parents := []int{-1, 0, 1,
		2, 3, 4, 5, // left: shoulder 3, upper 4, forearm 5, hand 6
		2, 7, 8, 9, // right: shoulder 7, upper 8, forearm 9, hand 10
		2, 11, // neck 11, head 12
		6, 6, 6, // left fingers 13-15
		10, 10, 10, // right fingers 16-18
		12, 12, // jaw 19, face 20
		0, 21, 22, // left thigh 21, shin 22, foot 23
		0, 24, 25, // right thigh 24, shin 25, foot 26
	}
	pos := [][3]float32{{0, 1, 0}, {0, 1.2, 0}, {0, 1.4, 0},
		{-0.2, 1.5, 0}, {-0.4, 1.5, 0}, {-0.6, 1.5, 0}, {-0.8, 1.5, 0},
		{0.2, 1.5, 0}, {0.4, 1.5, 0}, {0.6, 1.5, 0}, {0.8, 1.5, 0},
		{0, 1.6, 0}, {0, 1.7, 0},
		{-0.9, 1.5, 0}, {-0.9, 1.5, 0.05}, {-0.9, 1.5, -0.05},
		{0.9, 1.5, 0}, {0.9, 1.5, 0.05}, {0.9, 1.5, -0.05},
		{0, 1.65, 0.05}, {0, 1.75, 0.05},
		{-0.15, 0.9, 0}, {-0.15, 0.5, 0}, {-0.15, 0.1, 0},
		{0.15, 0.9, 0}, {0.15, 0.5, 0}, {0.15, 0.1, 0},
	}
	return parents, pos
}

func TestBodyRegions(t *testing.T) {
	parents, pos := testSkeleton()
	r, chest, head := BodyRegions(parents, pos)
	if chest != 2 || head != 12 {
		t.Fatalf("chest %d head %d, want 2 12", chest, head)
	}
	want := map[int]int{0: BodyLegs, 1: BodyChest, 2: BodyChest, 4: BodyChest, 5: BodyBracer, 6: BodyGloves, 14: BodyGloves,
		9: BodyBracer, 10: BodyGloves, 11: BodyNone, 12: BodyNone, 20: BodyNone, 21: BodyLegs, 22: BodyBoots, 26: BodyBoots}
	for j, w := range want {
		if r[j] != w {
			t.Errorf("joint %d region %d, want %d", j, r[j], w)
		}
	}
}

func TestBodySlotByMaterial(t *testing.T) {
	parents, pos := testSkeleton()
	h := &eqoa.HSpriteHierarchy{}
	for i := range parents {
		h.Joints = append(h.Joints, eqoa.Joint{ParentIndex: int32(parents[i]), Position: pos[i]})
	}
	vs := func(j uint8, n int) []eqoa.Vertex {
		out := make([]eqoa.Vertex, n)
		for i := range out {
			out[i].Joints[0], out[i].Weights[0] = j, 255
		}
		return out
	}
	m := &eqoa.Mesh{Type: 5, FaceGroups: []eqoa.FaceGroup{
		{MaterialIndex: 7, Vertices: vs(12, 9)}, // head: no slot
		{MaterialIndex: 4, Vertices: vs(2, 9)},  // chest
		{MaterialIndex: 9, Vertices: vs(5, 4)},  // bracer
		{MaterialIndex: 10, Vertices: vs(6, 4)}, // gloves
		{MaterialIndex: 2, Vertices: vs(0, 9)},  // legs
		{MaterialIndex: 8, Vertices: vs(23, 4)}, // boots
		{MaterialIndex: 3, Vertices: vs(1, 2)},  // a smaller chest piece loses
	}}
	got := BodySlotByMaterial([]*eqoa.Mesh{m}, h)
	want := map[int]int{4: 0, 9: 1, 10: 2, 2: 3, 8: 4}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(BodySlotByMaterial([]*eqoa.Mesh{m}, nil)) != 0 {
		t.Fatal("no skeleton must give no slots")
	}
}
