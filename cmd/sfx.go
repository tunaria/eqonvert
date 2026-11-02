package cmd

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/average-bit/eqonvert/pkg/eqoa"
)

// dev sfx: the character sound table. Every CSprite's 0x2910 sound ids,
// decoded to OUT/SFX/0x<id>.wav, and the sprite -> animation -> sounds table as
// OUT/SFX/sfx.json (pkg/eqoa/spritesounds.go). The JavaScript port
// (tunaria tools/eqoa `dev sfx`) writes the same files.
var sfxCmd = &cobra.Command{
	Use:   "sfx <esf_dir|file.esf> [output_dir]",
	Short: "Character sounds per animation -> SFX/<id>.wav + SFX/sfx.json",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := "."
		if len(args) == 2 {
			out = args[1]
		}
		return runSFX(args[0], out)
	},
}

func init() {
	devCmd.AddCommand(sfxCmd)
}

func runSFX(in, outRoot string) error {
	var paths []string
	st, err := os.Stat(in)
	if err != nil {
		return err
	}
	if st.IsDir() {
		filepath.Walk(in, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.EqualFold(filepath.Ext(p), ".esf") {
				paths = append(paths, p)
			}
			return nil
		})
		sort.Strings(paths)
	} else {
		paths = []string{in}
	}
	var srcs []eqoa.SpriteSoundSource
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, objects, _, order, err := eqoa.ParseESF(f)
		if err != nil {
			logf("  %s: %v\n", p, err)
			continue
		}
		srcs = append(srcs, eqoa.SpriteSoundSource{File: filepath.Base(p), R: f, Order: order, Objects: objects})
	}
	got := eqoa.CollectSpriteSounds(srcs)
	for _, e := range got.Errors {
		logf("  %s\n", e)
	}
	wanted := map[uint32]bool{}
	for _, byAnim := range got.Sprites {
		for _, list := range byAnim {
			for _, s := range list {
				wanted[s] = true
			}
		}
	}
	ids := make([]uint32, 0, len(wanted))
	for id := range wanted {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	written := map[uint32]bool{}
	for _, id := range ids {
		ref, ok := got.Sounds[id]
		if !ok {
			continue
		}
		pcm, rate, err := eqoa.DecodeSpriteSound(srcs, ref)
		if err != nil || len(pcm) == 0 {
			continue
		}
		raw := make([]byte, len(pcm)*2)
		for i, s := range pcm {
			binary.LittleEndian.PutUint16(raw[i*2:], uint16(s))
		}
		path := filepath.Join(outRoot, filepath.FromSlash(eqoa.SFXSoundPath(id)))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, buildWAV(raw, int(rate), 1), 0o644); err != nil {
			return err
		}
		written[id] = true
	}
	man := eqoa.BuildSFXManifest(got, written)
	b, err := json.MarshalIndent(man, "", " ")
	if err != nil {
		return err
	}
	mpath := filepath.Join(outRoot, eqoa.SFXDir, eqoa.SFXManifestName)
	if err := os.MkdirAll(filepath.Dir(mpath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(mpath, b, 0o644); err != nil {
		return err
	}
	missing := ""
	if len(got.Missing) > 0 {
		missing = fmt.Sprintf("; %d sound id(s) not found", len(got.Missing))
	}
	logf("SFX: %d sprite(s), %d sound(s) -> %s%s\n", len(man.Sprites), len(written), mpath, missing)
	return nil
}
