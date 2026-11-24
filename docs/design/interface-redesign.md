# MaxPack: redesign of the two interfaces (design)

*24 November 2026. It changes the look of the PC window ([pc-interface.md](pc-interface.md),
[pc-window.md](../pc-window.md)) and of the console page (`src/ui.html` of maxpack-ps5, port 8095). It
does not change what they do.*

## 1. Intent

Your words: "improve the UX and UI of everything so that it looks as striking and pretty as possible,
functional and with striking animations if possible".

There are two interfaces:
- **the PC window**, for compressing, decompressing, verifying and seeing details;
- **the console page**, with the library, the installer and the settings. It opens in the PS5 browser,
  with the controller, and also on a phone.

You saw three mock-ups: A "Ambient", B "Colour blocks" and C "Neon". **You chose A.**

## 2. Goals, non-goals and anti-goals

**Goals**
- R1. **Direction A on both interfaces.** It is like the PS5 menu: a background that takes the colour of
  the game, the blue of MaxPack's icon with a gradient to cyan for what advances and mint green for the
  space saved.
- R2. **The console, livelier:**
  - the background takes the colour of the focused game;
  - the focused game shows large at the top;
  - cards grow when focused, with a white ring and a halo of their colour;
  - the tabs have a white pill that slides;
  - the progress bars have a shine that runs along them;
  - the game sheet and the dialogs appear with an animation.
- R3. **The PC, livelier, without moving anything from its place:**
  - the menu highlight slides from one section to another;
  - games enter the list with an animation;
  - the bars have a shine, and the "-72 %" appears with a pop;
  - the dialogs appear with an animation;
  - the end of a compression is the star moment: the size bar squeezes down to the new size and the GB
    saved count up, with a small burst.
- R4. **Animations that the PS5 runs without stutter.** Only with `transform` and `opacity`. With
  `prefers-reduced-motion`, nothing moves and everything keeps working.

**Non-goals**
- Changing functions, texts, the order of things or the layout of the screens. The only new thing you
  see is the large name of the focused game on the console. It repeats data that is already on the card.
- Changing NXPress. MaxPack stops looking like NXPress; bringing this look to it is another job.
- Own fonts. System ones are used.
- A light theme on the console. It stays dark only, as now.

**Anti-goals**
- Stutter or slowness on the PS5 because of the effects.
- Losing anything: a function, a text, a language, right-to-left reading (Arabic), the focus visible
  with the controller or what a screen reader reads.
- Reading worse: less contrast than WCAG AA asks for text.
- Red CI, or anything loaded from the internet.

## 3. Constraints

**Console page** (`src/ui.html`, a single file):
- **The PS5 browser is an old WebKit.** There is no `aspect-ratio`, `:has()`, `color-mix()`, `inset` or
  `@starting-style`. There is `async`/`await`, flex with `gap` and `transform`. The existing code
  already avoids those things, and the new code does the same.
- **It is used with the controller** (D-pad, L2/R2) on a 1920 x 1080 TV, and with a finger on a phone.
- **Offline.** It uses appcache and makes no requests outside. The CSS and JS are inside the file.
- **11 languages, Arabic included.** No key of `MSG` is removed. If one is added, it goes at least in
  `en`, which is the fallback language.
- **CI.**
  - `node --check` of the `<script>`.
  - `tools/check_identity.sh`, which requires that the line of the credit link (`<a class="gh-link
    button" data-credit="kraken-mount" ...>`) and those of `aboutBased` stay **exactly** the same.
  - `node --check tools/mock_ui.js`, checked by hand.

**PC window** (`gui/frontend`):
- **The security policy (CSP) is `default-src 'self'`.** The HTML cannot have `style=` attributes or
  `<style>` tags. From JS, `element.style.transform` can be set.
- **`gui/frontend_test.go`** requires:
  - that `innerHTML` receives only fixed templates;
  - that data goes in with `textContent`;
  - that nothing is loaded from outside;
  - that es.js and en.js have the same keys, and that every text asked for is in both;
  - that the usual ids, sections and dialogs are there, hanging from `<body>`;
  - that the scripts load in their order;
  - that every file has the SPDX header.
