# TODO: field-test feedback (2026-10-04)

Worked in this order (quick fixes first, the walk-timing overhaul last). Assumptions are noted where the
feedback left a choice open; say if any are wrong.

## Trip screen

- [x] **Keep the selected option across live refreshes.** Looking at any option but the first, a refresh
      jumped back to the first. (The "same option" key included the leave time, which shifts with live data.)
- [x] **Door-to-door time in the option list**, to the right of the connection-risk square.
- [ ] **Number of stops in the selected option's summary** (stops travelled on all rides; also shown per ride).
- [x] **Start a trip from "Leave at" and "Arrive by"**, not only "Leave now". In-trip re-checks then plan
      from the trip's own leave time until it's close, so a later trip isn't reported as missed.
- [x] **Order of choices:** direction (to the gym / home), home, then when (now / leave at / arrive by),
      then the gym, then the plan.
- [x] **Collapse the gym list once one is chosen**, showing only that gym (tap it to choose another).
- [x] **"Gym Router" title goes back to the start** (clears the choices); drop the "Trips" tab.
- [x] **Settings: no double rule above Homes** (the header's line is enough).

## Map

- [ ] **Only the trip's own lines, only the sections ridden.** No faint network lines.
- [ ] **Stops along the ridden sections** as small dots in the line colour with a white outline.
- [ ] **Vehicles only near your part of each trip:** only the vehicles of the trips in the option, shown
      from about 3 stops before you board until about 3 stops after you get off. Other vehicles on the same
      lines are no longer drawn.
- [ ] **Your location shows as soon as the map opens during a trip.** Location is on by default in a trip
      (no "Use my location" tap), and the map draws you without pressing the locate button.

## Walk timing overhaul

- [ ] **Time walks from the live trip**, not from home/gym settings: the walk to the first stop, each
      change, and the walk from the last stop.
- [ ] **Saved per walk and reused whenever that walk comes up again**, for any stop (not only the stops
      ticked under a home or gym; a timed walk to an unticked stop makes that stop usable too).
- [ ] **First recording replaces the default** (street-map estimate) for planning, and its GPS trace is
      drawn on the map from then on.
- [ ] **Later recordings replace or average** according to a setting (default: average the last 5). The
      save screen offers the other choice too.
- [ ] Assumption: a walk is the same either way (home → stop is timed once and also used for stop → home;
      a change A → B also covers B → A, trace reversed).
- [ ] Settings lists the timed walks and changes (with remove); the old per-stop "Time it" button goes.
      Existing timings (stop walks, "Set my time" changes) are carried over.

## Setup

- [ ] **Adding many built-in gyms timed out** (5 at once failed; 3 then 2 worked): one long request per
      batch. Look up lines one gym per request with progress ("2 of 5"), so no request runs long and a
      failure for one gym doesn't lose the others.
