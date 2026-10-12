# Design: Gym Router web UI

The rules for every screen. When a screen needs something this file doesn't cover, add the rule here first and then
build it. If code and this file disagree, the code is wrong. The decisions behind it are in the spec's decision log
(2026-10-05).

## Principles

- **A chalk wall and graphite ink.** The background is the climbing wall, with a faint grid of bolt holes. Everything
  else is ink.
- **Colour only means something.** Saturated colour is reserved for:
  - transit line colours;
  - connection risk: safe, tight, at risk, missed;
  - the focus ring and the "you" marker, in blue.
- **Times read like a departure board.** Times are set in condensed Archivo numerals.
- **The tape strips are the signature.** Each option is a strip of tape across its row, its rides named in the line
  colours, and the trip's steps hang off a rail in the same colours. Spend boldness there and keep everything around them quiet.
- **Rules, not cards.** Sections start with a heavy ink rule and rows are separated by hairlines. Don't use rounded
  card stacks or drop shadows.
- **One action leads.** A screen has at most one filled button, the action that moves it forward. Everything else is
  quieter: an outline, a row, or underlined text.

## Tokens

All values live as CSS custom properties on `:root`, with dark values under `:root[data-theme='dark']`. Never write a
raw colour, size or spacing value in a component. Use a token, or add one here first.

### Colour

| Token | Light | Dark | Use |
|---|---|---|---|
| `--wall` | `#ebeeea` | `#1b1e21` | page background (with `--dot` holes every 24px) |
| `--panel` | `#f7f8f6` | `#23272b` | field fill, selected row, callout and change-step fill |
| `--ink` | `#1e2226` | `#e8ebe7` | text, button outlines, heavy rules, icons |
| `--pencil` | `#59616a` | `#9aa2a9` | secondary text, walking steps, quiet actions |
| `--rule` | `#c9cfc9` | `#383e44` | hairline dividers between rows; disabled outlines. Never the outline of a working control |
| `--edge` | `#7c837e` | `#6b737a` | outlines of fields and controls (≥3:1 on wall and panel) |
| `--dot` | ink at 9% | ink at 6% | the wall's bolt holes |
| `--focus`, `--you` | `#2457d6` | `#7aa2ff` / `#4d8df0` | the focus ring; the "you" marker on the rail and the map |
| `--safe` | `#2a6e45` | `#5cc285` | safe connections, on time, good-news callouts |
| `--tight` | `#865404` | `#e0a53a` | tight connections, slightly late, caution callouts |
| `--risk` | `#b13a0a` | `#f08a4b` | at-risk connections |
| `--missed` | `#b42318` | `#f2766b` | missed or late, errors, danger actions |
| `--on-tone` | `#ffffff` | `#1b1e21` | text on a risk colour (badges) |
| `--stop-ink` | `#1e2226` | `#1e2226` | the outline and centre of your stop's white dot, on the rail, the strip and the map: ink in either theme, as a stop's outline is on the map |
| `--shoe-sole`, `--shoe-upper`, `--shoe-strap` | the drawing's colour, none, the drawing's colour | none, the drawing's colour, `--wall` | which part of a gym's shoe is filled, and its straps (see The shoe) |

Line colours come from the feed. Text on a line colour is ink or white, whichever contrasts better; it is computed, never
taken from the feed's `text_color`. A line without a colour uses one fallback grey, `#5e6670`, everywhere.

The map's own paint (casing, walk lines, stop dots) and the MapLibre controls (zoom, locate, attribution) use these
tokens too, in both themes. Controls are square, 44px, edge-outlined, and have no shadow. The attribution is a
`--panel` box with the same outline and `--r-control` corners, never a pill. Ride lines are the line colour only, with
no casing. Walks are round 4.5px ink dots every 8px along the street route (a dot
image placed along the line, never a dashed line, which MapLibre stretches between zoom levels). A ride's stops (where you get on and
off, and those passed on the way) sit on the line's centre (the server places them on the shape): white dots with a
1.5px ink outline, 10px across, so they stand a little proud of the 6px line.
Vehicles are discs in the line's colour, 20px inside a 2.5px ink outline, with the mode's glyph from the rail (train, bus
or ferry) in ink or white: all one size whatever the line is called, since the colour and the trip say which line it is.
You are a `--you` disc, 22px inside a 3px white border (28px across, as on the trip's rail) with a soft dark edge so
the border shows on a pale map (and the halo of the location's accuracy). In it, in white and 16px, is what you're
doing: the rail's walker while you walk or wait at a stop, the vehicle's glyph (train, bus or ferry) while you ride.
Once you're on board, your own vehicle isn't drawn: you are where it is. Vehicles you have yet to catch still are.

**The ride you're on.** The part of its line behind you is dimmed (a 60% wash of the map's ground over it: white on
the light map, black on the dark), and the stops behind you go quiet (smaller, half as strong). The stops ahead stay
plain dots. Where you get off is a larger white dot with the stop's dot in it (12px inside a 2px `--stop-ink` outline,
a 5px `--stop-ink` centre), as on the rail.

