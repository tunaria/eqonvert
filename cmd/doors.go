package cmd

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/average-bit/eqonvert/pkg/eqoa"
	"github.com/spf13/cobra"
)

// Doors, portals and other travel spots.
//
// The zone files hold no door, portal or trigger record: no object type in
// TUNARIA.ESF, ARENA.ESF or ZONE0107.ESF carries a travel target. What the disc
// does hold is the door or portal mesh itself: an ordinary zone actor (0x6000)
// of a static SimpleSprite (0x2000), standing within a few units of the
// server's travel spawn. The server's door spawns have model 0, so the server
// spawn is an invisible trigger and the disc mesh is scenery.
//
// The doors command reads the zone actors and sprite kinds from one ESF/CSF and
// matches them to travel spawn rows from a CSV the user supplies (columns
// spawn_id, name, world, x, y, z, in server coordinates, which are the zone
// actor coordinates). It reads no body except the 36-byte actor bodies and the
// 4-byte sprite header ids.

var (
	doorsSpawns string
	doorsOut    string
	doorsRadius float64
	doorsHeight float64
)

// spriteHeaders maps a sprite object type to its header child type; the header
// body starts with the sprite id.
var spriteHeaders = map[uint16]uint16{0x2000: 0x2001, 0x2200: 0x2210, 0x2C00: 0x2C10, 0x2700: 0x2710, 0x2310: 0x2311}

// spriteKindName names each sprite object type.
var spriteKindName = map[uint16]string{0x2000: "static", 0x2200: "animated", 0x2C00: "group", 0x2700: "character", 0x2310: "subsprite"}

// spriteKind is one sprite's object type and whether it has a 0x4200 CollBuffer child.
type spriteKind struct {
	Type      uint16
	Collision bool
}

// zoneActor is one 0x6000 placement. Zone is the 0x3000 index in file order and
// Actor the actor's index within that zone in preorder, the order extractRegion
// and the converter place them in.
type zoneActor struct {
	Zone  int     `json:"zone"`
	Actor int     `json:"actor"`
	ID    uint32  `json:"id"`
	Hex   string  `json:"hex,omitempty"`
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	Z     float32 `json:"z"`
	Yaw   float32 `json:"yaw"`
	Pitch float32 `json:"pitch"`
	Roll  float32 `json:"roll"`
	Scale float32 `json:"scale"`
}

// travelRow is one travel spawn from the CSV.
type travelRow struct {
	SpawnID string
	Name    string
	World   int
	X, Y, Z float64
	HasXZ   bool
	HasY    bool
}

// doorMesh is the actor matched to a travel spawn.
type doorMesh struct {
	zoneActor
	Kind      string  `json:"kind"`
	Collision bool    `json:"collision"`
	Distance  float64 `json:"distance"`
}

// doorMatch is one output row; Mesh is nil when no static actor is close enough.
type doorMatch struct {
	SpawnID string    `json:"spawnId"`
	Name    string    `json:"name"`
	World   int       `json:"world"`
	Mesh    *doorMesh `json:"mesh"`
}

var doorsCmd = &cobra.Command{
	Use:   "doors <zone.ESF|zone.CSF>",
	Short: "Match travel spawns (door/portal triggers) to the static zone actors that are their meshes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if doorsSpawns == "" {
			return fmt.Errorf("--spawns is required")
		}
		rows, err := readTravelRows(doorsSpawns)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		var r io.ReadSeeker
		if len(data) >= 4 && string(data[:4]) == eqoa.MagicCESF {
			dr, _, derr := eqoa.DecompressCSF(bytes.NewReader(data))
			if derr != nil {
				return derr
			}
			all, rerr := io.ReadAll(dr)
			if rerr != nil {
				return rerr
			}
			r = bytes.NewReader(all)
		} else {
			r = bytes.NewReader(data)
		}
		_, objects, _, order, err := eqoa.ParseESF(r)
		if err != nil {
			return err
		}
		kinds := readSpriteKinds(objects, r, order)
		actors := readZoneActors(objects, r, order)
		out := matchTravelActors(rows, actors, kinds, doorsRadius, doorsHeight)
		js, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		js = append(js, '\n')
		if doorsOut == "" || doorsOut == "-" {
			_, err = os.Stdout.Write(js)
			return err
		}
		return os.WriteFile(doorsOut, js, 0o644)
	},
}

func init() {
	doorsCmd.Flags().StringVar(&doorsSpawns, "spawns", "", "CSV of travel spawns (spawn_id,name,world,x,y,z)")
	doorsCmd.Flags().StringVarP(&doorsOut, "output", "o", "", "output JSON path (default stdout)")
	doorsCmd.Flags().Float64Var(&doorsRadius, "radius", 3, "max ground-plane distance from spawn to actor")
	doorsCmd.Flags().Float64Var(&doorsHeight, "height", 6, "max vertical distance when the spawn has a y")
	rootCmd.AddCommand(doorsCmd)
}

