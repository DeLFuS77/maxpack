// SPDX-License-Identifier: GPL-3.0-or-later
"use strict";

// The texts of the window, by language, as in NXPress: each file of this folder adds one
// (texts.es, texts.en) and the page asks for them by name with t("name"), or with data-t in
// index.html. A text is a string, or a function of what it says (a number, a size, a path).
// Each one goes on a line of its own, "  name: …,", which is how the tests read them.
const texts = {};

// the languages of the window, by their own name, in the order of Ajustes
const languages = [["es", "Español"], ["en", "English"]];