**The train you're heading for.** While you're getting to a train or waiting for it, and the server has said where to
board (see Where to board), the platform you board from is picked out in the line's colour: a 16% fill inside a 1.5px
outline at 60%, under the ride's own line. Where your carriage stops on it stands the runner in a "you" marker at 45%
strength: the same disc, border and soft edge, with the runner in white where you'd have the walker, drawn under
your own marker. It never reacts to where you are. There is no "you're there", and your own marker passes over it: a
phone's location on a platform is out by about a carriage, so anything that reacted would flicker. Both go once
you're on board.

**What the map fits.** The whole trip, with the vehicle you'll catch first once it's near. During a trip, once you've
set off (the "you" marker has left the trip's first step: by your location, or by the leave time without one), it
fits the step you're on instead, whole, with you in it: the walk, the ride from where you get on to where you get off,
the change. At the stop waiting, that's the ride you're waiting for, with its vehicle once it's near. The view moves
on to each new step by itself. With three stops to go on a ride (or fewer) it closes in on what's left of it: from the
last stop behind you to yours. Once you've zoomed or moved the map yourself it's left alone, by all of this and by
live updates alike, until the map is next opened.

### Type

One family, Archivo Variable, self-hosted, in three widths and three weights.

| Token | Size | Use |
|---|---|---|
| `--fs-display` | `clamp(56px, 17vw, 80px)` | the countdown hero, the walk timer and pace clocks |
| `--fs-num` | 32px | the big secondary number (arrive time in the hero) |
| `--fs-lead` | 24px | the in-trip "Now" instruction, the map footer's leave time |
| `--fs-title` | 20px | section and screen titles |
| `--fs-strong` | 18px | times in lists and on the board (700, narrow), gym names (800, semi) |
| `--fs-body` | 16px | reading text, buttons, fields, list rows |
| `--fs-small` | 14px | secondary lines, hints, meta |
| `--fs-micro` | 12px | line chips, risk badges, tape labels, the footer |

- **Widths:** `--narrow` (68%) for times and numbers; `--semi` (84%) for titles and gym names; 100% for reading.
- **Weights:** 400 for reading, 600 for labels, buttons and emphasis, 800 for titles and numerals. 700 is allowed only
  for times in lists and the board (`--fs-strong`, narrow).
- Fields always use weight 400, whatever their label's weight.
- **Line height:** 1.45 for text, 1.15 for titles, 1 for numerals.
- Use `font-variant-numeric: tabular-nums` everywhere.
- Sentence case. No all-caps labels and no letter-spaced eyebrows.

### Space, shape and lines

- **Spacing** is on a 4px base: `--s1` 4, `--s2` 8, `--s3` 12, `--s4` 16, `--s5` 24, `--s6` 32, `--s7` 48. The page
  gutter is `--s4`; the gap between sections is `--s5`; rows are padded `--s2` to `--s3`.
- **Radii:**
  - `--r-mark` 2px for tape, chips, badges and risk marks;
  - `--r-control` 4px for buttons, fields and segmented controls;
  - 50% for dots and discs;
  - the app icon keeps its own 7px corner.
  - Nothing else.
- **Lines:**
  - `--heavy` (3px ink) starts a section: Settings sections, Now, sheet bars. The hero on the trip screen sits under a
    hairline instead: the status line above it is part of the same results.
  - `--hair` (1px rule) separates rows.
  - `--bar` (4px) is the side bar of a callout and of the selected row.
  - Control outlines are 1.5px `--edge` (fields) or 1.5px ink (buttons and segmented controls).
- **Shadows:** none. The only exceptions are the "you" marker's soft edge and pulse (on the rail and the map) and the
  inset underline and selection bars.
- **Control height:** `--control` is 44px for every button, field and segmented control. Every tap target is at least
  44px, including text actions, which get an invisible hit area instead of padding.

## Layout

- **Phone (below 1024px):** one column, at most 34rem wide, with `--s4` gutters. The header is sticky, with no rule;
  the current screen is underlined in ink.
- **Desktop (1024px and up):** the header spans the page (fixed, `--header-h`) over two panes. The trip pane on the
  left is `--pane-w` (460px) and scrolls with the page; the map (`.map-pane`, fixed) fills the rest and always shows the
  selected option, or says what will appear there before there is one. There's no "Show on map" button on desktop.
  - In-trip uses the same split: Now/Then and the steps on the left, the map on the right.
  - The map's own note ("Your services appear here…") sits at the bottom left, only as wide as its text; on the phone's
    map sheet it spans the sheet.
  - Settings and the editors use a single readable column, max 40rem, centred. So does the trip screen until there
    is a home and a gym (the "Get started" empty state): there's nothing for a map to show yet.
- **The commit action lives in the action bar.** The screen's main action ("Start trip", "Save gym") sits in a bar
  pinned to the bottom of the pane, with a hairline above it. The commit button is rightmost and filled (see Button); a
  secondary action (the way out, or "Show on map") is leftmost and outlined. Long forms never hide their Save at the
  bottom of the page.
  - A bar holds two buttons at most: three don't fit a phone without wrapping. Another way forward is a text action
    in the content, where it applies: the gym chooser's "Not listed? Add another gym" sits under its list.
- **Order of a trip screen:**
  1. when and which way (and which home, when there are several): everything that changes the search comes before
     the gym, so a search starts only once it's what you meant. It stays on top after a gym is chosen, so it never
     moves;
  2. route (the gym leads the results; before one is chosen, the list of gyms);
  3. status line;
  4. hero;
  5. Earlier/Later;
  6. options board;
  7. summary;
  8. steps;
  9. action bar.

## Components

Each exists once in code and is reused: `web/src/views/ui.tsx` (Button, TextButton, IconButton, Segmented, Section,
Field, Callout, Row, RowAction, ActionBar, Confirm, ConfirmSheet, plus `useDialog` and `onRadioKeys`), the tokens in
`web/src/style.css`, data colours in `web/src/colour.ts`, and error wording in `problem()` (`web/src/api.ts`). Don't restyle one locally; add a variant here if one is really needed.

- **Button**, four looks at one size:
  - `commit`: filled ink, `--wall` text. The one action that moves the screen forward: "Start trip", "Save home",
    "Add gyms", "Switch to this", the one button of an empty state. **At most one on a screen**, and never beside
    another filled button. It's ink, not an accent colour: the same fill as a stop tag or the route's tape.
  - default: ink outline, transparent. The alternative beside a commit and the way out ("Show on map", "Cancel",
    "Back", "Keep"), and an action inside a form that isn't its commit ("Search", "Use my current location",
    "Try again").
  - `danger`: `--missed` outline and text.
  - **disabled**: a thin `--rule` outline, `--pencil` text, no fill, whatever the look.
  - There's no "ghost" variant. A row of buttons puts the commit action last.
