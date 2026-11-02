package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Disc zone map against a server zone grid.
//
// Every 0x3000 zone on the disc is one map tile of zoneTile units square. A
// server zone grid CSV (columns world, server_zone, cell, sub, grid_x, grid_z,
// rows_inside, ...) gives, per server cell, the tile (grid_x, grid_z) =
// (floor(x / zoneTile), floor(z / zoneTile)) it lies on and how many spawn rows
// it holds.
//
// The check passes when every disc zone with placements fits inside one tile
// and every server cell that holds a spawn lies on a disc zone's tile. Server
// cells holding no spawn and disc tiles without a server cell are listed, not
// failed: the server only has cells where its spawn data reaches.
//
// Region file -> server world: TUNARIA 0, RATHE 1, ODUS 2, LAVASTM 3,
// PLANESKY 4, SECRETS 5. Other regions (ARENA) are left out.

const zoneTile = 2000

var regionWorlds = map[string]int{"TUNARIA": 0, "RATHE": 1, "ODUS": 2, "LAVASTM": 3, "PLANESKY": 4, "SECRETS": 5}

var (
	zoneGridCSV string
	zoneGridOut string
)

// discTile is one disc zone with placements and its tile.
type discTile struct {
	World     int    `json:"world"`
	Region    string `json:"region"`
	ZoneIndex int    `json:"zone_index"`
	GridX     int    `json:"grid_x"`
	GridZ     int    `json:"grid_z"`
	Fits      bool   `json:"fits"`
}

// serverCell is one row of the server zone grid.
type serverCell struct {
	World      int `json:"world"`
	ServerZone int `json:"server_zone"`
	Cell       int `json:"cell"`
	Sub        int `json:"sub"`
	GridX      int `json:"grid_x"`
	GridZ      int `json:"grid_z"`
	RowsInside int `json:"rows_inside"`
}

// discOnlyTile is a disc tile with no server cell.
type discOnlyTile struct {
	World     int    `json:"world"`
	GridX     int    `json:"grid_x"`
	GridZ     int    `json:"grid_z"`
	Region    string `json:"region"`
	ZoneIndex int    `json:"zone_index"`
}

type zoneGridResult struct {
	OK               bool           `json:"ok"`
	DiscTiles        int            `json:"discTiles"`
	ServerTiles      int            `json:"serverTiles"`
	MatchedTiles     int            `json:"matchedTiles"`
	Overflowing      []discTile     `json:"overflowing"`
	SpawnCellsOffMap []serverCell   `json:"spawnCellsOffMap"`
	EmptyCellsOffMap []serverCell   `json:"emptyCellsOffMap"`
	DiscOnly         []discOnlyTile `json:"discOnly"`
}

var zoneGridCmd = &cobra.Command{
	Use:   "zone-grid <path>",
	Short: "Compare the disc zone map with a server zone grid CSV",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if zoneGridCSV == "" {
			return fmt.Errorf("--grid is required")
		}
		regions, err := collectRegions(args[0])
		if err != nil {
			return err
		}
		if len(regions) == 0 {
			return fmt.Errorf("no zone data found in %s", args[0])
		}
		grid, err := readServerZoneGrid(zoneGridCSV)
		if err != nil {
			return err
		}
		res := compareZoneGrid(discZoneTiles(regions, 50), grid)
		js, err := json.MarshalIndent(res, "", " ")
		if err != nil {
			return err
		}
		js = append(js, '\n')
		if zoneGridOut == "" || zoneGridOut == "-" {
			_, err = os.Stdout.Write(js)
		} else {
			err = os.WriteFile(zoneGridOut, js, 0o644)
		}
		if err != nil {
			return err
		}
		verdict := "match"
		if !res.OK {
			verdict = "MISMATCH"
		}
		logf("zone grid: %d/%d disc tiles have server cells; %d zone(s) overflow a tile; %d spawn cell(s) and %d empty cell(s) off the disc map: %s\n",
			res.MatchedTiles, res.DiscTiles, len(res.Overflowing), len(res.SpawnCellsOffMap), len(res.EmptyCellsOffMap), verdict)
		if !res.OK {
			return fmt.Errorf("zone grid mismatch")
		}
		return nil
	},
}

