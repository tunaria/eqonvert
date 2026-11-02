package cmd

import "testing"

func gridZone(index int, x0, z0, w float32, n int) zoneRow {
	z := zoneRow{index: index}
	for i := 0; i < n; i++ {
		f := float32(i) / float32(max(n-1, 1))
		z.placements = append(z.placements, placementRow{x: x0 + w*f, z: z0 + w*f, scale: 1})
	}
	return z
}

func TestZoneGridMatch(t *testing.T) {
	regions := []regionRow{
		{name: "TUNARIA", zones: []zoneRow{gridZone(0, 4050, 4050, 1900, 3), gridZone(1, 6050, 4050, 1900, 3), gridZone(2, 0, 0, 1900, 0)}},
		{name: "ARENA", zones: []zoneRow{gridZone(0, -18, -29, 40, 3)}},
		{name: "odus", zones: []zoneRow{gridZone(0, 2050, 2050, 1900, 3)}},
	}
	tiles := discZoneTiles(regions, 50)
	if len(tiles) != 3 || tiles[0].GridX != 2 || tiles[1].GridX != 3 || tiles[2].World != 2 || tiles[2].GridX != 1 {
		t.Fatalf("tiles = %+v", tiles)
	}
	grid := []serverCell{
		{World: 0, ServerZone: 0, GridX: 2, GridZ: 2, RowsInside: 16},
		{World: 0, ServerZone: 1, Sub: 1, GridX: 2, GridZ: 2, RowsInside: 5},
		{World: 0, ServerZone: 9, Cell: 4, GridX: 8, GridZ: 8},
	}
	r := compareZoneGrid(tiles, grid)
	if !r.OK || r.DiscTiles != 3 || r.ServerTiles != 2 || r.MatchedTiles != 1 || len(r.EmptyCellsOffMap) != 1 || len(r.DiscOnly) != 2 {
		t.Fatalf("result = %+v", r)
	}
	off := compareZoneGrid(tiles, append(grid, serverCell{World: 1, ServerZone: 4, GridX: 5, GridZ: 5, RowsInside: 2}))
	if off.OK || len(off.SpawnCellsOffMap) != 1 {
		t.Fatalf("spawn cell off map not failed: %+v", off)
	}
	wide := discZoneTiles([]regionRow{{name: "TUNARIA", zones: []zoneRow{gridZone(0, 3800, 4050, 2400, 3)}}}, 50)
	if compareZoneGrid(wide, grid).OK {
		t.Fatal("zone wider than a tile not failed")
	}
}

func TestTravelTriggersByZone(t *testing.T) {
	kinds := map[uint32]spriteKind{0xAAAA0001: {Type: 0x2000, Collision: true}, 0xAAAA0002: {Type: 0x2000}}
	actors := []zoneActor{{Zone: 3, Actor: 0, ID: 0xAAAA0001, X: 100, Z: 100}, {Zone: 1, Actor: 0, ID: 0xAAAA0002, X: 200, Z: 200}, {Zone: 3, Actor: 1, ID: 0xAAAA0002, X: 300, Z: 300}}
	rows := []travelRow{
		{SpawnID: "1", Name: "Door", X: 100.5, Z: 100, HasXZ: true, HasY: true},
		{SpawnID: "2", Name: "Shimmering Portal", X: 200, Z: 201, HasXZ: true},
		{SpawnID: "3", Name: "Door", X: 300, Y: 1, Z: 300, HasXZ: true, HasY: true},
		{SpawnID: "4", Name: "Door", X: 900, Z: 900, HasXZ: true},
		{SpawnID: "5", Name: "Door", Z: 1},
	}
	got := travelTriggersByZone(matchTravelActors(rows, actors, kinds, 3, 6))
	if len(got.Zones) != 2 || got.Zones[0].Zone != 1 || got.Zones[1].Zone != 3 || len(got.Zones[1].Triggers) != 2 {
		t.Fatalf("zones = %+v", got.Zones)
	}
	if tr := got.Zones[1].Triggers[0]; tr.SpawnID != "1" || tr.Hex != "0xAAAA0001" || tr.Distance != 0.5 {
		t.Fatalf("trigger = %+v", tr)
	}
	if len(got.Unmatched) != 2 || got.Unmatched[0].SpawnID != "4" || got.Unmatched[1].SpawnID != "5" {
		t.Fatalf("unmatched = %+v", got.Unmatched)
	}
}
