// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestPageKeys: the keyboard shortcuts (Ctrl+O, Ctrl+Shift+O, Ctrl+Enter, Esc, Ctrl+P, Ctrl+1 to Ctrl+5; ⌘ on
// macOS), what each does, when each does nothing, and the list Settings shows of them.
func TestPageKeys(t *testing.T) {
	runPage(t, `
  const key = (k, more) => Object.assign({ key: k, code: /^[0-9]$/.test(k) ? "Digit" + k : "", ctrlKey: false, metaKey: false, shiftKey: false, altKey: false, target: {} }, more || {});
  const ctrl = (k, more) => key(k, Object.assign({ ctrlKey: true }, more || {}));
  const cases = [[ctrl("o"), "addFiles"], [ctrl("O", { shiftKey: true }), "addFolder"], [ctrl("Enter"), "start"], [key("Escape"), "cancel"],
    [ctrl("p"), "pause"], [ctrl("1"), "section:compress"], [ctrl("2"), "section:decompress"], [ctrl("3"), "section:verify"],
    [ctrl("4"), "section:details"], [ctrl("5"), "section:rename"], [key("o", { metaKey: true }), "addFiles"],
    [ctrl("6"), ""], [key("o"), ""], [ctrl("o", { altKey: true }), ""], [ctrl("p", { shiftKey: true }), ""], [key("Enter"), ""], [ctrl("Escape"), ""],
    [ctrl("&", { code: "Digit1", shiftKey: true }), ""], [ctrl("&", { code: "Digit1" }), "section:compress"],
    // a layout that writes no Latin letters (Russian, Arabic): the letter of the key's place
    [ctrl("щ", { code: "KeyO" }), "addFiles"], [ctrl("Щ", { code: "KeyO", shiftKey: true }), "addFolder"], [ctrl("з", { code: "KeyP" }), "pause"],
    [ctrl("خ", { code: "KeyO" }), "addFiles"], [ctrl("ح", { code: "KeyP" }), "pause"], [ctrl("ы", { code: "KeyS" }), ""],
    // a Latin layout (AZERTY, Dvorak): the letter printed on the key, wherever it is
    [ctrl("o", { code: "KeyS" }), "addFiles"], [ctrl("s", { code: "KeyO" }), ""], [ctrl("p", { code: "KeyR" }), "pause"]];
  for (const [event, want] of cases) if (shortcutOf(event) !== want) say("the shortcut of " + JSON.stringify(event) + ": " + JSON.stringify(shortcutOf(event)) + ", not " + want);
  // what each one does, and that the browser does not take it
  const press = event => { event.preventDefault = () => { event.prevented = true; }; onKey(event); return event; };
  state.section = "compress";
  let e = press(ctrl("o"));
  if (calls.join() !== 'ChooseFiles ["compress"]' || !e.prevented) say("Ctrl+O: " + calls + " " + e.prevented);
  calls.length = 0;
  press(ctrl("o", { shiftKey: true }));
  if (calls.join() !== 'ChooseFolder ["compress"]') say("Ctrl+Shift+O: " + calls);
  calls.length = 0;
  $("start").hidden = false; $("start").disabled = false;
  press(ctrl("Enter"));
  if ($("start").clicked !== 1) say("Ctrl+Enter does not press Start");
  $("start").disabled = true;
  press(ctrl("Enter"));
  if ($("start").clicked !== 1) say("Ctrl+Enter pressed a Start that cannot be pressed");
  // Esc: with a queue running it asks first, and cancels only once the answer is yes
  state.busy = true;
  state.job = { section: "compress", mode: "p5z", rows: [], running: true };
  e = press(key("Escape"));
  if (!$("cancel-dialog").open || calls.includes("Cancel") || !e.prevented) say("Esc: " + $("cancel-dialog").open + " " + calls);
  $("cancel-dialog").open = false;
  press(key("Escape", { target: { tagName: "INPUT" } }));
  if ($("cancel-dialog").open) say("Esc in a field asks to cancel");
  for (const target of [{ tagName: "INPUT", type: "text" }, { tagName: "INPUT", type: "search" }, { tagName: "TEXTAREA" }, { tagName: "SELECT" }]) {
    press(key("Escape", { target }));
    if ($("cancel-dialog").open) { say("Esc while typing asks to cancel: " + JSON.stringify(target)); $("cancel-dialog").open = false; }
  }
  // a checkbox, a radio, a range or a switch with the focus takes no typing: Esc asks, as anywhere
  for (const target of [{ tagName: "INPUT", type: "checkbox" }, { tagName: "INPUT", type: "radio" }, { tagName: "INPUT", type: "range" },
    { tagName: "INPUT", type: "checkbox", role: "switch" }]) {
    press(key("Escape", { target }));
    if (!$("cancel-dialog").open) say("Esc on a " + target.type + " does not ask to cancel");
    $("cancel-dialog").open = false;
  }
  const query = document.querySelector;
  document.querySelector = selector => (selector === "dialog[open]" ? {} : null);
  press(key("Escape"));
  press(ctrl("p"));
  document.querySelector = query;
  if ($("cancel-dialog").open || calls.length) say("a shortcut under an open dialog: " + calls);
  press(ctrl("p"));
  await Promise.resolve();
  if (calls.join() !== "Pause") say("Ctrl+P: " + calls);
  calls.length = 0;
  state.busy = false; state.job = null; state.paused = false;
  press(key("Escape"));
  if ($("cancel-dialog").open) say("Esc with nothing running asks to cancel");
  // in Settings there is nothing to add
  state.section = "settings";
  press(ctrl("o"));
  if (calls.length) say("Ctrl+O in Settings: " + calls);
  // the list of Settings, on Windows and on macOS
  const rows = keyRows("Win32");
  if (rows.map(row => row[0]).join("|") !== "Ctrl+O|Ctrl+Shift+O|Ctrl+Enter|Esc|Ctrl+P|Ctrl+1 … Ctrl+5") say("the keys on Windows: " + rows.map(row => row[0]));
  if (keyRows("MacIntel").map(row => row[0]).join("|") !== "⌘O|⇧⌘O|⌘↩|Esc|⌘P|⌘1 … ⌘5") say("the keys on macOS: " + keyRows("MacIntel").map(row => row[0]));
  for (const [, text] of rows) if (typeof text !== "string" || !text) say("a key without its text");
  for (const name of ["prefKeys", "keysHint", "cancelTitle", "cancelText", "cancelYes", "cancelNo", "removeRow", "progressQueue", "progressGame", "endNotice", "endNoticeHint"])
    if (!t(name)) say("no text " + name);
`)
}

