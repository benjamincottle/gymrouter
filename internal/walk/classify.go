package walk

// Penalties multiply a way's length to get its walking cost in "effective metres". Steps are slow.
const (
	costNormal = 1.0
	costSteps  = 1.6
)

// classify says whether a pedestrian can use a way, and how costly it is to walk, from its OSM tags.
// The rules follow OSM's usual conventions: roads are walkable unless tagged otherwise; private or
// no-foot ways are not; motorways and areas are skipped.
func classify(tags map[string]string) (cost float32, ok bool) {
	hw := tags["highway"]
	if hw == "" {
		// Station platforms and a few other railway ways carry pedestrians too.
		if tags["railway"] == "platform" {
			hw = "platform"
		} else {
			return 0, false
		}
	}
	switch hw {
	case "motorway", "motorway_link", "construction", "proposed", "raceway", "bus_guideway", "busway",
		"bridleway", "escape", "elevator", "services", "rest_area", "via_ferrata":
		return 0, false
	case "cycleway":
		// Shared paths are tagged foot=yes/designated; plain cycleways are for bikes.
		if f := tags["foot"]; f != "yes" && f != "designated" && f != "permissive" {
			return 0, false
		}
	}
	if tags["area"] == "yes" {
		return 0, false
	}
	foot := tags["foot"]
	if foot == "no" || foot == "private" {
		return 0, false
	}
	if foot != "yes" && foot != "designated" && foot != "permissive" {
		switch tags["access"] {
		case "no", "private":
			return 0, false
		}
	}
	if hw == "steps" {
		return costSteps, true
	}
	return costNormal, true
}
