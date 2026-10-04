# TODO: field-test feedback (2026-10-04)

## Round 2 (before pushing)

- [x] **Unauthenticated visits look like nothing's there.** Without a token the app shows a plain
      "404 page not found" like Go/Traefik's own, and the API answers unauthenticated requests the same way
      (instead of 401 JSON). A setup link is the only way in; the paste-a-link setup form goes.
- [ ] **Two more built-in gyms:** ClimbFit Macquarie and ClimbFit St Leonards.
- [ ] **A small colour logo of the gym's brand** (9 Degrees, ClimbFit) to the left of each gym in the list.
- [ ] **An app icon** next to the "Gym Router" title, and the same mark as the favicon / home-screen icon
      (the current one is a placeholder).
- [ ] **"Earlier trips" / "Later trips"** in place of the first/last times above the option list, moving the
      window by 30 minutes.
- [ ] **Start a trip from the map** without closing it first.
- [ ] **Name the destination in the last walk:** "Walk 5 min to 9 Degrees Parramatta", not "to your destination".
- [ ] **A started trip always uses location** (falling back to the timetable if it isn't available).
- [ ] **A change icon** in the time column of a change in the trip description.

## Round 1

Worked in this order (quick fixes first, the walk-timing overhaul last). Assumptions are noted where the
feedback left a choice open; say if any are wrong.

## Trip screen

- [x] **Keep the selected option across live refreshes.** Looking at any option but the first, a refresh
      jumped back to the first. (The "same option" key included the leave time, which shifts with live data.)
- [x] **Door-to-door time in the option list**, to the right of the connection-risk square.
- [x] **Number of stops in the selected option's summary** (stops travelled on all rides; also shown per ride).
- [x] **Start a trip from "Leave at" and "Arrive by"**, not only "Leave now". In-trip re-checks then plan
      from the trip's own leave time until it's close, so a later trip isn't reported as missed.
- [x] **Order of choices:** direction (to the gym / home), home, then when (now / leave at / arrive by),
      then the gym, then the plan.
- [x] **Collapse the gym list once one is chosen**, showing only that gym (tap it to choose another).
- [x] **"Gym Router" title goes back to the start** (clears the choices); drop the "Trips" tab.
- [x] **Settings: no double rule above Homes** (the header's line is enough).

## Map

- [x] **Only the trip's own lines, only the sections ridden.** No faint network lines.
- [x] **Stops along the ridden sections** as small dots in the line colour with a white outline.
- [x] **Vehicles only near your part of each trip:** only the vehicles of the trips in the option, shown
      from about 3 stops before you board until about 3 stops after you get off. Other vehicles on the same
      lines are no longer drawn.
- [x] **Your location shows as soon as the map opens during a trip.** Location is on by default in a trip
      (no "Use my location" tap), and the map draws you without pressing the locate button.

## Walk timing overhaul

- [x] **Time walks from the live trip**, not from home/gym settings: the walk to the first stop, each
      change, and the walk from the last stop.
- [x] **Saved per walk and reused whenever that walk comes up again**, for any stop (not only the stops
      ticked under a home or gym; a timed walk to an unticked stop makes that stop usable too).
- [x] **First recording replaces the default** (street-map estimate) for planning, and its GPS trace is
      drawn on the map from then on.
- [x] **Later recordings replace or average** according to a setting (default: average the last 5). The
      save screen offers the other choice too.
- [x] Assumption: a walk is the same either way (home → stop is timed once and also used for stop → home;
      a change A → B also covers B → A, trace reversed).
- [x] Settings lists the timed walks and changes (with remove); the old per-stop "Time it" button goes.
      Existing timings (stop walks, "Set my time" changes) are carried over.

## Setup

- [x] **Adding many built-in gyms timed out** (5 at once failed; 3 then 2 worked): one long request per
      batch. Now a server-side job the app polls, with a progress percentage, so no request runs long.
      (Splitting per gym would have multiplied the work: reading the timetable is most of the cost.)