func init() {
	zoneGridCmd.Flags().StringVar(&zoneGridCSV, "grid", "", "server zone grid CSV (server_zone_grid.csv)")
	zoneGridCmd.Flags().StringVarP(&zoneGridOut, "output", "o", "", "output JSON path (default stdout)")
	devCmd.AddCommand(zoneGridCmd)
}

// discZoneTiles puts each disc zone with placements on its tile (from the
// centre of its placement bounds) and records whether the bounds stay inside
// that tile within slack units.
func discZoneTiles(regions []regionRow, slack float64) []discTile {
	var out []discTile
	for _, reg := range regions {
		name := strings.ToUpper(reg.name)
		w, ok := regionWorlds[name]
		if !ok {
			continue
		}
		for _, z := range reg.zones {
			if len(z.placements) == 0 {
				continue
			}
			var mn, mx [3]float32
			have := false
			for _, p := range z.placements {
				accum(&mn, &mx, &have, [3]float32{p.x, p.y, p.z})
			}
			cx, _, cz := center(mn, mx)
			gx := int(math.Floor(float64(cx) / zoneTile))
			gz := int(math.Floor(float64(cz) / zoneTile))
			x0, z0 := float64(gx*zoneTile), float64(gz*zoneTile)
			fits := float64(mn[0]) >= x0-slack && float64(mx[0]) <= x0+zoneTile+slack &&
				float64(mn[2]) >= z0-slack && float64(mx[2]) <= z0+zoneTile+slack
			out = append(out, discTile{World: w, Region: name, ZoneIndex: z.index, GridX: gx, GridZ: gz, Fits: fits})
		}
	}
	return out
}

// readServerZoneGrid reads the grid CSV by header name.
func readServerZoneGrid(path string) ([]serverCell, error) {
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
		col[strings.TrimSpace(h)] = i
	}
	get := func(rec []string, name string) int {
		if i, ok := col[name]; ok && i < len(rec) {
			v, _ := strconv.Atoi(strings.TrimSpace(rec[i]))
			return v
		}
		return 0
	}
	var out []serverCell
	for _, rec := range recs[1:] {
		out = append(out, serverCell{
			World: get(rec, "world"), ServerZone: get(rec, "server_zone"), Cell: get(rec, "cell"), Sub: get(rec, "sub"),
			GridX: get(rec, "grid_x"), GridZ: get(rec, "grid_z"), RowsInside: get(rec, "rows_inside"),
		})
	}
	return out, nil
}

func compareZoneGrid(tiles []discTile, grid []serverCell) zoneGridResult {
	key := func(w, x, z int) string { return fmt.Sprintf("%d,%d,%d", w, x, z) }
	disc := map[string]discTile{}
	var discOrder []string
	for _, t := range tiles {
		k := key(t.World, t.GridX, t.GridZ)
		if _, ok := disc[k]; !ok {
			disc[k] = t
			discOrder = append(discOrder, k)
		}
	}
	server := map[string]bool{}
	for _, c := range grid {
		server[key(c.World, c.GridX, c.GridZ)] = true
	}
	res := zoneGridResult{Overflowing: []discTile{}, SpawnCellsOffMap: []serverCell{}, EmptyCellsOffMap: []serverCell{}, DiscOnly: []discOnlyTile{}}
	for _, c := range grid {
		if _, ok := disc[key(c.World, c.GridX, c.GridZ)]; ok {
			continue
		}
		if c.RowsInside > 0 {
			res.SpawnCellsOffMap = append(res.SpawnCellsOffMap, c)
		} else {
			res.EmptyCellsOffMap = append(res.EmptyCellsOffMap, c)
		}
	}
	for _, k := range discOrder {
		if server[k] {
			res.MatchedTiles++
		} else {
			t := disc[k]
			res.DiscOnly = append(res.DiscOnly, discOnlyTile{World: t.World, GridX: t.GridX, GridZ: t.GridZ, Region: t.Region, ZoneIndex: t.ZoneIndex})
		}
	}
	for _, t := range tiles {
		if !t.Fits {
			res.Overflowing = append(res.Overflowing, t)
		}
	}
	res.DiscTiles = len(disc)
	res.ServerTiles = len(server)
	res.OK = len(res.Overflowing) == 0 && len(res.SpawnCellsOffMap) == 0
	return res
}
