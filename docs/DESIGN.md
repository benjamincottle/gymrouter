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
| `--tight` | `#865404` | `#e0a53a` | tight and slightly late, as words ("1 min late", "40 s spare") |
| `--tight-fill` | `#e0a100` | `#e0a53a` | tight as a fill: risk marks and badges, the bar of a caution callout, the server status dot. A yellow reads as tight where the dark amber read as brown, but words in it can't be read on the light wall (1.9:1), so they keep `--tight` |
| `--on-tight` | `#1e2226` | `#1b1e21` | text on `--tight-fill` (the Tight badge) |
| `--risk` | `#b13a0a` | `#f08a4b` | at-risk connections |
| `--missed` | `#b42318` | `#f2766b` | missed or late, errors, danger actions |
| `--on-tone` | `#ffffff` | `#1b1e21` | text on a risk colour (badges) |
| `--stop-ink` | `#1e2226` | `#1e2226` | the outline and centre of your stop's white dot, on the rail, the strip and the map: ink in either theme, as a stop's outline is on the map |
| `--shoe-sole`, `--shoe-upper`, `--shoe-strap` | the drawing's colour, none, the drawing's colour | none, the drawing's colour, `--wall` | which part of a gym's shoe is filled, and its straps (see The shoe) |

Line colours come from the feed. Text on a line colour is ink or white, whichever contrasts better; it is computed, never
taken from the feed's `text_color`. A line without a colour uses one fallback grey, `#5e6670`, everywhere.

The basemap is Protomaps' light flavour, and on a dark page its dark flavour recoloured as Google Maps' night style
(`NIGHT` in `mapview.tsx`): slate-navy land `#242f3e`, deep navy water `#17263c`, slate roads `#38414e` (main roads
`#45505f`) with amber-brown motorways `#746855`, muted teal parks, place names in warm tan `#d59563` and street names
in cool grey `#9ca5b3`. Protomaps' own dark is near black, and its water hardly parts from its land.
The map's own paint (casing, walk lines, stop dots) and the MapLibre controls (zoom, attribution) use these
tokens too, in both themes. Controls are square, 44px, edge-outlined, and have no shadow. The attribution is a
`--panel` box with the same outline and `--r-control` corners, never a pill. Ride lines are the line colour only, with
no casing. Walks are round 4.5px ink dots every 8px along the street route (a dot
image placed along the line, never a dashed line, which MapLibre stretches between zoom levels). A ride's stops (where you get on and
off, and those passed on the way) sit on the line's centre (the server places them on the shape): white dots with a
1.5px ink outline, 10px across, so they stand a little proud of the 6px line.
The home and the gym at the trip's ends are drawn as on the rail: the house (18px, in ink) in a 30px `--wall` disc
inside a 2px ink ring. A gym is its brand's logo, as in the lists (24px and whole, in a 36px disc and ring), or the
shoe for a gym without one, with its name beside it in 13px ink haloed in the wall. Before there's a trip to draw
(desktop), your home and all your gyms, a gym chosen with a click, framed to take in the area the app covers too:
Blaxland in the west to Manly in the east.
The map is made afresh when the page turns light or dark (Settings, or the system under Auto), so it's always in the
page's theme. A failure to fetch its vehicles is said in `problem()`'s words, as every other error is.
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
set off (the "you" marker has left the trip's first step: by your location, or by the leave time without one), it fits
the step you're on instead, whole, with you in it: the walk, the ride from where you get on to where you get off, the
change. At the stop waiting, that's the ride you're waiting for, with its vehicle once it's near. The view moves on to
each new step by itself, and stays on the whole step to its end (it doesn't close in on a ride's last stops). Once
you've zoomed or moved the map yourself it's left alone, by all of this and by live updates alike, and a **re-centre**
control appears under the zoom buttons: the same square control, holding four corners of a frame around a dot, drawn
in ink ("Back to the trip"). A tap fits the view again and hands it back to the trip; the control goes until you next
move the map.

### Type

One family, Archivo Variable, self-hosted, in three widths and three weights.

| Token | Size | Use |
|---|---|---|
| `--fs-display` | `clamp(56px, 17vw, 80px)` | the countdown hero, the walk timer and pace clocks |
| `--fs-num` | 32px | the big secondary number (arrive time in the hero) |
| `--fs-lead` | 24px | the arrive time in a trip's panel, the map footer's leave time |
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
  - `--r-sheet` 16px for the top corners of the panel pulled up over a trip's map;
  - the app icon keeps its own 7px corner.
  - Nothing else.
- **Lines:**
  - `--hair` (1px rule) separates rows and starts a section (Settings, its editors). The hero on the trip screen
    has no rule of its own: it sits between two hairlines, the one under the gym's row and the one above
    Earlier/Later.
  - `--frame` (2px `--pencil`) is the rule above and below the planning map sheet's map: its bar and its footer. Quieter than ink, so the map, not its frame, is what you see.
  - `--heavy` (3px ink) is left for the dialog sheet's top.
  - `--bar` (4px) is the side bar of a callout and of the selected row.
  - Control outlines are 1.5px `--edge` (fields) or 1.5px ink (buttons and segmented controls).
- **Shadows:** none. The only exceptions are the "you" marker's soft edge and pulse (on the rail and the map) and the
  inset selection bars.
- **Control height:** `--control` is 44px for every button, field and segmented control. Every tap target is at least
  44px, including text actions, which get an invisible hit area instead of padding.

## Layout

- **Phone (below 1024px):** one column, at most 34rem wide, with `--s4` gutters. The header is sticky, with no rule.
- **The header** says where you are, three ways:
  - planning trips: the app's icon and "Gym Router" (together, a button back to the trip screen's start) on the left,
    the Settings cog (pencil) on the right;
  - on Settings and the screens opened from it: the same header, "Settings" in place of "Gym Router" (not a button),
    and Back where the cog is, a quiet text action like End trip in a trip's header. Back closes an editor if one is open (as its Cancel), else
    goes back to trips;
  - setting up for the first time: the icon and "Gym Router", nothing else (nowhere else to go yet).
