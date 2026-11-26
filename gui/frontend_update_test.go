// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"strings"
	"testing"
)

// TestPageUpdate: Settings › Internet looks for new versions only once turned on, and only in a build with
// the key; it says what it found in the page's words, offers Update (the installer on Windows, the page
// elsewhere) and says that Internet stays quiet until then.
func TestPageUpdate(t *testing.T) {
	runPage(t, `
  for (const key of ["updateCheck", "updateHint", "updateUnavailable", "updateChecking", "updateNone", "updateInstall", "updateOpenPage", "updateWait",
    "updateError_network", "updateError_unsigned", "updateError_bad_signature", "updateError_not_listed", "updateError_bad_download"])
    if (typeof t(key) !== "string" || !t(key)) say("no text " + key);
  for (const key of ["updateFoundInstall", "updateFoundPage", "updateFoundToast"])
    if (typeof t(key) !== "function" || !t(key)("1.1.0").includes("1.1.0")) say("no text " + key);
  if (!/GitHub/.test(t("internetText")) || /does not look for new versions/.test(t("internetText"))) say("Internet does not say when it goes on the internet: " + t("internetText"));
  // a build without the key: the switch cannot be turned on, and says why; nothing is asked
  state.setup = { updateKey: false };
  state.settings.UpdateCheck = true;
  paintUpdate();
  if (!$("pref-update").disabled || $("pref-update").checked || $("update-status").textContent !== t("updateUnavailable")) say("without a key: " + JSON.stringify($("pref-update")));
  await checkUpdate();
  if (calls.length) say("asked without a key: " + calls);
  // with the key but off: nothing is asked
  state.setup = { updateKey: true };
  state.settings.UpdateCheck = false;
  await checkUpdate();
  paintUpdate();
  if (calls.length || $("pref-update").disabled || $("pref-update").checked || $("update-status").textContent !== "") say("off: " + calls + " " + $("update-status").textContent);
  // on: asked once, and what was found is offered
  state.settings.UpdateCheck = true;
  window.answers = { CheckUpdate: { state: "found", version: "1.1.0", page: "https://github.com/DeLFuS77/maxpack/releases/tag/v1.1.0", install: true, error: "" } };
  await checkUpdate();
  await checkUpdate();
  if (calls.join() !== "CheckUpdate") say("asked: " + calls);
  if ($("update-status").textContent !== t("updateFoundInstall")("1.1.0") || $("update-go").hidden || $("update-go").textContent !== t("updateInstall")) say("found: " + $("update-status").textContent + " " + $("update-go").textContent);
  if ($("toast").textContent !== t("updateFoundToast")("1.1.0")) say("the toast: " + $("toast").textContent);
  calls.length = 0;
  await runUpdate();
  if (calls.join() !== "Update") say("Update: " + calls);
  // the installer waits for the queue; the page does not
  calls.length = 0;
  state.busy = true;
  await runUpdate();
  if (calls.length || $("toast").textContent !== t("updateWait")) say("updated while busy: " + calls);
  state.update = { state: "found", version: "1.1.0", install: false };
  await runUpdate();
  paintUpdate();
  if (calls.join() !== "Update" || $("update-go").textContent !== t("updateOpenPage")) say("the page: " + calls + " " + $("update-go").textContent);
  state.busy = false;
  // what it says of each answer
  for (const [view, text] of [[{ state: "none" }, t("updateNone")], [{ state: "error", error: "bad_signature" }, t("updateError_bad_signature")],
    [{ state: "error", error: "what" }, t("updateError_network")], [null, t("updateChecking")], [{ state: "found", version: "2.0.0", install: false }, t("updateFoundPage")("2.0.0")]]) {
    if (updateText(view) !== text) say("updateText(" + JSON.stringify(view) + "): " + updateText(view));
  }
`)
	if !strings.Contains(readPage(t, "index.html"), `id="update-status" aria-live="polite"`) {
		t.Error("what the look for versions says is not read out")
	}
}