// readSpriteKinds returns sprite id -> kind for every sprite in the tree. The
// first sprite with an id wins, as in the converter's sprite library.
func readSpriteKinds(objects []*eqoa.ESFObject, r io.ReadSeeker, order binary.ByteOrder) map[uint32]spriteKind {
	out := map[uint32]spriteKind{}
	var walk func(o *eqoa.ESFObject)
	walk = func(o *eqoa.ESFObject) {
		t := uint16(o.Header.ObjectType)
		if h, ok := spriteHeaders[t]; ok {
			var id uint32
			coll := false
			for _, c := range o.Children {
				ct := uint16(c.Header.ObjectType)
				if ct == h && id == 0 {
					if b, err := c.ReadBody(r); err == nil && len(b) >= 4 {
						id = order.Uint32(b[0:4])
					}
				}
				if ct == 0x4200 {
					coll = true
				}
			}
			if id != 0 {
				if _, seen := out[id]; !seen {
					out[id] = spriteKind{Type: t, Collision: coll}
				}
			}
		}
		for _, c := range o.Children {
			walk(c)
		}
	}
	for _, o := range objects {
		walk(o)
	}
	return out
}

// readZoneActors returns every 0x6000 actor under a 0x3000 zone, in preorder.
func readZoneActors(objects []*eqoa.ESFObject, r io.ReadSeeker, order binary.ByteOrder) []zoneActor {
	var out []zoneActor
	f32 := func(b []byte) float32 { return math.Float32frombits(order.Uint32(b)) }
	zone := -1
	perZone := map[int]int{}
	var walk func(o *eqoa.ESFObject, z int)
	walk = func(o *eqoa.ESFObject, z int) {
		t := uint16(o.Header.ObjectType)
		if t == 0x3000 {
			zone++
			z = zone
		}
		if t == 0x6000 && z >= 0 {
			if b, err := o.ReadBody(r); err == nil && len(b) >= 36 {
				n := perZone[z]
				perZone[z] = n + 1
				out = append(out, zoneActor{
					Zone: z, Actor: n, ID: order.Uint32(b[0:4]),
					X: f32(b[4:8]), Y: f32(b[8:12]), Z: f32(b[12:16]),
					Yaw: f32(b[16:20]), Pitch: f32(b[20:24]), Roll: f32(b[24:28]), Scale: f32(b[28:32]),
				})
			}
		}
		for _, c := range o.Children {
			walk(c, z)
		}
	}
	for _, o := range objects {
		walk(o, -1)
	}
	return out
}

// matchTravelActors gives each travel spawn the nearest actor of a static
// sprite within radius units on the ground plane (and within height units up or
// down when the row has a y).
func matchTravelActors(rows []travelRow, actors []zoneActor, kinds map[uint32]spriteKind, radius, height float64) []doorMatch {
	out := make([]doorMatch, 0, len(rows))
	for _, row := range rows {
		m := doorMatch{SpawnID: row.SpawnID, Name: row.Name, World: row.World}
		bestD := math.Inf(1)
		if row.HasXZ {
			for _, a := range actors {
				k, ok := kinds[a.ID]
				if !ok || k.Type != 0x2000 {
					continue
				}
				d := math.Hypot(float64(a.X)-row.X, float64(a.Z)-row.Z)
				if d > radius {
					continue
				}
				dy := float64(a.Y) - row.Y
				if row.HasY && math.Abs(dy) > height {
					continue
				}
				d3 := d
				if row.HasY {
					d3 = math.Hypot(d, dy)
				}
				if d3 < bestD {
					bestD = d3
					a.Hex = fmt.Sprintf("0x%08X", a.ID)
					m.Mesh = &doorMesh{zoneActor: a, Kind: spriteKindName[k.Type], Collision: k.Collision, Distance: math.Round(d3*100) / 100}
				}
			}
		}
		out = append(out, m)
	}
	return out
}

// readTravelRows reads the travel spawn CSV by header name.
func readTravelRows(path string) ([]travelRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	recs, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, nil
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	num := func(s string) (float64, bool) {
		if s == "" {
			return 0, false
		}
		v, err := strconv.ParseFloat(s, 64)
		return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	var rows []travelRow
	for _, rec := range recs[1:] {
		row := travelRow{SpawnID: get(rec, "spawn_id"), Name: get(rec, "name")}
		row.World, _ = strconv.Atoi(get(rec, "world"))
		x, okx := num(get(rec, "x"))
		z, okz := num(get(rec, "z"))
		y, oky := num(get(rec, "y"))
		row.X, row.Y, row.Z = x, y, z
		row.HasXZ = okx && okz
		row.HasY = oky
		rows = append(rows, row)
	}
	return rows, nil
}