- **Desktop (1024px and up):** the header spans the page (fixed, `--header-h`) over two panes. The trip pane on the
  left is `--pane-w` (460px) and scrolls with the page; the map (`.map-pane`, fixed) fills the rest and always shows the
  selected option. Before there's one (no gym chosen yet, or no trip found) it shows your home and your gyms, each gym
  a shoe with its name beside it; a click on a gym chooses it. There's no "Show on map" button on desktop.
  - A trip under way uses the same split, with no header: the trip's panel on the left, the map on the right (see A
    trip under way).
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
  3. status line (only while there's something to say: see Status line);
  4. hero;
  5. Earlier/Later;
  6. options board;
  7. action bar.
  - No steps: they're for a trip under way (Timeline), not for choosing one. The board and the map say enough to
    choose.

## Components

Each exists once in code and is reused: `web/src/views/ui.tsx` (Button, TextButton, IconButton, Segmented, Section,
Field, Callout, Row, RowAction, ActionBar, Progress, Confirm, ConfirmSheet, plus `useDialog` and `onRadioKeys`), the tokens in
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
- **Segmented control:** one size (16px/600, 44px), a radio group (one tab stop, arrow keys). The chosen option is
  marked as the chosen row of the options board is: a `--panel` fill, and a 4px ink bar along its bottom (inset), the
  same ink as the control's outline so it reads as part of the frame. The others stay ink, never greyed (grey means
  disabled).
