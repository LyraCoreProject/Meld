package vmangos

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"Meld/internal/lyracore"
	"Meld/internal/stdb"
)

type lyraCreatureDisplay struct {
	DisplayID      uint32  `json:"display_id"`
	Scale          float32 `json:"scale"`
	Weight         uint32  `json:"weight"`
	Gender         uint8   `json:"gender"`
	BoundingRadius float32 `json:"bounding_radius"`
}
type lyraCreatureDisplays struct {
	Entry    uint32                `json:"entry"`
	Displays []lyraCreatureDisplay `json:"displays"`
}

func readLyraCreatureDisplays(realm *lyracore.Realm, entry int) ([]lyraCreatureDisplay, error) {
	t, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT * FROM game_creature_display WHERE entry = %d", entry))
	if err != nil {
		return nil, err
	}
	type row struct {
		id      uint64
		display lyraCreatureDisplay
	}
	rows := []row{}
	err = t.Each(func(r *stdb.Row) error {
		rows = append(rows, row{r.Uint("id"), lyraCreatureDisplay{uint32(r.Uint("display_id")), float32(r.Float("scale")), uint32(r.Uint("weight")), uint8(r.Uint("gender")), float32(r.Float("bounding_radius"))}})
		return nil
	})
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	out := make([]lyraCreatureDisplay, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.display)
	}
	return out, err
}

func effectiveLyraDisplays(f lyraCreatureFields, rows []lyraCreatureDisplay) []lyraCreatureDisplay {
	if len(rows) > 0 && rows[0].DisplayID == f.DisplayID && rows[0].Scale == f.Scale {
		return rows
	}
	return []lyraCreatureDisplay{{DisplayID: f.DisplayID, Scale: f.Scale, Weight: 1, BoundingRadius: 0.389}}
}

func isLyraDisplayColumn(name string) bool {
	if name == "display_total_probability" {
		return true
	}
	for i := 1; i <= 4; i++ {
		for _, prefix := range []string{"display_id", "display_scale", "display_probability"} {
			if name == fmt.Sprintf("%s%d", prefix, i) {
				return true
			}
		}
	}
	return false
}

func addLyraDisplayColumns(r *TemplateRecord, displays []lyraCreatureDisplay) {
	var total uint64
	for _, d := range displays {
		total += uint64(d.Weight)
	}
	r.Values["display_total_probability"] = strconv.FormatUint(total, 10)
	if len(displays) <= 4 {
		r.Columns = append(r.Columns, TemplateColumn{Name: "display_total_probability", Kind: "uint", Max: math.MaxUint32})
	}
	for i := 1; i <= 4; i++ {
		d := lyraCreatureDisplay{Scale: 1}
		if i <= len(displays) {
			d = displays[i-1]
		}
		for _, v := range []struct{ name, value, kind string }{{"display_id", strconv.FormatUint(uint64(d.DisplayID), 10), "uint"}, {"display_scale", strconv.FormatFloat(float64(d.Scale), 'f', -1, 32), "float"}, {"display_probability", strconv.FormatUint(uint64(d.Weight), 10), "uint"}} {
			name := fmt.Sprintf("%s%d", v.name, i)
			r.Values[name] = v.value
			if len(displays) <= 4 {
				r.Columns = append(r.Columns, TemplateColumn{Name: name, Kind: v.kind, Max: math.MaxUint32})
			}
		}
	}
}

func lyraDisplayChanges(realm *lyracore.Realm, dataDir string, entry int, f lyraCreatureFields, stored []lyraCreatureDisplay, changes map[string]string) (*lyraCreatureDisplays, error) {
	current := effectiveLyraDisplays(f, stored)
	if len(current) > 4 {
		return nil, refuse("display_id1", "this creature has more than four displays; Meld cannot replace that list")
	}
	r := TemplateRecord{Values: map[string]string{}}
	addLyraDisplayColumns(&r, current)
	changed := false
	for k, v := range changes {
		if r.Values[k] != v {
			changed = true
			r.Values[k] = v
		}
	}
	if !changed {
		return nil, nil
	}
	total, err := strconv.ParseUint(r.Values["display_total_probability"], 10, 32)
	if err != nil {
		return nil, refuse("display_total_probability", "%s", err)
	}
	out := &lyraCreatureDisplays{Entry: uint32(entry), Displays: []lyraCreatureDisplay{}}
	contiguous := true
	var sum uint64
	for i := 1; i <= 4; i++ {
		idCol, scaleCol, weightCol := fmt.Sprintf("display_id%d", i), fmt.Sprintf("display_scale%d", i), fmt.Sprintf("display_probability%d", i)
		id, err := strconv.ParseUint(r.Values[idCol], 10, 32)
		if err != nil {
			return nil, refuse(idCol, "%s", err)
		}
		scale, err := strconv.ParseFloat(r.Values[scaleCol], 32)
		if err != nil || math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0 {
			return nil, refuse(scaleCol, "scale must be finite and nonnegative")
		}
		weight, err := strconv.ParseUint(r.Values[weightCol], 10, 32)
		if err != nil {
			return nil, refuse(weightCol, "%s", err)
		}
		if id == 0 {
			contiguous = false
			continue
		}
		if total == 0 {
			weight = 0
			if contiguous {
				weight = 1
			}
		}
		d := lyraCreatureDisplay{DisplayID: uint32(id), Scale: float32(scale), Weight: uint32(weight), BoundingRadius: 0.389}
		metadata, err := realm.DB.Query(realm.Database, fmt.Sprintf("SELECT gender, bounding_radius FROM game_creature_display WHERE display_id = %d LIMIT 1", id))
		if err != nil {
			return nil, err
		}
		if err := metadata.Each(func(r *stdb.Row) error {
			d.Gender = uint8(r.Uint("gender"))
			d.BoundingRadius = float32(r.Float("bounding_radius"))
			return nil
		}); err != nil {
			return nil, err
		}
		if d.Scale == 0 {
			d.Scale = 1
			if dataDir != "" {
				tables, err := cachedModelTables(dataDir)
				if err != nil {
					return nil, err
				}
				if s := tables.creatureLook(d.DisplayID, gear{}).scale; s > 0 {
					d.Scale = s
				}
			}
		}
		sum += weight
		out.Displays = append(out.Displays, d)
	}
	if len(out.Displays) == 0 {
		return nil, refuse("display_id1", "keep at least one display for the template fallback")
	}
	if sum == 0 || sum > math.MaxUint32 {
		return nil, refuse("display_total_probability", "display weights must total 1 through 4294967295")
	}
	if total != 0 && sum != total {
		return nil, refuse("display_total_probability", "the total must equal the display weights, or be zero for equal weights")
	}
	return out, nil
}
