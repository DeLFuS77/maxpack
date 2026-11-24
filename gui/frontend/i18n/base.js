// SPDX-License-Identifier: GPL-3.0-or-later
"use strict";

// The texts of the window, by language, as in NXPress: each file of this folder adds one
// (texts.en, texts.es, ...) and the page asks for them by name with t("name"), or with data-t in
// index.html. A text is a string, or a function of what it says (a number, a size, a path).
// Each one goes on a line of its own, "  name: ...,", which is how the tests read them.
const texts = {};

// The languages of the window, by their own name, in the order of Settings: the same eleven as the
// page of MaxPack on the console. tag is what the document says it is (<html lang>), when it needs
// more than the code; rtl is a language written from right to left.
const languages = [
  ["en", "English"], ["ru", "Русский"], ["zh", "简体中文", "zh-Hans"], ["de", "Deutsch"],
  ["es", "Español"], ["fr", "Français"], ["ja", "日本語"], ["pt", "Português (Brasil)", "pt-BR"],
  ["it", "Italiano"], ["ko", "한국어"], ["ar", "العربية", "ar", true],
];

const isLanguage = code => languages.some(language => language[0] === code);
const languageInfo = code => languages.find(language => language[0] === code) || languages[0];

// The language to start in when the user never chose one: the first of the languages of the
// system that the window has, by its base subtag (pt-BR is pt, zh-Hant-TW is zh), else English.
function systemLanguage(nav) {
  const tags = nav && nav.languages && nav.languages.length ? Array.from(nav.languages) : nav && nav.language ? [nav.language] : [];
  for (const tag of tags) {
    const base = String(tag).toLowerCase().split(/[-_]/)[0];
    if (isLanguage(base)) return base;
  }
  return "en";
}