- **Field:** a label (600) above, then a 44px input in `--panel` with a 1.5px `--edge` outline that turns ink on focus,
  then an optional hint (14px pencil, 400).
  - **A choice from a list** is the system's picker drawn as a field (`Select`): no system arrow, the app's 16px
    chevron at its end. **A tick box** is drawn as a field too: 22px, edge outline on panel, `--r-control`; ticked,
    it fills with ink and its tick is `--wall`.
  - **Number settings** are one line each, rows between hairlines (`NumberField` in Settings): the name on the left,
    a short field (7.5rem) on the right holding the number, right-aligned, and its unit after it in pencil ("5.5
    km/h", "1 min", "1000 m"); the unit is never in the name. An empty field means "use the default" and shows the
    default as its placeholder; nothing under it says so. A second line under the name, in 14px pencil, only when
    there's something to say: what the setting is for when its name doesn't ("Extra time to get out the door: shoes,
    keys."), "Use the default (3)" once it's changed, and "Saved." for two seconds after a change.
- **Callout:** a `--panel` fill with a 4px left bar. Tones:
  - neutral (ink) for information and confirmations;
  - `good` (`--safe`) for a better option;
  - `caution` (`--tight-fill`) when something works but is degraded;
  - `bad` (`--missed`) for failed or missed.
  - At most one action. A dismiss × only if it can safely be ignored.
  - A callout that suggests a trip shows it under its text as a row from the options board (`TripRow`: leave, tape,
    arrive, risk mark, compact duration), and its text leaves out what the row says.
  - Panels with their own job (the pace test) are sections, not callouts.
- **Section:** a hairline on top (except the first section on Settings and the screens opened from it, right under the
  header), then a title (20px/800/semi) with an optional 24px icon (ink lines, one part filled). An intro line in 14px
  pencil may follow. Settings' own sections have no rule at all: each starts with its title and the space above it
  (the editors opened from it keep their hairlines). Its lists end without a closing hairline too: hairlines
  part the rows, and the space parts the sections.
- **A home's or gym's name in a list** (the trip screen's gym rows, Settings, the gym chooser): 18px/800/semi, its
  details (address, lines) under it in 14px pencil. A name is set the same wherever it's listed.
- **Pick rows:** a search result is a whole-row button with a `›` at the end, not a link.
- **Rows:** a hairline-separated list, min 56px tall. The main text sits on line 1 and details on a second 14px pencil
  line. **Never join details with middle dots.** Actions (icon buttons) go on the right.
- **Tick lists** (the gym chooser): checkbox rows start unticked, and the commit button counts what's ticked. At a
  limit the unticked rows are disabled (pencil text) and a 14px pencil line under the list says what the limit is and
  how to free a place ("A device holds up to 12 gyms. Untick one to tick another.").
  - While the lines near home are found for the ticked gyms, the action bar shows how far it is (see Progress), the
    commit button reads "Adding gyms…" and Cancel stays live: leaving stops waiting for the search.
- **Progress** (a job long enough to watch: finding a gym's lines): a `--bar` (4px) of `--rule` that fills with ink,
  then one line in 14px, regular weight: what's being done on the left, the percentage on the right. No spinner.
  - The words are the server's stages: "Processing timetables", then "Analysing trips", once for each of the two days
    searched ("Waiting for another search" if one is already running). In the gym chooser, "Adding your gyms" as the
    bar runs to its end, before the screen goes.
  - The bar is real: the server says how much of each timetable it has read and counts every departure time it
    searches. It's asked every second, and the bar glides from one answer to the next.
  - **In an action bar** it takes the hairline's place along the top, edge to edge, so it's on screen however long
    the list above is; its line sits `--s4` clear of the buttons. Beside a job in the content ("Suggest lines" in the
    gym editor) it sits under the job's button, as wide as the column.
- **A gym's lines** (the gym editor): a row for each kind of transport that has any, between hairlines, in the order
  train, metro, light rail, bus, ferry, regional, coach. Each row starts with the kind's drawing from the rail (24px,
  ink: the train for everything on rails) over its word in 12px/600 pencil, then its lines as chips to tap.
  - A chip is a button, 30px tall and at least 44px wide, 14px/800 narrow, `--r-mark`; `--s2` apart in a row and
    14px between rows, so each has a 44px target. A line in use is filled in its own colour, as on the tape; one that
    isn't is a 1.5px `--edge` outline with its name in ink. A tap adds or drops it (`aria-pressed`).
  - Above the rows, in 14px pencil: "Filled lines are used. Tap a line to add or drop it."
  - A search's lines come first in each row, most used first, then the gym's others. Nothing moves when a chip is
    tapped, and a line that's dropped stays in its row as an outline for as long as the editor is open.
  - A chip doesn't say how often its line was used (its title does, after a search): the order says it.
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
- **Gym rows** (trip screen, before a gym is chosen): a `--rule` tape, the gym's logo (or the shoe, for a gym
  without one: see The shoe), its name (18px/800/semi) and its address beneath in 14px pencil. The row is a button that chooses the gym and shows its options.
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
- **Live or timetabled** (`LiveTag`; at the right end of a row it costs no height on: the "Leave in" row on the trip
  screen, the arrive line during a trip, after its countdown): in 14px pencil, a `--safe` dot and "Live", or a pencil dot
  and "Timetabled". No time beside it. Nothing about live data goes in the gym's row.
  - **It speaks for one ride**, not the server: "Live" when that ride has live times (predicted, or an extra service
    only live data knows about), otherwise "Timetabled". A trip that's only walking has no tag. Whether the server
    has live data at all is the footer's to say (Server status).
  - **On the trip screen** it's the highlighted trip's first ride, the one the countdown counts to; choosing another
    trip changes it. While a refresh of the same search has been on its way for 400 ms (a
    weak signal, or coming back to the app) it reads "Updating…" beside a pencil ring (the dot, not yet filled), so
    the times under it are known to be the last ones; a refresh usually lands sooner and shows nothing. A
    changed search doesn't keep the old plan: it starts again from "Planning…". The ring is pencil, not a status colour: nothing is wrong.
  - **During a trip** it's the ride you're on, or the one you're on your way to (a change included), as the last
    re-check left it. A re-check that fails gets the trip's status line, and
    the word goes until one works again. It shows while there's still a service ahead (not on the last walk, nor once
    you've arrived).
- **Status line** (trip screen, under the route row, in 14px pencil): only while there's something to say that the
  tag can't. "Planning…" (or "Finding the fastest trip…") while the first plan loads, before there's a countdown;
  "Walks are rough estimates until the server has prepared the street map." while it's true; "Updating…" when a newer
  plan is on its way and there are no options, so no countdown to say it beside.
- **Hero** (trip screen, leaving now only): "Leave in" (with the tag at the row's end), the countdown, and the arrive
  time. Nothing else: the chosen row says the rest. With "Leave at" or "Arrive by" there's no hero: the chosen row's
  leave time is the number, and it's already there.
- **Earlier/Later** (trip screen, between the hero and the options): "Earlier trips" on the left and "Later trips"
  on the right, quiet text actions in 14px, under a hairline that parts the countdown's block from the options
  (`--s4` above the line, `--s3` under it). With no countdown above them (no options, or a set time) there's no line.
- **Options board:** one row per option: leave, tape strip, arrive, risk mark, compact duration.
  - The selected row gets a `--panel` fill and an inset 4px ink bar. Its content doesn't move.
  - Under the selected row, inside its fill and bar, starting where the tape starts, two 14px pencil lines: where its
    first ride leaves from and when, with how it runs ("271 at 14:49 from Clarence St After Market St, 4 min late";
    "timetabled" without live times), then **how sure the trip is**: its risk badge and why, in the steps' words, and
    its stops ("At risk change at North Ryde Station, 22 s spare, 12 stops"; with several changes the tightest:
    "Tight tightest change at Central Station, 1 min spare"; with none, "Safe no changes, 2 stops"). The badge
    teaches what the rows' squares mean. Door to door isn't repeated: it's the row's last column. There's no summary
    under the board, and nothing says an option is an alternative route.
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
- **Timeline** (the steps, during a trip only): time | rail | description, sharing columns through subgrid.
  - Rides: a thick line in the line colour that fills its row, stopping about 2px short of the rules above and below,
    with the mode disc at its top (where you get on) and a dot inside its bottom end (where you get off).
  - A ride's description: line chip, headsign and status; "from" the stop (and platform); "to" the stop at its
    time; then, on a line of its own, `3 stops (7 min)` (or just `7 min` when the stop count isn't known).
  - **A ride opens into its stops.** The stops line is a quiet text action with a chevron (when the server has sent
    the ride's stops). A tap opens the ride: its step runs on into a row for each stop still ahead, the line passing
    through them all, each with its time (14px/600 pencil, narrow), a dot on the line and its name. The last is yours:
    its time and name in the step's own weights (18px/700, 800), "Get off" after the name, and on the line a larger
    white dot with the stop's dot in it (18px, `--stop-ink`). The line ends a little past it (5px). Open, the "to"
    line goes: the last row says it. A tap on the stops line closes it again.
    - **On the ride you're on** the stops line counts down, `10 of 19 stops to go`, and the part of the line behind
      you is dimmed (the line's colour under 62% of the wall), closed or open. Open, the stops behind you fold into
      one pencil line, `3 stops passed`, you sit in a row of your own under it, and the next stop has "Next" after its
      name.
    - **It only opens when asked.** Every ride starts closed and stays as you leave it: nothing opens by itself, not
      even for the last stops of the ride you're on.
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
  - **A gym without a logo** (one you added yourself) has the shoe where its logo would be, in ink at the logo's
    size, on the trip screen and in Settings. It isn't coloured and can't be changed: the name tells two apart.
  - Its lines are 1.25 in its 24 box at every size, finer than the other drawings (2, or 1.7 on a section's icon): at
    their weight the loop and the straps close up.
  - **The sole is the dark part in either theme**, since rubber is. On a light page that's an outline with the sole
    filled. On a dark page the upper is filled instead and the sole is the wall left inside the outline, as thick as
    the filled sole is on a light page, with the straps cut across the upper in `--wall`. The `--shoe` tokens switch it.
  - In the "you" marker the shoe is white on `--you`, so it's the dark page's drawing in either theme, its straps
    `--you`, its lines 1.5.
  - The app icon and the page before sign-in keep their climbing volume: that's the app's mark, not a gym.
- **Line chip, risk badge, risk mark:** 12px/800 on `--r-mark`. Badge text is `--on-tone` (`--on-tight` on Tight,
  whose fill is `--tight-fill`). A line's chip is in its line's colour wherever it's drawn, the gym editor's chips
  included: the colours of lines no search has used yet are asked of the server (`GET /api/lines`).
- **Trackwork:** a `caution` callout above the board for each line whose trains the options replace with buses: the
  line's chip, "**Trackwork:** buses replace some trains. Their signs say 20T4 or 23T4." The buses keep their own names
  and colour everywhere (chip, tape, map): they're the names on the buses' signs.
- **In-trip warnings** (in the trip's panel, under the arrive line, at most one at a time):
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
  - **The ride you're on, as a strip** (under the instruction in the trip's panel while you ride, when the server has sent the ride's stops; `RideCountdown`):
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
  - **Where to board, as a strip** (in a train ride's step during a trip, when the server says which carriage;
    `BoardStrip`): the train, heading right like everything else on a trip, across 80% of the step's column from its
    left. A piece for each carriage, 22px tall with `--s1` between, each an outline only: 1.5px `--rule` with `--r-mark` corners. The front carriage (the
    right-hand one) has a driver's cab, its top front corner swept down to the nose. Your carriage's outline is the
    line's colour instead, at the same weight. No carriage is filled and none is numbered.
    - Under it, the runner from the gym rows (`IconHurry`, at 16px, its lines 2 and its speed lines 1.5 in its 24
      box) and the instruction in 14px pencil, like the step's other lines: "Board the 4th carriage from the
      front." ("Board the front carriage.", "Board the back carriage."). The strip above it is the emphasis. On a
      gym's row the runner starts the fastest trip; here it marks the quick way off the train. It doesn't say why
      (which stairs it's for): short wins on a platform.
    - It sits in the ride's step, under where the ride goes and above its stops line, `--s2` either side: in the
      trip's steps only: not with what to do now, and not on the trip screen.
    - It shows on every train ride you haven't boarded yet (those ahead, and the one you're getting to or waiting
      for), so a change's carriage can be read in advance. It goes from a ride once you're on board.
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
- **Footer:** a hairline, then the server status (with the version in its line). It's for whoever runs the server, so it shows:
  - on Settings, always, at the bottom edge of the screen when the content is shorter than the screen;
  - on the trip screen, only when there's something to act on: the server isn't OK, or the app is out of date.
  - The data credits are only on Settings, at the end, above the footer: "Contains Transport for NSW data (CC BY
    4.0). Map data © OpenStreetMap contributors. Basemap by Protomaps."
- **Server status** (the footer): a status dot and one short line, "Server OK, version 87692b5, checked 09:31", so a
  problem on the server gets noticed without anyone checking it. The dot uses the status colours: `--safe` "Server OK", `--tight-fill` "Server
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
  - **Gyms** (the last row, and the only one about this device rather than the server): "7 on this device", then
    **Reset order** on the same line, a quiet text action. It forgets how often each gym has been chosen, so the trip
    screen lists them as added again. It's offered only while a gym has been chosen at least once; once tapped it's
    gone, which is the confirmation. No confirm: nothing but the order is lost.
  - Under the rows, "The server's reply" opens the same way to the reply as it came: JSON on `--panel`, the one place a
    monospace face is used (the system's).
- **Version** (in the server status's line, after the verdict: "version 87692b5"): the first seven characters of the
  commit the server was built from (`dev` for an unstamped local build, a trailing `+` for one with uncommitted
  changes). Shown once the server has answered; the app is built into the same binary, so it's the app's version
  too.
  - **Out of date:** when the server starts reporting a different version from the one it reported when the page
    loaded (a deploy while the app stayed open), the status line drops its version and a line under it says "This app
    is out of date. Reload to get version 87692b5.", with Reload a text action that reloads the page. Opening the app afresh always loads the newest, so
    this only shows in an app left open.
- **Simulator** (temporary, for testing: only with `?sim` in the address; spec, 2026-10-16). It's a tool, not part of
  the app, so it's plain and stays out of the way:
  - On the trip screen, a neutral callout under the header: "Simulator on: trips you start are simulated."
  - During a simulated trip, a panel (`SimPanel`): on desktop over the map's bottom left, `--s3` in from its
    edges, 20rem wide; on a phone across the bottom of the screen. A `--panel` fill at 65%, so what's under it
    shows through, under a `--hair` rule, padded `--s3`, no shadow. It stays live over the phone's map sheet
    (it's outside the sheet, so the sheet's dialog leaves it out of what it makes inert: `data-over-dialogs`). Its
    first line: "Simulation" (600) and the simulated time (narrow numerals; the one clock with seconds,
    `17:42:10`, with the day before it when it isn't today) together in the middle, and Hide (Show when hidden), a
    quiet text action, at the right. Hidden, that line is all there is, padded only `--s1` top and bottom.
  - Then: the speed (Segmented: Pause, 1×, 5×, 20×, 60×); "+1 min", "+5 min" and "Next step", quiet text actions;
    what the simulated you is doing, in 14px pencil; then rows between hairlines, a name on the left and its control
    on the right, as Number settings: Walking pace (Select, 50% to 150%), Stay where you are (tick box), Location
    (Segmented: Good, Poor, Off), and for each ride its line chip and the time it leaves, with how late it runs
    (Select: On time, 1 min late to 15 min late).
- **Status text** for a service:
  - on time (or under a minute late): `--safe`;
  - 1 min late: `--tight`;
  - 2 min or more late: `--missed`;
  - no live data, "timetabled": `--pencil`.
  - The word and the colour always agree.
- **In-trip status line** (in the trip's panel, under what to do now): only when something is off. "No location just now; following the
  timetable.", "Location unavailable; following the timetable." with "Try my location again", and "Couldn't re-check
  the trip: …". With location on and the last check good there is no line.
- **Inline confirm:** see Destructive actions.
- **Dialog sheet:** a scrim at 45%, then a sheet on the wall colour with a heavy top rule. It rises from the bottom on
  phones and is centred on desktop. A title says what will happen; the body lists exactly what is lost; actions are
  Cancel, then the danger action.
- **Map sheet (phone, while planning):** a full-screen dialog. Its header is a trip's: the app's icon, the title,
  and Back at the right, a quiet text action where a trip has End trip. Under
  the map: the leave time (24px) and the arrive time, the option's tape, and Start trip.
- **A trip under way** (`InTrip`): one screen, the map with the trip in a panel under it. Started trips go straight
  to it; there's no separate summary screen and no "Show on map".
  - **No header**: the map runs to the top of the screen (its controls clear of the phone's status bar), so it feels
    like the whole screen, with the trip pulled up from the bottom. No icon, no title: the map's ends and the steps
    say where it goes.
  - **End trip** is a map chip at the map's top left, in line with the zoom buttons at its right: a text action drawn
    as the map's controls are (44px, `--panel` fill, 1.5px `--edge` outline, `--r-control` corners, 600 ink). Its
    confirm takes its place there, over the map (see Destructive actions). On desktop it's over the map pane's top
    left.
  - The map fills the screen and stays put. The panel is pulled up over it: no rule, its top corners rounded
    (`--r-sheet`), at most the screen less 72px (the map's top and End trip stay in sight). The map's attribution
    sits under End trip, where the panel can't cover it. When the map fits the trip it fits it above the panel, as
    tall as the panel is then; moving the panel never moves the map. It holds, in order:
    - **the arrive line**: "Arrive 14:28" (24px narrow numerals; "Arrived" once you're there) on the left; on the
      right, in 14px pencil, the countdown to it ("in 52 min") and the Live tag;
    - **a warning**, when there is one (see In-trip warnings);
    - **what to do now**: the instruction (16px/600, "Get to Town Hall Station for the T1", "Wait for…", "On the T9:
      get off at…") and its line beneath in 14px pencil ("T1 leaves 13:48 from platform 1 (in 27 min), on time",
      naming the platform when the stop is one of a station's; "Get off at 14:28 (in 6 min), Wynyard Station"), with
      the time to spare to the stop under it while you're walking to it ("2 min spare from here.", `--safe`,
      `--tight` under a minute, `--missed` "You need to hurry: …"). On a ride whose stops are known, the strip and
      its two labels take that line's place;
    - **the status line**, only when something is off;
    - **the steps** (Timeline), scrolling on their own (at most 38% of the screen's height), the step you're on
      scrolled into view (again once your location first places you: until then it's the
      clock's guess).
  - Not shown: the leave time before you set off (the steps' first row has it), and the next change ("Then": the
    steps show it with its risk).
  - **The handle** (phone): a 40 × 3px `--pencil` bar, `--r-mark`, centred at the top of the panel, with a 44px hit
    area. Dragged, the panel follows to any height, over the map; let go within 48px of its foot (what to do now
    showing, and no steps), it settles there. No other position is held for it: where it's let go, it stays. A tap
    takes it down to its foot, or back up as it opened (the steps at most 38% of the screen). It's a button ("Show
    more of the map" / "Show the trip's steps"). The panel stays as you left it for the rest of the trip.
  - **Desktop:** the panel is the left pane, from the top of the page (no header there either), and the map the
    right; no handle, and the steps scroll with the page.

## UX conventions

### Time formats

Predictions are in minutes, the pace test is a stopwatch, and clock times are 24-hour. Each kind of time has one
format, everywhere.

| Kind | Format | Used for |
|---|---|---|
| Clock | `09:42` | departures, arrivals, "The plan below is from 09:31" |
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
- **End trip** gets an **inline confirm bar**: "End this trip? Live tracking and re-checks stop." then [Keep going]
  [End trip].
  - End trip is a chip over the map's top left (see A trip under way). Its confirm bar takes the chip's place, across
    the top of the map, so nothing moves.
- **Whole-device actions** get a **dialog sheet**. These are: Reset this device, Import backup, and a link that would
  replace saved settings or change the access token.

### Errors, empty and loading

- **Errors** are a `bad` callout (wording from `problem()`, never a raw exception) that:
  - says what happened in plain words (never a raw exception or server string);
  - says what still works ("Your last plan from 09:31 is below");
  - offers Try again where retrying can help.
  - Keep the last good result on screen under it.
  - A server that can't be reached is an error, not "Loading…".
- **Degraded but working** (timetabled times, walks estimated, location off) is the tag beside the countdown, the
  status line or a `caution` callout.
  It's not an error.
- **Empty states** say what to do and offer the action as a button ("Add a gym").
  - **Without a setup link** (the page before sign-in): the mark, the name, and one line in pencil, "Open your setup
    link to start." Nothing to press, and nothing about the server.
- **First run** is two steps, each a question: "Where do you start from?" with "Step 1 of 2. Next: the gyms you climb
  at." under it, then "Which gyms do you climb at?" with "Step 2 of 2." leading its intro. Neither has Cancel: there's
  nothing to go back to.
- **Editing a home or a built-in gym:** its name as the title and its address under it, as plain text: they stay as
  they were set (a built-in gym's are the app's; a home that moves is deleted and added again). Only its stops, and
  a gym's lines, can be changed. Adding a home, and a gym you added yourself, have the Name and Address fields. A
  home keeps the address it was found by, on the device only.
- **A gym without lines from your home** (Settings' list): "13 lines, none from Home yet". Its pencil is right beside
  it.
- **Stops for a place** (the home and gym editors) are closed until asked for: "Using every stop within a 1 km walk."
  then "Choose stops", a text action with a chevron that opens the list. A place with stops already ticked opens with
  the list showing.
- **Loading:** show what's coming in place, as a skeleton of rows or the board's ruled lines (`.skeleton`), rather
  than a bare "Loading…". Long jobs (finding a gym's lines) show how far they are (see Progress), not only a button label.
- **Searching as you choose:** a pick (a gym, reversing the trip) searches at once; editing the time (the When choice,
  day, hour, minute, Earlier/Later with a set time) searches once it has been still for half a second, so stepping
  through a time doesn't search every step on the way. The last result stays on screen meanwhile.
- **After an action, confirm it in a neutral callout**, using the action's own verb: "Saved", "Deleted", "Settings
  saved on this device". Settings that save as you type show a brief "Saved" beside the field.

### Refreshing

The app refreshes itself: a plan as soon as the server says its live data has changed (about every 30 s while the
app is in use), else every 60 s (every 30 s while it can't hear from the server); the map's vehicles as soon as the
server has new positions (about every 15 s), else every 30 s (10 s while it can't hear from the server); the server
status every 5 minutes; and all of them at once when the app comes back to the foreground. So there's no
pull-to-refresh (`overscroll-behavior-y: contain` on the page), and no refresh
button. The one thing that needs a reload is a new version of the app, and the footer offers it (see Version).

What the server keeps for itself refreshes on its own schedule too. The one thing that can be asked for early is the
street map, from its row in the server status (see Server status, Update now): its schedule is a month long, and only
the person who mapped a path knows there's something new to fetch.

### Keyboard and focus

- Every interactive element shows the 3px `--focus` ring with a 2px offset.
- Focus the app moves itself (into a dialog, onto Keep in a confirm, back to what opened a dialog) shows its ring only
  when the last thing done was with the keyboard (`focusQuietly`): after a tap there's no ring, as a tapped control has
  none.
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