// TestPageEndSound: the window asks for the sound of the end (job:chime) and the page plays it; the page tells
// the window whether it is in front, as it gains and loses the focus.
func TestPageEndSound(t *testing.T) {
	runPage(t, `
  const played = [];
  window.AudioContext = function () {
    this.state = "running"; this.currentTime = 0; this.destination = {};
    this.resume = () => Promise.resolve();
    const node = () => ({ connect: next => next || {}, start: at => played.push(at), stop() {}, frequency: { value: 0 }, gain: { setValueAtTime() {}, exponentialRampToValueAtTime() {} }, type: "" });
    this.createOscillator = node; this.createGain = node;
  };
  chime();
  if (played.length < 2) say("the sound of the end did not play: " + played);
  tellFront();
  if (calls.join() !== "TellFront [true]") say("the page did not tell it is in front: " + calls);
  calls.length = 0;
  document.hasFocus = () => false;
  tellFront();
  if (calls.join() !== "TellFront [false]") say("the page did not tell it is behind: " + calls);
  delete window.AudioContext;
  chime();
`)
}

// TestPageAccessible: every button says what it does (text, or a label for one that is only an icon), every
// bar is a progressbar with its value and name, what changes by itself is told to screen readers, nothing
// jumps the order of the tab key, the focus shows on everything, and nothing moves when less motion is asked.
func TestPageAccessible(t *testing.T) {
	page, err := html.Parse(strings.NewReader(readPage(t, "index.html")))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]*html.Node{}
	walk(page, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		id, _ := attr(node, "id")
		if id != "" {
			ids[id] = node
		}
		if index, ok := attr(node, "tabindex"); ok {
			if n, _ := strconv.Atoi(index); n > 0 {
				t.Errorf("%s has tabindex %s: the order of the tab key is the order of the page", id, index)
			}
		}
		class, _ := attr(node, "class")
		// a button that is only a sign (the ✕ of a row) hides the sign: the page names it (paintRemove)
		if node.Data == "button" && len([]rune(strings.TrimSpace(textOf(node)))) == 1 {
			sign := node.FirstChild
			if hidden, _ := attr(sign, "aria-hidden"); sign == nil || sign.Type != html.ElementNode || hidden != "true" {
				t.Errorf("a button that is only a sign (%q) reads it aloud", textOf(node))
			}
		}
		if hidden, _ := attr(node, "aria-hidden"); node.Data == "div" && (hasClass(class, "bar") || hasClass(class, "segs")) && hidden != "true" {
			role, _ := attr(node, "role")
			min, _ := attr(node, "aria-valuemin")
			max, _ := attr(node, "aria-valuemax")
			_, label := attr(node, "data-t-label")
			// the bar of a game is named after it by the page (paintState)
			if role != "progressbar" || min != "0" || max != "100" || (!label && !hasClass(class, "segs")) {
				t.Errorf("a bar (%s %s) is not a progressbar with its range and name", id, class)
			}
		}
	})
	for _, id := range []string{"status", "toast", "send-asking", "levels-status", "measure-status"} {
		node := ids[id]
		if node == nil {
			t.Errorf("no #%s", id)
			continue
		}
		if live, _ := attr(node, "aria-live"); live != "polite" {
			t.Errorf("#%s is not told to screen readers (aria-live %q)", id, live)
		}
	}
	css := readPage(t, "style.css")
	if !strings.Contains(css, ":focus-visible { outline: 2px solid var(--focus)") {
		t.Error("the focus does not show")
	}
	if !regexp.MustCompile(`@media \(prefers-reduced-motion: reduce\) \{\s*\*, \*::before, \*::after \{ animation: none !important; transition: none !important; \}`).MatchString(css) {
		t.Error("animations go on when less motion is asked for")
	}
	if strings.Contains(css, "outline: none") {
		t.Error("an outline is taken away")
	}
	app := readPage(t, "app.js")
	if !strings.Contains(app, `matchMedia("(prefers-reduced-motion: reduce)")`) {
		t.Error("the end card animates whatever the system asks")
	}
	for _, set := range []string{"tellBar(bar, share)", "tellBar(segs, ", `tellBar($(prefix + "-bar"), share)`} {
		if !strings.Contains(app, set) {
			t.Errorf("the page does not set %s", set)
		}
	}
}