- **Row action:** an action that belongs to a list or a section of Settings is a whole row between hairlines, 56px,
  not a button (`RowAction`):
  - adding to a list is the list's last row, a plus then the words at 600: "Add home", "Add gym";
  - an action of a section is a row of body text ending in `›` ("Export backup", "Import backup"), or in the chevron
    of something that opens in place ("Set up another device": down to open, up to close);
  - disabled, its text is `--pencil`.
- **Text action:** underlined, weight 600, inline in a sentence. It has a 44px hit area through `::after`.
  - **Ink or pencil:** ink when it's what gets you going again ("Reload", "Try my location again", "Update now");
    `quiet` (`--pencil`) when it's there if you want it ("Earlier trips", "Measure my pace", "Choose stops",
    "Show 59 more", "End trip"). Most are quiet.
  - One that opens or closes something ends with a 16px chevron, `--s1` after the words: down to open, up to close
    (`Chevron`), and says which with `aria-expanded`. A ride's stops line is the one that does.
- **Icon button:** 44×44, no outline, `--panel` on hover, ink. Always has an `aria-label`. A row's bin is ink like
  its pencil: red arrives with the question (see Destructive actions), not before.
- **Segmented control:** one size (16px/600, 44px), a radio group (one tab stop, arrow keys). The chosen option has
  a 4px ink bar along its bottom (inset), the same ink as the control's outline so it reads as part of the frame;
  the others stay ink, never greyed (grey means disabled).
