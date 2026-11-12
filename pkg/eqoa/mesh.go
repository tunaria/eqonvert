package eqoa

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

type Vertex struct {
	Pos     [3]float32
	UV      [2]float32
	Normal  [3]float32
	Color   [4]float32
	Joints  [4]uint8
	Weights [4]uint8
	VGroup  int16
}

type FaceGroup struct {
	MaterialIndex int32
	Vertices      []Vertex
	Indices       []uint32
}

type Mesh struct {
	Type       int32
	PackingPos float32
	PackingUV  float32
	FaceGroups []FaceGroup
}

func ParsePrimBuffer(r io.ReadSeeker, obj *ESFObject, order binary.ByteOrder) (*Mesh, error) {
	data, err := obj.ReadBody(r)
	if err != nil {
		return nil, err
	}

	if obj.Header.ObjectVersion == 0 {
		return parsePrimBufferV0(data, order)
	}

	if len(data) < 28 {
		return nil, fmt.Errorf("PrimBuffer data too short for header: %d", len(data))
	}

	br := bytes.NewReader(data)

	var pbType, nmats, numFaceGroups, totalVerts, p1, p2, p3 int32
	if obj.Header.ObjectVersion > 1 {
		var dictID uint32
		binary.Read(br, order, &dictID)
	}

	binary.Read(br, order, &pbType)
	binary.Read(br, order, &nmats)
	binary.Read(br, order, &numFaceGroups)
	binary.Read(br, order, &totalVerts)
	binary.Read(br, order, &p1)
	binary.Read(br, order, &p2)
	binary.Read(br, order, &p3)

	// Sanity checks
	if numFaceGroups < 0 || numFaceGroups > 10000 {
		return nil, fmt.Errorf("invalid numFaceGroups: %d", numFaceGroups)
	}
	if p1 < 0 || p1 > 31 || p2 < 0 || p2 > 31 {
		return nil, fmt.Errorf("invalid packing factors: p1=%d, p2=%d", p1, p2)
	}

	m := &Mesh{
		Type:       pbType,
		PackingPos: float32(1.0 / math.Pow(2, float64(p1))),
		PackingUV:  float32(1.0 / math.Pow(2, float64(p2))),
		FaceGroups: make([]FaceGroup, 0, numFaceGroups),
	}

	for fi := 0; fi < int(numFaceGroups); fi++ {
		var numVerts, matIdx int32
		if err := binary.Read(br, order, &numVerts); err != nil {
			break
		}
		binary.Read(br, order, &matIdx)

		if numVerts < 0 || numVerts > 10000 {
			return nil, fmt.Errorf("invalid numVerts: %d in face group %d", numVerts, fi)
		}

		fg := FaceGroup{
			MaterialIndex: matIdx,
			Vertices:      make([]Vertex, 0, numVerts),
		}

		for i := 0; i < int(numVerts); i++ {
			// Read coords
			var x, y, z, u, v int16
			binary.Read(br, order, &x)
			binary.Read(br, order, &y)
			binary.Read(br, order, &z)
			binary.Read(br, order, &u)
			binary.Read(br, order, &v)

			// Read normal
			var nx, ny, nz int8
			binary.Read(br, order, &nx)
			binary.Read(br, order, &ny)
			binary.Read(br, order, &nz)

			// Read next 4 bytes (Color or Joint Indices)
			var b1, b2, b3, b4 uint8
			binary.Read(br, order, &b1)
			binary.Read(br, order, &b2)
			binary.Read(br, order, &b3)
			binary.Read(br, order, &b4)

			vgroup := int16(0)
			color := [4]float32{1, 1, 1, 1}
			joints := [4]uint8{0, 0, 0, 0}
			weights := [4]uint8{0, 0, 0, 0}

			if pbType == 5 {
				// Skinned mesh: b1-b4 are joints, next 4 bytes are weights
				joints = [4]uint8{b1, b2, b3, b4}
				binary.Read(br, order, &weights)
			} else {
				// Static mesh: b1-b4 are color (RGBA)
				// PS2 GS vertex colors: 0-128 per channel where 128 = full intensity.
				clamp1 := func(v float32) float32 {
					if v > 1.0 {
						return 1.0
					}
					return v
				}
				color = [4]float32{
					clamp1(float32(b1) / 128.0),
					clamp1(float32(b2) / 128.0),
					clamp1(float32(b3) / 128.0),
					clamp1(float32(b4) / 128.0),
				}
				if pbType == 4 {
					binary.Read(br, order, &vgroup)
				}
			}

			fg.Vertices = append(fg.Vertices, Vertex{
				Pos:     [3]float32{float32(x) * m.PackingPos, float32(y) * m.PackingPos, float32(z) * m.PackingPos},
				UV:      [2]float32{float32(u) * m.PackingUV, float32(v) * m.PackingUV},
				Normal:  [3]float32{float32(nx) / 127.0, float32(ny) / 127.0, float32(nz) / 127.0},
				Color:   color,
				Joints:  joints,
				Weights: weights,
				VGroup:  vgroup,
			})
		}

		stripIndices(&fg)
		m.FaceGroups = append(m.FaceGroups, fg)
	}

	return m, nil
}