// TestPageBarsTellTheirValue: the bars of the queue and of a game say how far they are (and that they are
// paused), and the button that takes a game out of the list says which.
func TestPageBarsTellTheirValue(t *testing.T) {
	runPage(t, `
  state.busy = true;
  const tr = { querySelector: selector => (tr.parts[selector] = tr.parts[selector] || $("row" + selector)), parts: {}, classList: { toggle() {}, add() {}, remove() {} }, addEventListener() {} };
  const row = { path: "D:/a", section: "compress", game: { Name: "Starfall Odyssey", OriginalSize: 100 }, state: "packing", step: "packing", phase: "packing", offset: 0, done: 40, total: 100, warnings: [], tr };
  state.job = { section: "compress", mode: "p5z", rows: [row], running: true, rate: 0 };
  paintState(row);
  const segs = tr.querySelector(".segs");
  if (segs.getAttribute("aria-valuenow") !== "20" || segs.getAttribute("aria-label") !== t("progressGame")("Starfall Odyssey")) say("the bar of a game: " + JSON.stringify(segs.attributes));
  paintSummary();
  const total = $("total-bar");
  if (total.getAttribute("aria-valuenow") !== "20" || !total.getAttribute("aria-valuetext")) say("the bar of the queue: " + JSON.stringify(total.attributes));
  state.paused = true;
  paintSummary();
  if (!String(total.getAttribute("aria-valuetext")).includes(t("stepPaused"))) say("the bar of the queue does not say it is paused: " + total.getAttribute("aria-valuetext"));
  paintRemove(row);
  const remove = tr.querySelector(".remove button");
  if (remove.getAttribute("aria-label") !== t("removeRow")("Starfall Odyssey") || remove.title !== t("removeRow")("Starfall Odyssey")) say("the remove button: " + JSON.stringify(remove.attributes));
`)
}

