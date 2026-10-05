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
  - the highlight colour, used only for selection marks and the current-screen underline;
  - the focus ring and the "you" marker, in blue.
- **Times read like a departure board.** Times are set in condensed Archivo numerals.
- **The tape strips are the signature.** Options are tape strips on one shared time axis, and the trip's steps hang off
  a rail in the line colours. Spend boldness there and keep everything around them quiet.
- **Rules, not cards.** Sections start with a heavy ink rule and rows are separated by hairlines. Don't use rounded
  card stacks or drop shadows.

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
| `--hl` | `--ink` or a grade colour | | Settings > Highlight colour: underlines and selection marks only |

Line colours come from the feed. Text on a line colour is ink or white, whichever contrasts better; it is computed, never
taken from the feed's `text_color`. A line without a colour uses one fallback grey, `#5e6670`, everywhere.

The map's own paint (casing, walk lines, stop dots) and the MapLibre controls (zoom, locate, attribution) use these
tokens too, in both themes. Controls are square, 44px, edge-outlined, and have no shadow. Ride lines are the line colour only, with
no casing. Walks are round 4.5px ink dots every 8px along the street route (a dot
image placed along the line, never a dashed line, which MapLibre stretches between zoom levels). A ride's stops (where you get on and
off, and those passed on the way) sit on the line's centre (the server places them on the shape): white dots with a 1px ink outline, as wide as the line.

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
  - `--heavy` (3px ink) starts a section: the hero, Settings sections, Now, sheet bars.
  - `--hair` (1px rule) separates rows.
  - `--bar` (4px) is the side bar of a callout and of the selected row.
  - Control outlines are 1.5px `--edge` (fields) or 1.5px ink (buttons and segmented controls).
- **Shadows:** none. The only exceptions are the "you" marker's pulse and the inset underline and selection bars.
- **Control height:** `--control` is 44px for every button, field and segmented control. Every tap target is at least
  44px, including text actions, which get an invisible hit area instead of padding.

## Layout

- **Phone (below 1024px):** one column, at most 34rem wide, with `--s4` gutters. The header is sticky, with no rule;
  the current screen is underlined in `--hl`.
- **Desktop (1024px and up):** the header spans the page (fixed, `--header-h`) over two panes. The trip pane on the
  left is `--pane-w` (460px) and scrolls with the page; the map (`.map-pane`, fixed) fills the rest and always shows the
  selected option, or says what will appear there before there is one. There's no "Show on map" button on desktop.
  - In-trip uses the same split: Now/Then and the steps on the left, the map on the right.
  - Settings and the editors use a single readable column, max 40rem, centred. So does the trip screen until there
    is a home and a gym (the "Get started" empty state): there's nothing for a map to show yet.
- **The commit action lives in the action bar.** The screen's main action ("Start trip", "Save gym") sits in a bar
  pinned to the bottom of the pane, with a hairline above it. The commit button is rightmost; a secondary action (the way
  out, or "Show on map") is leftmost. Long forms never hide their Save at the bottom of the page.
- **Order of a trip screen:**
  1. when (and which home, when there are several): everything that changes the search comes before the gym, so a
     search starts only once it's what you meant. It stays on top after a gym is chosen, so it never moves;
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
Field, Callout, Row, ActionBar, Confirm, ConfirmSheet, plus `useDialog` and `onRadioKeys`), the tokens in
`web/src/style.css`, data colours in `web/src/colour.ts`, and error wording in `problem()` (`web/src/api.ts`). Don't restyle one locally; add a variant here if one is really needed.

- **Button**, outline only (no button is ever filled), three looks at one size:
  - default: ink outline, transparent;
  - `danger`: `--missed` outline and text;
  - **disabled**: a thin `--rule` outline, `--pencil` text, no fill.
  - There's no "ghost" variant. A row of buttons puts the commit action last.
- **Text action:** underlined, weight 600, inline in a sentence. `quiet` uses `--pencil`. It has a 44px hit area
  through `::after`. Use it for things like "Earlier trips", "Time my walk" and "Measure my pace".