// stripIndices expands a triangle strip to a list. Even (i,i+2,i+1) / odd (i,i+1,i+2) makes the glTF
// counter-clockwise front face agree with the stored EQOA vertex normals (Qeynos terrain: 559,538 triangles this
// way, 3,724 the other) and with the client's VU1 strip cull, whose parity flag flips every vertex. The opposite
// order lit the back face of double-sided materials and rendered the ground black. Same as the JS port.
func stripIndices(fg *FaceGroup) {
	for i := 0; i+2 < len(fg.Vertices); i++ {
		if i%2 == 0 {
			fg.Indices = append(fg.Indices, uint32(i), uint32(i+2), uint32(i+1))
		} else {
			fg.Indices = append(fg.Indices, uint32(i), uint32(i+1), uint32(i+2))
		}
	}
}

// parsePrimBufferV0 reads the older float record (ObjectVersion 0, the client's ParsePrimBufferObjV0):
// nmats, numFaceGroups, totalVerts, then per face group numVerts, matIdx and numVerts x
// { x,y,z,u,v f32; nx,ny,nz f32; r,g,b,a u8 } (36 bytes a vertex). Returned as type 0 with unit packing.
func parsePrimBufferV0(data []byte, order binary.ByteOrder) (*Mesh, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("PrimBuffer v0 data too short for header: %d", len(data))
	}
	br := bytes.NewReader(data)
	var nmats, numFaceGroups, totalVerts int32
	binary.Read(br, order, &nmats)
	binary.Read(br, order, &numFaceGroups)
	binary.Read(br, order, &totalVerts)
	if numFaceGroups < 0 || numFaceGroups > 10000 {
		return nil, fmt.Errorf("invalid numFaceGroups: %d", numFaceGroups)
	}
	m := &Mesh{Type: 0, PackingPos: 1, PackingUV: 1, FaceGroups: make([]FaceGroup, 0, numFaceGroups)}
	clamp1 := func(v float32) float32 {
		if v > 1.0 {
			return 1.0
		}
		return v
	}
	for fi := 0; fi < int(numFaceGroups); fi++ {
		var numVerts, matIdx int32
		if err := binary.Read(br, order, &numVerts); err != nil {
			break
		}
		binary.Read(br, order, &matIdx)
		if numVerts < 0 || numVerts > 10000 || int64(numVerts)*36 > int64(br.Len()) {
			return nil, fmt.Errorf("invalid numVerts: %d in face group %d", numVerts, fi)
		}
		fg := FaceGroup{MaterialIndex: matIdx, Vertices: make([]Vertex, 0, numVerts)}
		for i := 0; i < int(numVerts); i++ {
			var f [8]float32
			var c [4]uint8
			binary.Read(br, order, &f)
			binary.Read(br, order, &c)
			fg.Vertices = append(fg.Vertices, Vertex{
				Pos:    [3]float32{f[0], f[1], f[2]},
				UV:     [2]float32{f[3], f[4]},
				Normal: [3]float32{f[5], f[6], f[7]},
				Color:  [4]float32{clamp1(float32(c[0]) / 128.0), clamp1(float32(c[1]) / 128.0), clamp1(float32(c[2]) / 128.0), clamp1(float32(c[3]) / 128.0)},
			})
		}
		stripIndices(&fg)
		m.FaceGroups = append(m.FaceGroups, fg)
	}
	return m, nil
}
