// Standalone probe: what do multi-layer materials actually contain?
//
// The corrected material header (see pkg/eqoa/material.go) means second layers now parse, and
// the textures they reference get embedded in the GLB -- but pkg/gltf/export.go only ever reads
// Layers[0], so that artwork ships inside the file and never reaches a material. On character
// models those second layers are full garment sheets: robes with gold trim, patterned fabric.
// On the ARENA zone they are solid black, i.e. empty slots.
//
// Whether the composite can be expressed in glTF at all depends on how layer 1 combines with
// layer 0, which is the BlendMode field -- parsed but read by nothing. This dumps the actual
// distribution so that question can be answered from data instead of assumed.
//
//	go run ./cmd/probe_layers <file.esf|file.csf> [more...]
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/average-bit/eqonvert/pkg/eqoa"
)

type key struct {
	layers int
	idx    int
	blend  int32
	wrap   int32
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe_layers <file.esf|file.csf> [...]")
		os.Exit(2)
	}

	counts := map[key]int{}
	layerHist := map[int]int{}
	// Colour by layer index: the skin modulate lives on layer 0, and it is worth seeing whether
	// layer 1 carries one too.
	colByLayer := map[int]map[[4]float32]int{}
	totalMats := 0

	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			continue
		}
		var r io.ReadSeeker
		if len(data) >= 4 && string(data[:4]) == "CESF" {
			dr, _, err := eqoa.DecompressCSF(bytes.NewReader(data))
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: decompress: %v\n", path, err)
				continue
			}
			all, _ := io.ReadAll(dr)
			r = bytes.NewReader(all)
		} else {
			r = bytes.NewReader(data)
		}

		_, objects, _, order, err := eqoa.ParseESF(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: parse: %v\n", path, err)
			continue
		}

		var walk func(objs []*eqoa.ESFObject)
		walk = func(objs []*eqoa.ESFObject) {
			for _, o := range objs {
				if o.Header.ObjectType == 0x1101 { // MaterialArray
					for _, mo := range o.Children {
						body, err := mo.ReadBody(r)
						if err != nil {
							continue
						}
						m, err := eqoa.ParseMaterialBody(body, mo.Header.ObjectVersion, order)
						if err != nil || m == nil {
							continue
						}
						totalMats++
						layerHist[len(m.Layers)]++
						for i, l := range m.Layers {
							counts[key{len(m.Layers), i, l.BlendMode, l.WrapMode}]++
							if colByLayer[i] == nil {
								colByLayer[i] = map[[4]float32]int{}
							}
							colByLayer[i][l.Color]++
						}
					}
				}
				walk(o.Children)
			}
		}
		walk(objects)
	}

	fmt.Printf("%d materials\n\nlayers per material:\n", totalMats)
	var ls []int
	for l := range layerHist {
		ls = append(ls, l)
	}
	sort.Ints(ls)
	for _, l := range ls {
		fmt.Printf("  %d layer(s): %6d materials\n", l, layerHist[l])
	}

	fmt.Println("\nblend / wrap mode by layer position:")
	var ks []key
	for k := range counts {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(a, b int) bool {
		if ks[a].idx != ks[b].idx {
			return ks[a].idx < ks[b].idx
		}
		return counts[ks[a]] > counts[ks[b]]
	})
	for _, k := range ks {
		fmt.Printf("  layer %d of %d:  blend=%-4d wrap=%-4d  x%d\n",
			k.idx, k.layers, k.blend, k.wrap, counts[k])
	}

	fmt.Println("\nmost common layer colours (RGBA 0-255):")
	for _, i := range []int{0, 1} {
		m := colByLayer[i]
		if m == nil {
			continue
		}
		type cv struct {
			c [4]float32
			n int
		}
		var vs []cv
		for c, n := range m {
			vs = append(vs, cv{c, n})
		}
		sort.Slice(vs, func(a, b int) bool { return vs[a].n > vs[b].n })
		fmt.Printf("  layer %d:\n", i)
		for j, v := range vs {
			if j >= 6 {
				break
			}
			fmt.Printf("     (%3.0f,%3.0f,%3.0f,%3.0f)  x%d\n",
				v.c[0]*255, v.c[1]*255, v.c[2]*255, v.c[3]*255, v.n)
		}
	}
}