- **Field:** a label (600) above, then a 44px input in `--panel` with a 1.5px `--edge` outline that turns ink on focus,
  then an optional hint (14px pencil, 400).
  - **A choice from a list** is the system's picker drawn as a field (`Select`): no system arrow, the app's 16px
    chevron at its end. **A tick box** is drawn as a field too: 22px, edge outline on panel, `--r-control`; ticked,
    it fills with ink and its tick is `--wall`.
  - **Number settings** are one line each, rows between hairlines (`NumberField` in Settings): the name on the left,
    a short field (5.5rem, its number right-aligned) on the right. An empty field means "use the default" and shows
    the default as its placeholder; nothing under it says so. A second line under the name, in 14px pencil, only when
    there's something to say: what the setting is for when its name doesn't ("Extra time to get out the door: shoes,
    keys."), "Use the default (3)" once it's changed, and "Saved." for two seconds after a change.
- **Callout:** a `--panel` fill with a 4px left bar. Tones:
  - neutral (ink) for information and confirmations;
  - `good` (`--safe`) for a better option;
  - `caution` (`--tight`) when something works but is degraded;
  - `bad` (`--missed`) for failed or missed.
  - At most one action. A dismiss × only if it can safely be ignored.
  - A callout that suggests a trip shows it under its text as a row from the options board (`TripRow`: leave, tape,
    arrive, risk mark, compact duration), and its text leaves out what the row says.
  - Panels with their own job (the pace test) are sections, not callouts.
- **Section:** a heavy top rule (except the first section on Settings and the screens opened from it, right under the header), then a title (20px/800/semi) with an optional 24px icon (ink lines, one part filled).
  An intro line in 14px pencil may follow.
- **Pick rows:** a search result is a whole-row button with a `›` at the end, not a link.
- **Rows:** a hairline-separated list, min 56px tall. The main text sits on line 1 and details on a second 14px pencil
  line. **Never join details with middle dots.** Actions (icon buttons) go on the right.
- **Tick lists** (the gym chooser): checkbox rows start unticked, and the commit button counts what's ticked. At a
  limit the unticked rows are disabled (pencil text) and a 14px pencil line under the list says what the limit is and
  how to free a place ("A device holds up to 12 gyms. Untick one to tick another.").
- **Direction toggle** (trip screen, at the end of the When row, above the runners): an outlined button, 44px tall
  with `--s2` padding, holding the rail's own drawings at 20px (the house and the arrow in 2px lines, the shoe in its
  own): the house, an arrow, the shoe when the trip goes from home to the gym; the shoe, an arrow, the house when it
  goes home. A tap reverses it. It says which way
  the gyms' rows and their runners go before a gym is chosen, at no cost in height; with a gym chosen it stays where
  it is and reverses that trip. It's the only control that does: the route row has no reverse button.
  - The When control gives up the room: its options are padded `--s1` in this row and never wrap. On a screen too
    narrow for both (under about 340px) the toggle wraps onto a line of its own, still at the right.
  - Its label says which way and what a tap does: "From Home to a gym: reverse", "From 9 Degrees Chatswood to Home:
    reverse". The arrow is a drawing between two drawings, not a symbol standing in for a word (see Copy).
- **Gym rows** (trip screen, before a gym is chosen): a `--rule` tape, the gym's logo, its name (18px/800/semi) and
  its address beneath in 14px pencil. The row is a button that chooses the gym and shows its options.
  - **The runner** (with "Leave now" only): an icon button at the far right of the row, someone running with speed
    lines behind (drawn 2px like the walker on the rail, the lines 1.5px). It starts the fastest trip, the way the direction toggle says,
    without stopping at the options: the option that arrives soonest among those you can still catch. The gym's screen shows
    while it's planned, its status line reading "Finding the fastest trip…" over the board's skeleton (the options
    aren't shown on the way: they'd only flash past), and the trip starts as soon as there's a plan. If nothing can
    be caught, or the plan fails, the screen stays as if the gym had been chosen: the options, or the reason there
    are none.
  - There's no runner with "Leave at" or "Arrive by": a trip that starts, starts now.
- **Route row** (trip screen): an ink tape, the gym's logo, the gym's name (18px/800/semi, a button that changes
  the gym, with a chevron), and "from Home" (or "to Home") beneath it. The direction toggle above reverses the trip.
- **Status line** (trip screen, under the route row): in 14px pencil, a `--safe` dot and "Live data, updated 09:31",
  or a pencil dot and "Timetable only, updated 09:31"; ", refreshing…" follows while a newer plan is on its way.
  Nothing about live data goes in the gym's row. While the first plan loads the line says "Planning…" (or "Finding the
  fastest trip…"); "Walks are rough estimates until the server has prepared the street map." is its second line while
  it's true.
- **Hero** (trip screen): "Leave in" and the countdown, the arrive time, then "for the M1 at 15:20 from Epping
  Station (on time)", then **how sure the chosen trip is**: its risk badge and why, in the steps' words. "At risk
  change at North Ryde Station, 22 s spare"; with several changes, the tightest: "Tight tightest change at Central
  Station, 1 min spare"; with none, "Safe no changes". The word sits beside the number it qualifies, and teaches what
  the squares in the rows mean.
- **Options board:** one row per option: leave, tape strip, arrive, risk mark, compact duration.
  - The selected row gets a `--panel` fill and an inset 4px ink bar. Its content doesn't move.
  - The rows share their columns: the times and the duration are as wide as their widest entry, the duration is
    right-aligned, and the risk mark sits evenly between the arrive time and the duration (`--s3` either side).
  - **The tape** fills its row on the trip's own scale; there is no time axis shared between rows (the times either
    side say when). Each piece is as wide as its share of the trip's time.
    - A ride is 18px tall in its line's colour with the line's name centred, and is **never narrower than its name**
      (plus `--s1` either side). The room it needs comes out of the walks, each in proportion to its size, and then out
      of the rides that are wider than their names, so a long ride is trimmed before a short one is squeezed.
    - Walking is a run of whole round dots (3.2px `--pencil`, about 8px apart, spread evenly from end to end): the walk
      to the first stop, the walk from the last, and between two rides the walk and the wait as one run. A run is at
      least 13px wide while there's room, and two rides never touch.
    - **The tape never leaves its column.** When a trip has too many rides for every name and 13px runs (four rides on
      a 360px phone), the runs narrow evenly, down to one dot (6px). Past that the tape is cut at the end of its column
      rather than run into the arrive time.
    - A ride running 2 min or more late has a 2px `--missed` outline.
  - The board is one tab stop; arrow keys move between options (radio group).
