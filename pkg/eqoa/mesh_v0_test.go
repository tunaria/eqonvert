package eqoa

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The float record (ObjectVersion 0): two strips of 3 and 4 vertices.
func TestParsePrimBufferV0(t *testing.T) {
	var b bytes.Buffer
	w := func(v ...interface{}) {
		for _, x := range v {
			binary.Write(&b, binary.LittleEndian, x)
		}
	}
	vtx := func(x, z float32) {
		w([8]float32{x, 2, z, x / 4, z / 4, 0, 1, 0}, [4]uint8{128, 64, 255, 128})
	}
	w(int32(1), int32(2), int32(7))
	w(int32(3), int32(0))
	vtx(0, 0)
	vtx(1, 0)
	vtx(0, 1)
	w(int32(4), int32(1))
	vtx(0, 0)
	vtx(1, 0)
	vtx(0, 1)
	vtx(1, 1)
	body := b.Bytes()
	obj := &ESFObject{Header: ObjectHeader{ObjectType: 0x1200, ObjectVersion: 0}, BodyOffset: 0, BodySize: int32(len(body))}
	m, err := ParsePrimBuffer(bytes.NewReader(body), obj, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != 0 || len(m.FaceGroups) != 2 || m.FaceGroups[1].MaterialIndex != 1 {
		t.Fatalf("got type %d, %d groups", m.Type, len(m.FaceGroups))
	}
	v := m.FaceGroups[1].Vertices[3]
	if v.Pos != [3]float32{1, 2, 1} || v.UV != [2]float32{0.25, 0.25} || v.Normal != [3]float32{0, 1, 0} || v.Color != [4]float32{1, 0.5, 1, 1} {
		t.Fatalf("vertex %+v", v)
	}
	want := []uint32{0, 2, 1, 1, 2, 3}
	for i, x := range want {
		if m.FaceGroups[1].Indices[i] != x {
			t.Fatalf("indices %v", m.FaceGroups[1].Indices)
		}
	}
}