- **It runs on three web engines:** WebView2 on Windows, WKWebView on macOS and WebKitGTK on Linux. It
  has a **light and dark theme**, the system's or the one chosen in Settings.

## 4. Approaches

| | What it is | In favour | Against |
|---|---|---|---|
| **A · Ambient** (chosen) | Background in the colour of the focused game, blue to cyan, mint green | Feels like the PS5 itself; keeps MaxPack's blue; its effects are cheap (gradients and opacity) | Depends on getting the colour of each game (with a fallback) |
| B · Colour blocks | A solid colour per screen and per section, big shapes, no glass | The lightest for the PS5 | Less "PS5"; new colours that do not come from the icon |
| C · Neon | Arcade: pink and cyan with glow, moving grid | The most striking | Tiring to the eye, worse contrast, the heaviest for the PS5 |

## 5. Design

### 5.1 Shared specs

**Colours, dark theme** (the console and the PC in dark):

| Name | Value | What for |
|---|---|---|
| Night | `#07101f` (console), `#0b111d` (PC) | background |
| Surface | `#111827` / `#172033` (PC); white at 6-12 % (console) | panels, rows, cards |
| Line | `#222d42` | borders and separators |
| Text | `#eef3ff` | text |
| Muted | `#93a0b8` | secondary text |
| MaxPack blue | `#2d7dff` | choosing, starting, focus |
| Cyan | `#36e2ff` | end of the gradient of what advances |
| Mint | `#3fe3a2` | space saved, OK |
| Amber | `#ffb547` | warnings |
| Coral | `#ff5c73` | errors and what cannot be undone |

**PC light theme:** background `#eef2f8`, white panels, line `#dde3ec`, text `#111a2b`, muted `#5b667a`,
blue `#1b6ef3`, cyan `#0a9fd0`, mint `#0f9468`, amber `#9a5a00`, coral `#d02a43`. The blue to cyan
gradient and its shine are the same.

**Font:** the system's. "Segoe UI Variable Display" for titles and big numbers, with weight 700-800 and
slightly tighter letters (`letter-spacing` -0.02 to -0.04 em). Figures use `tabular-nums`. Group titles
go from UPPERCASE to normal sentence case in bold.

**Motion:**
- Entrances and focus: 0.3-0.45 s with a soft bounce, `cubic-bezier(.3, 1.35, .5, 1)`.
- Fades: 0.8-1.1 s.
- Shine of the bars: 1.6-1.8 s in a loop.
- All with `transform` and `opacity`. Bars stop animating `width` and switch to `transform: scaleX()`.
- With `prefers-reduced-motion` there are no animations or transitions, as now.

### 5.2 The console page

- **Ambient background.** Two fixed layers behind everything, each with radial gradients of the game's
  colour. When the game changes, one fades into the other with `opacity` in 1.1 s.
  - The colour comes from the game's icon (`/icon/<TID>`, same origin). It is painted on a small canvas
    and the most saturated average colour is kept. It is computed once per game and stored.
  - If there is no icon or it fails, it comes from the hue that `gameHue(title_id)` already gives.
  - With no focused game, the background is MaxPack's blue.
  - Over the layers goes a veil that darkens downwards so the text reads well.
- **Less glass.** Today almost everything uses `backdrop-filter` (blur), and with a background that
  changes it would cost a lot on the PS5. It switches to translucent fills without blur. Only the
  background of the game sheet and of the dialogs keeps it, as they do not move while open.
- **Header.**
  - MaxPack's icon, with today's glow.
  - The tabs with a white pill that slides with `translateX` + `scaleX` to the chosen tab.
  - The Stop button.
- **Large name (new, only in the library and on screens of 860 px or more):**
  - the name of the focused game, at about 50 px on the 1920 scale, weight 800;
  - below it, its ID, version, type and state, with the same labels the card already uses;
  - when the game changes, the text fades and rises;
  - it has `aria-hidden="true"`, because the card already says it;
  - with no focus on a card the last one stays; with no games, it is not shown.
- **Filters and drives.** The same pills and meters, without blur. The meter is drawn with `scaleX` and
  the blue to cyan gradient.