- **Timeline:** time | rail | description, sharing columns through subgrid.
  - Rides: a thick line in the line colour that fills its row, stopping about 2px short of the rules above and below,
    with the mode disc at its top (where you get on) and a dot inside its bottom end (where you get off).
  - A ride's description: line chip, headsign and status; "from" the stop (and platform); "to" the stop at its
    time; then, on a line of its own, `3 stops (7 min)` (or just `7 min` when the stop count isn't known).
  - **A ride opens into its stops.** The stops line is a quiet text action with a chevron (when the server has sent
    the ride's stops). A tap opens the ride: its step runs on into a row for each stop still ahead, the line passing
    through them all, each with its time (14px/600 pencil, narrow), a dot on the line and its name. The last is yours:
    its time and name in the step's own weights (18px/700, 800), "Get off" after the name, and on the line a larger
    white dot with the stop's dot in it (18px, `--stop-ink`). The line ends a little past it (5px). Open, the "to"
    line goes: the last row says it. A tap on the stops line closes it again. It works while planning too.
    - **On the ride you're on** the stops line counts down, `10 of 19 stops to go`, and the part of the line behind
      you is dimmed (the line's colour under 62% of the wall), closed or open. Open, the stops behind you fold into
      one pencil line, `3 stops passed`, you sit in a row of your own under it, and the next stop has "Next" after its
      name.
    - **It opens by itself** with three stops to go, or fewer: so a ride of three stops or fewer is open from the
      moment you're on it. Before that it stays the short step it always was.
    - **Your tap wins.** A ride you've opened stays open; one you've closed stays closed, even for its last stops.
    - "Next" and "Get off" are stop tags: 12px/800 on `--r-mark`, wall on ink.
  - Walks: whole round dots (4.5px, about 10px apart, spaced out to fit; never cut off), stopping as short of the rules
    as a ride does. A walk or change step has a walker (heading right) in the middle instead of a dot. From the house,
    the dots run to just above the rule; into the shoe, from just below it. Changes get a `--panel` row with a risk
    badge.
  - The house or the shoe marks each end, centred on its row's line of text (the time, "Leave Home", "Arrive"). The
    "you" marker (28px) never covers text. It holds what you're doing, in white: at either end the drawing of the place
    you're at; on a ride you're on, the vehicle's glyph (train, bus or ferry); otherwise the walker, walking or
    waiting at a stop. The map's marker is the same.
- **The shoe:** a gym is a climbing shoe, as home is a house: toe down and to the right, a loop on its heel, two
  straps and a rubber sole (`Shoe` in `web/src/views/icons.tsx`, drawn once and used everywhere). It marks the gym's
  end of the rail and of the direction toggle, and heads Gyms in Settings.
  - Its lines are 1.25 in its 24 box at every size, finer than the other drawings (2, or 1.7 on a section's icon): at
    their weight the loop and the straps close up.
  - **The sole is the dark part in either theme**, since rubber is. On a light page that's an outline with the sole
    filled. On a dark page the upper is filled instead and the sole is the wall left inside the outline, as thick as
    the filled sole is on a light page, with the straps cut across the upper in `--wall`. The `--shoe` tokens switch it.
  - In the "you" marker the shoe is white on `--you`, so it's the dark page's drawing in either theme, its straps
    `--you`, its lines 1.5.
  - The app icon and the page before sign-in keep their climbing volume: that's the app's mark, not a gym.
- **Line chip, risk badge, risk mark:** 12px/800 on `--r-mark`. Badge text is `--on-tone`. A line's chip is in its
  line's colour wherever it's drawn, the gym editor's list of lines included: the colours of lines no search has
  used yet are asked of the server (`GET /api/lines`).
- **Trackwork:** a `caution` callout above the board for each line whose trains the options replace with buses: the
  line's chip, "**Trackwork:** buses replace some trains. Their signs say 20T4 or 23T4." The buses keep their own names
  and colour everywhere (chip, tape, map): they're the names on the buses' signs.
- **In-trip warnings** (above Now, at most one at a time):
  - `bad`, when the trip can't be made as it stands. It says what won't be made, then the next best with how sure its
    changes are, that trip's row, and offers the switch: "**You won't make the change at North Ryde Station: the 288
    leaves at 12:34.** Next best, with no changes:", the row, [Switch to this]. The other openings: "You won't make
    the M1 at 11:52." and "One of your services is no longer running."
  - It shows as soon as there's nothing to spare, and then stays until there's the "tight" setting to spare again (1 min
    unless changed), so a connection near the line doesn't come and go with each live update. While it's only staying, it
    says "You may not make…" rather than "You won't make…".
  - `good`, when another way arrives at least 3 minutes sooner and its changes are no riskier than those still ahead
    of you: "**A faster way just opened up:** 4 min sooner, with no changes.", that trip's row, [Switch to this].
    - It can be dismissed, and then that way isn't offered again on this trip: it's known by the vehicles it takes
      that yours doesn't. Only the fastest way is ever offered, so once it's dismissed nothing shows until a different
      way is the fastest.
  - The row is the suggested trip from where you are now: it leaves when you'd have to set off from here (now, on a
    vehicle).
  - **The ride you're on, as a strip** (in Now while you ride, when the server has sent the ride's stops; `RideCountdown`):
    the line diagram above a train's doors. A 14px line in the line's colour across the section: where you got on at
    its left end (the mode's disc, dimmed), a dot for every stop, evenly spaced however far apart they are, and yours
    at the right end as the larger white dot with the stop's dot in it. You ride along it as the "you" marker, and
    the line behind you is dimmed, as on the rail. Under it, in 14px: "Next: Wahroonga Station" in pencil on the left,
    "18 stops to go" in ink (600) on the right; with one to go, just "Next stop is yours" on the right.
    - The instruction above it turns from "On the T9: get off at Central Station" to "Get off at the next stop:
      Central Station" with one stop to go, and the line under it only says when ("Get off at 17:17 (in 4 min)"):
      the strip and the instruction name the stops.
    - How many stops are behind you comes from where you are along the ride's line (your location, or your
      vehicle's when yours has gone quiet); with nothing to place you by, from the times it calls at them. It never
      goes back a stop.
  - **Where to board, as a strip** (in Now while you're getting to a train or waiting for it, when the server says
    which carriage; `BoardStrip`): the train, heading right like everything else on a trip. A piece for each
    carriage, 22px tall with `--s1` between, each an outline only: 1.5px `--rule` with `--r-mark` corners. The front
    carriage (the right-hand one) has a driver's cab, its top front corner swept down to the nose. Your carriage's
    outline is the line's colour instead, at the same weight. No carriage is filled and none is numbered.
    - Under it, the runner from the gym rows (`IconHurry`, at its own 24px) and the instruction at the body's own
      weight, not bold: "Board the 4th carriage from the front." ("Board the front carriage.", "Board the back
      carriage."). The strip above it is the emphasis, so the runner is lighter here than on a gym's row, to sit
      with regular text and the carriages' outlines: drawn 1.5px, its speed lines 1.2px. On a gym's row the
      runner starts the fastest trip; here it marks the quick way off the train. It doesn't say why (which stairs it's
      for): short wins on a platform.
    - It sits straight under the instruction, above when the train leaves ("T1 leaves 19:27 (in 16 min), on time")
      and how much time you have, with `--s3` between.
    - It's in Now from "Get to …" through "Wait for …", and nowhere else: not on the ride's step, not while planning,
      not under the pulled-down map. It goes once you're on board.
    - It shows whenever the server says which carriage, with no test for whether it's worth it. It never aims for a
      lift. Nothing shows when the server doesn't say: a bus, a train of unknown length, a platform the street map
      doesn't have, the next train at the same platform.
  - How sure the changes are reads `with no changes`, `with a safe change` (or tight, at-risk), `with 3 safe changes`,
    `with 2 changes, the tightest at risk`.
- **Longer walk:** a neutral callout above the board for each end whose nearest stop on the gym's lines is beyond the
  longest walk on foot (the server planned from the nearest stops anyway): "**Longer walk:** no stops on these lines
  within a 1 km walk of Home. The nearest is a 1.4 km walk." Distances read `800 m` (to 50 m) or `1.4 km`. It's information, not a
  fault, so it isn't `caution`.
  - It can be dismissed, and then stays away on this device for that walk, kept by the place and its
    nearest stop (the station, for a train, metro or light-rail platform). So it's gone on every trip with that walk,
    whichever gym and either way round, until the place's nearest stop is a different one.
  - Settings, under "Longest walk to a stop" and only while there are any: "You've dismissed 2 "Longer walk" notices.
    Show them again" (a quiet text action; "Show it again" for one).
- **Footer:** a hairline, then the server status and the version. It's for whoever runs the server, so it shows:
  - on Settings, always, at the bottom edge of the screen when the content is shorter than the screen;
  - on the trip screen, only when there's something to act on: the server isn't OK, or the app is out of date.
  - The data credits ("Contains Transport for NSW data…") are only on Settings, at the end, above the footer.
- **Server status** (the footer): a status dot and one short line, so a problem on the server
  gets noticed without anyone checking it. The dot uses the status colours: `--safe` "Server OK", `--tight` "Server
  working, needs a look", `--missed` "Server can't plan" or "Can't reach the server". Each issue follows on its own line
  in plain words. Only what's actionable and usually absent turns it orange (not lines that don't run on a weekend).
  Checked on opening and every 5 minutes while the app is open; checking doesn't count as using the app.
  - The line is a button (a small chevron after it, a 44px hit area): it opens to **everything the status call
    returned**, as rows of label (pencil) and what the server said (ink), hairlines between: the timetable's day and age,
    live data and each feed, live services matched, requests to TfNSW, lines not running today, the street map and the map.
    A row that's a problem is in its status colour (`--tight`, or `--missed` when the server can't plan). Opening it
    asks the server again.
  - **Update now** (the Street map row, on a line of its own under what the server said): a text action, in ink
    whatever the row's tone, that has the server download the street map again instead of waiting for its monthly
    refresh (a path was added to OpenStreetMap). Offered once there's a street map or a failed try at one. The row
    then reads "Updating now: walks use the one from before until it's ready" and the status is asked again every
    5 s while it's open, until the row is back to "Ready, built 8 s ago": the new age is the confirmation. If the
    server can't be asked, the row says why in `--missed` under the action (wording from `problem()`); a download
    that fails is the row's usual `--tight` problem, with Update now to try again.
  - Under the rows, "The server's reply" opens the same way to the reply as it came: JSON on `--panel`, the one place a
    monospace face is used (the system's).
- **Version** (the footer, last): `Version 87692b5`, the first seven characters of the commit the server was built
  from (`dev` for an unstamped local build, a trailing `+` for one with uncommitted changes), in `--pencil` like the
  rest of the footer. Shown once the server has answered; the app is built into the same binary, so it's the app's
  version too.
  - **Out of date:** when the server starts reporting a different version from the one it reported when the page
    loaded (a deploy while the app stayed open), the line becomes "This app is out of date. Reload to get version
    87692b5.", with Reload a text action that reloads the page. Opening the app afresh always loads the newest, so
    this only shows in an app left open.
- **Status text** for a service:
  - on time (or under a minute late): `--safe`;
  - 1 min late: `--tight`;
  - 2 min or more late: `--missed`;
  - no live data, "timetabled": `--pencil`.
  - The word and the colour always agree.
- **In-trip status line** (under Then): only when something is off. "No location just now; following the
  timetable.", "Location unavailable; following the timetable." with "Try my location again", and "Couldn't re-check
  the trip: …". With location on and the last check good there is no line.
- **Inline confirm:** see Destructive actions.
- **Dialog sheet:** a scrim at 45%, then a sheet on the wall colour with a heavy top rule. It rises from the bottom on
  phones and is centred on desktop. A title says what will happen; the body lists exactly what is lost; actions are
  Cancel, then the danger action.
- **Map sheet (phone):** a full-screen dialog. The header leads with "‹ Back to trip" (or "‹ Back"), then the title.
  - **The handle** (during a trip): a 40 × 4px ink bar, `--r-mark`, centred just under the rule between the map and
    the steps, with a 44px hit area that reaches up over the map's bottom edge (and stops short of the attribution in
    the corner). Dragged down 24px or more, the map takes the steps' room; dragged back up, they return; a tap does
    either. Two positions, nothing in between. It's a button ("Show more of the map" / "Show the trip's steps").
  - With the map pulled down, the arrive line stays under it with what to do now: the Now instruction (16px/600) and
    its line beneath in 14px pencil ("T1 leaves 13:48 (in 27 min), on time", "Get off at 14:28 (in 6 min), Wynyard
    Station"). On a ride whose stops are known, the strip and its two labels take that line's place.
  - It stays as you left it for the rest of the trip, however often the map is opened.

## UX conventions

### Time formats

Predictions are in minutes, the pace test is a stopwatch, and clock times are 24-hour. Each kind of time has one
format, everywhere.

| Kind | Format | Used for |
|---|---|---|
| Clock | `09:42` | departures, arrivals, "updated 09:31", "checked 09:32" |
| Countdown | `18 min`, `1 h 5 min`, `now`, `left 2 min ago` | hero, "leaves in", map footer |
| Planned duration | `17 min`, `1 h 7 min` | door to door, walks, rides |
| Compact duration | `42m`, `1h 7m` | only in tabular columns (the options board) |
| Spare time | `1 min spare`; under a minute `40 s spare` | changes (whole minutes, rounded down) |
| Measured | `2:23` | the pace test in Settings: its clock and its result |
| Age | `14 s ago`, `11 min ago`, `5 h ago`, `3 days ago` | the server status in full |

A walk time in a place's list of stops carries a tilde (`~3 min`): it's the street map's, at your pace, when you looked.

### Destructive actions

Use in-app confirmation only, never `window.confirm()`. Everything shares one visual language: a `danger` button for
the destructive choice, the safe choice focused by default, and Escape cancelling.

- **One item** (a home, a gym) gets an **inline confirm**: the row's bin is ink; tapped, the row turns into the
  question, with a `--panel` fill and a 4px `--missed` bar. "Delete 9 Degrees Chatswood?" sits on line 1, what goes with it on line 2,
  then [Keep] [Delete].
- **End trip** gets an **inline confirm bar** under the trip header: "End this trip? Live tracking and re-checks stop."
  then [Keep going] [End trip].
  - End trip is a quiet text action on the right of the trip header. It never sits where the map's "Back to trip" sits,
    and never looks like it.
- **Whole-device actions** get a **dialog sheet**. These are: Reset this device, Import backup, and a link that would
  replace saved settings or change the access token.

### Errors, empty and loading

- **Errors** are a `bad` callout (wording from `problem()`, never a raw exception) that:
  - says what happened in plain words (never a raw exception or server string);
  - says what still works ("Your last plan from 09:31 is below");
  - offers Try again where retrying can help.
  - Keep the last good result on screen under it.
  - A server that can't be reached is an error, not "Loading…".
- **Degraded but working** (timetable only, walks estimated, location off) is a `caution` callout or the status line.
  It's not an error.
- **Empty states** say what to do and offer the action as a button ("Add a gym").
  - **Without a setup link** (the page before sign-in): the mark, the name, and one line in pencil, "Open your setup
    link to start." Nothing to press, and nothing about the server.
- **First run** is two steps, each a question: "Where do you start from?" with "Step 1 of 2. Next: the gyms you climb
  at." under it, then "Which gyms do you climb at?" with "Step 2 of 2." leading its intro. Neither has Cancel: there's
  nothing to go back to.
- **Stops for a place** (the home and gym editors) are closed until asked for: "Using every stop within a 1 km walk."
  then "Choose stops", a text action with a chevron that opens the list. A place with stops already ticked opens with
  the list showing.
- **Loading:** show what's coming in place, as a skeleton of rows or the board's ruled lines (`.skeleton`), rather
  than a bare "Loading…". Long jobs (suggesting lines) show progress beside the job, not only inside the button label.
- **Searching as you choose:** a pick (a gym, reversing the trip) searches at once; editing the time (the When choice,
  day, hour, minute, Earlier/Later with a set time) searches once it has been still for half a second, so stepping
  through a time doesn't search every step on the way. The last result stays on screen meanwhile.
- **After an action, confirm it in a neutral callout**, using the action's own verb: "Saved", "Deleted", "Settings
  saved on this device". Settings that save as you type show a brief "Saved" beside the field.

### Refreshing

The app refreshes itself: plans every 30 s, the server status every 5 minutes, and both at once when the app comes
back to the foreground. So there's no pull-to-refresh (`overscroll-behavior-y: contain` on the page), and no refresh
button. The one thing that needs a reload is a new version of the app, and the footer offers it (see Version).

What the server keeps for itself refreshes on its own schedule too. The one thing that can be asked for early is the
street map, from its row in the server status (see Server status, Update now): its schedule is a month long, and only
the person who mapped a path knows there's something new to fetch.

### Keyboard and focus

- Every interactive element shows the 3px `--focus` ring with a 2px offset.
- Dialogs (map sheet, dialog sheet) use `useDialog`, which does all of this:
  - move focus into the dialog on open;
  - keep focus inside;
  - make the page behind `inert`;
  - close on Escape;
  - return focus to the control that opened them.
- Groups of choices (options board, segmented controls) are radio groups: one tab stop, arrow keys inside
  (`role="radio"`, `aria-checked`, `tabIndex` 0 only on the chosen one, `onRadioKeys` on the group).

### Copy

- Write from the user's side, in plain verbs and sentence case. No filler, no apologies.
- **An action keeps its name through the flow:** "Delete" → "Deleted", "End trip" → the confirm says End trip.
- **One word per concept:**
  - "Cancel" leaves a form unsaved.
  - "Keep" declines a destructive action.
  - "Back" leaves a sheet.
  - "Dismiss" closes a callout.
  - Don't use "Discard" or "Hide".
- **Adding things:** "Add gym" (the action), "Add gyms" (when more than one is ticked: no count), "Save gym" (the
  editor's commit).
- **No symbols as words:** no `→`, no `~` between places, no middle dots in meta. Use a comma or a second line.

## Not doing

- Rounded card stacks, drop shadows, gradients as decoration.
- A single accent colour for buttons or links.
- Colour that doesn't mean a line, a risk level or status, or focus. Selection marks and underlines are ink.
- Middle-dot meta strings, all-caps labels, `→` appended to buttons or links.
- Native `alert()`, `confirm()` or `prompt()`.
- One-off sizes, spacings or radii in a component.