- **Icon button:** 44×44, no outline, `--panel` on hover. The `danger` variant colours the icon `--missed`. Always has
  an `aria-label`.
- **Segmented control:** one size (16px/600, 44px), a radio group (one tab stop, arrow keys). The chosen option has
  a 4px ink bar along its bottom (inset), the same ink as the control's outline so it reads as part of the frame,
  whatever the highlight colour; the others stay ink, never greyed (grey means disabled). Not `--hl`: a coloured bar
  clashed with the ink outline, and outlining controls in `--hl` would make it a button accent colour.
- **Field:** a label (600) above, then a 44px input in `--panel` with a 1.5px `--edge` outline that turns ink on focus,
  then an optional hint (14px pencil, 400).
  - Defaults: an empty field means "use the default", and its hint says what the default is ("Empty uses the default,
    4.7"). A changed field shows its value and offers "Use the default (3)". Settings save as they change and say
    "Saved." in the hint for two seconds (`NumberField` in Settings).
- **Callout:** a `--panel` fill with a 4px left bar. Tones:
  - neutral (ink) for information and confirmations;
  - `good` (`--safe`) for a better option;
  - `caution` (`--tight`) when something works but is degraded;
  - `bad` (`--missed`) for failed or missed.
  - At most one action. A dismiss × only if it can safely be ignored.
  - Panels with their own job (pace test, walk timer) are sections, not callouts.
- **Section:** a heavy top rule (except the first section on Settings and the screens opened from it, right under the header), then a title (20px/800/semi) with an optional 24px icon (ink lines, one `--hl` part).
  An intro line in 14px pencil may follow.
- **Pick rows:** a search result is a whole-row button with a `›` at the end, not a link.
- **Rows:** a hairline-separated list, min 56px tall. The main text sits on line 1 and details on a second 14px pencil
  line. **Never join details with middle dots.** Actions (icon buttons) go on the right.
- **Route row** (trip screen): a tape in `--hl`, the gym's logo, the gym's name (18px/800/semi, a button that changes
  the gym, with a chevron), "from Home" beneath it, and a reverse button (⇅) on the right.
- **Options board:** one row per option: leave, tape strip, arrive, risk mark, compact duration.
  - The selected row gets a `--panel` fill and an inset 4px `--hl` bar. Its content doesn't move.
  - Tape labels appear only where the whole line name fits.
  - The board is one tab stop; arrow keys move between options (radio group).
- **Timeline:** time | rail | description, sharing columns through subgrid.
  - Rides: a thick line in the line colour that fills its row, stopping about 2px short of the rules above and below,
    with the mode disc at its top (where you get on) and a dot inside its bottom end (where you get off).
  - A ride's description: line chip, headsign and status; "from" the stop (and platform); "to" the stop at its
    time; then, on a line of its own, `3 stops (7 min)` (or just `7 min` when the stop count isn't known).
  - Walks: whole round dots (4.5px, about 10px apart, spaced out to fit; never cut off), stopping as short of the rules
    as a ride does. A walk or change step has a walker (heading right) in the middle instead of a dot. From the house,
    the dots run to just above the rule; into the hold, from just below it. Changes get a `--panel` row with a risk
    badge.
  - The house or hold marks each end. The "you" marker never covers text.
- **Line chip, risk badge, risk mark:** 12px/800 on `--r-mark`. Badge text is `--on-tone`.
- **Trackwork:** a `caution` callout above the board for each line whose trains the options replace with buses: the
  line's chip, "**Trackwork:** buses replace some trains. Their signs say 20T4 or 23T4." The buses keep their own names
  and colour everywhere (chip, tape, map): they're the names on the buses' signs.
- **Server status** (the footer, above the data credits): a status dot and one short line, so a problem on the server
  gets noticed without anyone checking it. The dot uses the status colours: `--safe` "Server OK", `--tight` "Server
  working, needs a look", `--missed` "Server can't plan" or "Can't reach the server". Each issue follows on its own line
  in plain words. Only what's actionable and usually absent turns it orange (not lines that don't run on a weekend).
  Checked on opening and every 5 minutes while the app is open; checking doesn't count as using the app.
- **Status text** for a service:
  - on time (or under a minute late): `--safe`;
  - 1 min late: `--tight`;
  - 2 min or more late: `--missed`;
  - timetable only: `--pencil`.
  - The word and the colour always agree.
- **Inline confirm:** see Destructive actions.
- **Dialog sheet:** a scrim at 45%, then a sheet on the wall colour with a heavy top rule. It rises from the bottom on
  phones and is centred on desktop. A title says what will happen; the body lists exactly what is lost; actions are
  Cancel, then the danger action.
- **Map sheet (phone):** a full-screen dialog. The header leads with "‹ Back to trip" (or "‹ Back"), then the title.

## UX conventions

### Time formats

Predictions are in minutes, measurements are a stopwatch, and clock times are 24-hour. Each kind of time has one
format, everywhere.

| Kind | Format | Used for |
|---|---|---|
| Clock | `09:42` | departures, arrivals, "updated 09:31", "checked 09:32" |
| Countdown | `18 min`, `1 h 5 min`, `now`, `left 2 min ago` | hero, "leaves in", map footer |
| Planned duration | `17 min`, `1 h 7 min` | door to door, walks, rides |
| Compact duration | `42m`, `1h 7m` | only in tabular columns (the options board) |
| Spare time | `1 min spare`; under a minute `40 s spare` | changes (whole minutes, rounded down) |
| Measured | `2:23` | walk timer, timed walks, "timed 2:23" on stops, pace results |

Estimated walk times carry a tilde (`~3 min`); measured ones don't.

### Destructive actions

Use in-app confirmation only, never `window.confirm()`. Everything shares one visual language: a `danger` button for
the destructive choice, the safe choice focused by default, and Escape cancelling.

- **One item** (a home, a gym, a timed walk) gets an **inline confirm**: the row turns into the question, with a
  `--panel` fill and a 4px `--missed` bar. "Delete 9 Degrees Chatswood?" sits on line 1, what goes with it on line 2,
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
- **Loading:** show what's coming in place, as a skeleton of rows or the board's ruled lines (`.skeleton`), rather
  than a bare "Loading…". Long jobs (suggesting lines) show progress beside the job, not only inside the button label.
- **Searching as you choose:** a pick (a gym, reversing the trip) searches at once; editing the time (the When choice,
  day, hour, minute, Earlier/Later with a set time) searches once it has been still for half a second, so stepping
  through a time doesn't search every step on the way. The last result stays on screen meanwhile.
- **After an action, confirm it in a neutral callout**, using the action's own verb: "Saved", "Deleted", "Settings
  saved on this device". Settings that save as you type show a brief "Saved" beside the field.

### Keyboard and focus

- Every interactive element shows the 3px `--focus` ring with a 2px offset.
- Dialogs (map sheet, dialog sheet) use `useDialog`, which does all of this:
  - move focus into the dialog on open;
  - keep focus inside;
  - make the page behind `inert`;
  - close on Escape;
  - return focus to the control that opened them.
- Groups of choices (options board, swatches, segmented controls) are radio groups: one tab stop, arrow keys inside
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
- **Adding things:** "Add gym" (the action), "Add 3 gyms" (when it's a count), "Save gym" (the editor's commit).
- **No symbols as words:** no `→`, no `~` between places, no middle dots in meta. Use a comma or a second line.

## Not doing

- Rounded card stacks, drop shadows, gradients as decoration.
- A single accent colour for buttons or links.
- Colour that doesn't mean a line, a risk level or status, the highlight or focus.
- Middle-dot meta strings, all-caps labels, `→` appended to buttons or links.
- Native `alert()`, `confirm()` or `prompt()`.
- One-off sizes, spacings or radii in a component.