- **Cards.**
  - On the PS5 the controller moves a cursor (the page does not handle the arrows there, `PS`), so "the
    focused card" is the one that has the focus **or the cursor over it**. With the controller on a PC
    or with the keyboard it is the focus; on the PS5, the cursor. The ambient background and the large
    name follow whichever of the two happened last.
  - The focused card grows to 1.08, with a 3 px white ring plus a faint outer ring and a halo of its
    colour underneath. The ring and the halo are layers that fade with `opacity`.
  - While a card is focused, the others drop to 85 %. A class set by JS controls it, because `:has` does
    not exist on the PS5.
  - The name strip becomes a dark gradient, without blur.
  - The type and state pills stay where they are.
- **Game sheet and dialogs.** The background fades in and the sheet enters growing from 0.96 to 1. It is
  an animation with `@keyframes` when `hidden` is removed.
- **Install.**
  - The job in progress carries the game's cover (from its icon, or its initials), the percentage in
    large figures and a thick bar with a gradient and shine.
  - The one chosen in the list grows a little and has a white ring.
  - The Install button has the blue gradient.
  - Group titles are in normal sentence case.
- **Settings.** The same rows, on the new surfaces. The switches and buttons stay the same.
- **Notices.** They enter sliding from the top. They carry the new warning and error colours.
- **Phone (< 860 px).** No large name and no dimming of the other cards. Fixed blue ambient background.
  The current breakpoints are kept (4, 3 and 2 columns).
- **Arabic.** The `[dir="rtl"]` rules are kept. The pill and the halo work the same in both directions.

### 5.3 The PC window

Everything stays in its place: sections on the left, list in the centre, options on the right and
progress at the bottom.

- **Menu.**
  - A highlight (blue gradient with a stripe on the left) slides with `translateY` to the open section.
  - The little square of the open section's icon fills with the blue to cyan gradient and grows a
    little.
  - Verify and Details keep their teal tone.
- **Section header.**
  - Title at 26 px, weight 700.
  - Little icon square with the gradient.
  - A soft blue glow behind, which does not move.
- **Empty list.**
  - The drop arrow floats slowly.
  - A ring pulses behind it, with `scale` and `opacity`.
- **List with games.**
  - Each game that is added enters rising and fading in. Only when added, not on repaint.
  - Each game's bars are drawn with `scaleX`: blue to cyan when compressing, mint when verifying, with
    a shine.
  - The state has its colour. While working, its dot blinks.
  - The "-72 %" appears with a pop when it finishes.
- **Bottom.** The total bar is thicker, with a gradient, shine and halo. The Compress button has the
  blue gradient.
- **Options.**
  - The "Made for" cards are marked with a blue border and background.
  - The dot of the chosen option appears with a pop.
  - The level is shown large.
- **Dialogs.** They enter growing from 0.94 to 1 and fading in. The background fades. Warning ones keep
  their amber border at the top.
- **The end of a compression** (`done-dialog`, when the queue compressed and saved space):
  1. The size before is shown large: the sum of `OriginalSize` of the games that came out fine.
  2. It squeezes horizontally (`scaleX`) and disappears, and the size now comes out: the sum of
     `outSize`.
  3. At the same time, the full bar squeezes down to the new proportion. What was freed stays on the
     right in striped mint. Below it says "Before: X" and "Now: Y".
  4. "X saved" counts up to `summary.Saved`, with a burst of about 18 points (`transform` and
     `opacity`).
  5. The compress icon brings its arrows together.

  The rest of the dialog stays the same: statistics, "What saved the most", "What now" and the
  ampr-emu-ng notice. When decompressing or if nothing was saved, there is no bar or count, and the
  dialog is the usual one with its animated entrance. With reduced motion the final numbers show
  directly.
- **New texts** (es.js and en.js): `doneBefore` ("Before: {size}") and `doneAfter` ("Now: {size}").
- **Light theme.** The same shapes with the light palette of 5.1.

### 5.4 Performance on the PS5