// TestPageContrast: in each theme, text reads at 4.5:1 at least (WCAG AA) on every surface it is written on,
// the focus ring at 3:1, and the light theme of the system is the one Settings chooses.
func TestPageContrast(t *testing.T) {
	css := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(readPage(t, "style.css"), "")
	block := func(selector string) map[string]string {
		at := strings.Index(css, selector)
		if at < 0 {
			t.Fatalf("no %s in style.css", selector)
		}
		body := css[at+len(selector):]
		body = body[:strings.Index(body, "}")]
		tokens := map[string]string{}
		for _, found := range regexp.MustCompile(`--([a-z0-9-]+):\s*([^;]+);`).FindAllStringSubmatch(body, -1) {
			tokens[found[1]] = strings.TrimSpace(found[2])
		}
		return tokens
	}
	dark, light := block(":root {"), block(`:root[data-theme="light"] {`)
	system := block(`:root:not([data-theme="dark"]) {`)
	for name, value := range light {
		if system[name] != value {
			t.Errorf("the light theme of the system says --%s: %s, Settings' %s", name, system[name], value)
		}
	}
	if regexp.MustCompile(`[^-]color: var\(--accent\);`).MatchString(css) {
		t.Error("text in --accent reads below 4.5:1 on some surfaces: --accent-ink is for text")
	}
	for theme, tokens := range map[string]map[string]string{"dark": dark, "light": light} {
		hex := func(name string) [3]float64 {
			color, ok := parseHex(tokens[name])
			if !ok {
				t.Fatalf("%s: --%s is %q", theme, name, tokens[name])
			}
			return color
		}
		soft := func(name string, over [3]float64) [3]float64 {
			found := regexp.MustCompile(`rgba\((\d+), (\d+), (\d+), ([.\d]+)\)`).FindStringSubmatch(tokens[name])
			if found == nil {
				t.Fatalf("%s: --%s is %q", theme, name, tokens[name])
			}
			var color [3]float64
			for i := range color {
				color[i], _ = strconv.ParseFloat(found[i+1], 64)
			}
			alpha, _ := strconv.ParseFloat(found[4], 64)
			return mix(color, alpha, over)
		}
		surfaces := []string{"bg", "panel", "raised", "hover"}
		check := func(what string, fg, bg [3]float64, least float64) {
			if got := contrast(fg, bg); got < least {
				t.Errorf("%s: %s reads at %.2f:1, less than %.1f:1", theme, what, got, least)
			}
		}
		for _, fg := range []string{"text", "muted", "accent-ink", "ok", "bad", "warn"} {
			for _, bg := range surfaces {
				check("--"+fg+" on --"+bg, hex(fg), hex(bg), 4.5)
			}
		}
		for _, bg := range []string{"panel", "raised"} {
			check("--accent-ink on --accent-soft over --"+bg, hex("accent-ink"), soft("accent-soft", hex(bg)), 4.5)
			check("--ok on --ok-soft over --"+bg, hex("ok"), soft("ok-soft", hex(bg)), 4.5)
			check("--bad on --bad-soft over --"+bg, hex("bad"), soft("bad-soft", hex(bg)), 4.5)
			check("--warn on its 14 % over --"+bg, hex("warn"), mix(hex("warn"), .14, hex(bg)), 4.5)
		}
		for _, stop := range regexp.MustCompile(`#[0-9a-fA-F]{6}`).FindAllString(tokens["btn-grad"], -1) {
			color, _ := parseHex(stop)
			check("--btn-ink on "+stop+" (Start)", hex("btn-ink"), color, 4.5)
		}
		check("--accent-text on --accent", hex("accent-text"), hex("accent"), 4.5)
		check("a notice (--bg on --text)", hex("bg"), hex("text"), 4.5)
		for _, bg := range surfaces {
			check("the focus ring on --"+bg, hex("focus"), hex(bg), 3)
		}
	}
}

func parseHex(text string) ([3]float64, bool) {
	var color [3]float64
	if len(text) == 4 && text[0] == '#' { // #fff
		text = "#" + strings.Repeat(text[1:2], 2) + strings.Repeat(text[2:3], 2) + strings.Repeat(text[3:4], 2)
	}
	if len(text) != 7 || text[0] != '#' {
		return color, false
	}
	for i := range color {
		n, err := strconv.ParseUint(text[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return color, false
		}
		color[i] = float64(n)
	}
	return color, true
}

func mix(fg [3]float64, alpha float64, bg [3]float64) [3]float64 {
	var out [3]float64
	for i := range out {
		out[i] = math.Round(alpha*fg[i] + (1-alpha)*bg[i])
	}
	return out
}

// contrast is the WCAG 2 contrast ratio of two colors.
func contrast(a, b [3]float64) float64 {
	luminance := func(c [3]float64) float64 {
		var l [3]float64
		for i, v := range c {
			v /= 255
			if v <= 0.03928 {
				l[i] = v / 12.92
			} else {
				l[i] = math.Pow((v+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
	}
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func hasClass(class, name string) bool {
	for _, c := range strings.Fields(class) {
		if c == name {
			return true
		}
	}
	return false
}

func textOf(node *html.Node) string {
	var b strings.Builder
	walk(node, func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
	})
	return b.String()
}

func hasText(node *html.Node) bool { return strings.TrimSpace(textOf(node)) != "" }