- Out goes `backdrop-filter` from cards, chips, pills, buttons and notices.
- The background gradients are radial and carry no `filter: blur`.
- Only `transform` and `opacity` are animated, which the PS5 does on the GPU.
- `will-change` only on the two background layers.
- The shine of the bars goes on a pseudo-element that is translated.
- It is checked in the simulator at 1920 x 1080 with the browser's performance panel: no long tasks or
  full-screen repaints while the focus changes.

### 5.5 Accessibility

- **Focus always visible.** On cards, the white ring. On everything else, today's outline with the new
  blue.
- **AA contrast** for text over the ambient background: the dark veil guarantees it. It is checked with
  the lightest colours an icon can give.
- **The large name is `aria-hidden`.** The burst and the background layers are not announced.
- **Reduced motion** respected on both interfaces.

### 5.6 When building it

- **This document rules over the mock-up.** The mock-up (`body[data-dir="a"]`) is the visual
  reference: its CSS can be brought over, removing what the PS5 does not have (`aspect-ratio`, `:has()`,
  `inset`).
- **The blur.** All uses of `backdrop-filter` on the console are removed except the one on the
  background of the sheet and of the dialogs (`.scrim`).
- **The bars keep their HTML.** The track stays the outer element and the fill the inner one. The fill
  takes the whole width (`width: 100%`), with `transform-origin` on the left, and JS sets
  `transform: scaleX(f)` on it instead of `width`. In Arabic, the origin moves to the right.
- **Ids and elements can be added.** The tests require the existing ones, they do not forbid others.
- **The numbers at the end.**
  - "Before" is the sum of `OriginalSize` of the games that came out fine, and "Now" the sum of their
    `outSize`. The bar squeezes down to Now/Before.
  - The count ends at `summary.Saved`, the same number the system notification says.
  - If any size is missing or Before is 0, there is no bar, and only the saved number shows.
- **The console's offline cache** (appcache) is not touched: the manifest carries the version and the
  hash of the page, and renews itself.

## 6. What I decided (review it)

1. The large name of the focused game on the console, which is the only new thing you see.
2. Removing almost all the blur on the console, for performance.
3. Group titles in normal sentence case instead of UPPERCASE.
4. The palette of the PC light theme, which the mock-ups did not show.
5. Dimming to 85 % the cards without focus while the focus is in the grid.
6. The two new PC texts (`doneBefore` and `doneAfter`).
7. Redoing the screenshots of `docs/img/window-*.jpg`, which appear in the guide, with the new look.

## 7. What is left to whoever builds it

The exact spacing and shadow values within what was said, the shape of the halo, the number of points
of the burst and how to split the CSS in the file. As long as 3 and 5 are kept.

## 8. How it is checked

- **Console:** `node tools/mock_ui.js --port 8797`, at 1920 x 1080 and at 390 x 844. These are reviewed:
  - library with and without games, filters and drives;
  - a game's sheet, with an error and without;
  - Install with a job in progress, paused and finished;
  - Settings and the confirm dialog;
  - focus with the arrows;
  - Arabic (right-to-left direction) and reduced motion.

  Then, `node --check` of the script and of `tools/mock_ui.js`, and `bash tools/check_identity.sh`.
- **PC:** the window simulator, at 1180 x 720 and at 800 x 600, in dark and light theme. These are
  reviewed:
  - the five sections;
  - a queue compressing;
  - the end with the animation;
  - each warning dialog;
  - dropping files;
  - reduced motion.

  Then, `go test ./...` in `gui/`, which runs the page tests.
- **Where each simulator runs.** Both open in the built-in browser, which is Chromium. The PC window's
  one is a small server that loads `gui/frontend` with a stand-in for the window (`window.go` and
  `window.runtime`). With it the new screenshots of `docs/img/window-*.jpg` are also made, at the same
  size as the current ones.
- **On your PS5.** The console's WebKit can only be tested on it. When the branches are pushed, the last
  step is to open the page on the PS5 and move the cursor around the library: the background and the
  cards must change without stutter. If there is stutter, the dimming of the other cards goes first and
  then the halo.
- **CI green** on both branches, `ui-rediseno` (maxpack-ps5) and `interfaz-pc-rediseno` (maxpack), pushed
  to origin. No PR or merge: you decide that.
